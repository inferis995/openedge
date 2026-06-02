import { useState, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Trash2, Plus, Activity, AlertTriangle } from 'lucide-react';
import { tagsApi } from '@/api/tags';
import { toast } from 'sonner';

export interface AlarmDefinition {
    id?: number;
    tag_id: number;
    alarm_type: string;
    threshold: number | null;
    deadband: number;
    delay_seconds: number;
    severity: string;
    message: string;
    enabled: boolean;
}

interface Props {
    tagId: number;
    dataType: string;
}

// Etichette delle condizioni di allarme. Le label sono human-friendly:
// l'operatore vede "Valore troppo alto" non "high". L'helper text spiega
// la regola esatta ("scatta quando valore > soglia").
const ALARM_TYPE_LABELS: Record<string, { label: string; thresholdLabel: string; helper: string }> = {
    bool_true:  { label: 'Quando diventa VERO (ON)',  thresholdLabel: '', helper: 'Scatta sul fronte di salita 0 → 1.' },
    bool_false: { label: 'Quando diventa FALSO (OFF)', thresholdLabel: '', helper: 'Scatta sul fronte di discesa 1 → 0.' },
    high:       { label: 'Valore troppo alto',        thresholdLabel: 'Soglia massima',     helper: 'Scatta quando valore > soglia massima.' },
    low:        { label: 'Valore troppo basso',       thresholdLabel: 'Soglia minima',      helper: 'Scatta quando valore < soglia minima.' },
    high_high:  { label: 'Valore CRITICO alto',       thresholdLabel: 'Soglia critica alta', helper: 'Soglia oltre il "troppo alto" — usata per allarmi urgenti.' },
    low_low:    { label: 'Valore CRITICO basso',      thresholdLabel: 'Soglia critica bassa', helper: 'Soglia oltre il "troppo basso" — usata per allarmi urgenti.' },
};

const SEVERITY_LABELS: Record<string, string> = {
    info:     'Info — solo log',
    warning:  'Allarme — notifica operatore',
    critical: 'Critico — notifica + escalation',
};

// Genera un messaggio default sensato in base alla condizione + valore-soglia.
const defaultMessage = (alarm_type: string, alias: string, threshold: number | null): string => {
    const what = alias || 'tag';
    switch (alarm_type) {
        case 'bool_true':  return `${what}: attivato`;
        case 'bool_false': return `${what}: disattivato`;
        case 'high':       return `${what}: sopra soglia${threshold !== null ? ` (> ${threshold})` : ''}`;
        case 'low':        return `${what}: sotto soglia${threshold !== null ? ` (< ${threshold})` : ''}`;
        case 'high_high':  return `${what}: CRITICO alto${threshold !== null ? ` (> ${threshold})` : ''}`;
        case 'low_low':    return `${what}: CRITICO basso${threshold !== null ? ` (< ${threshold})` : ''}`;
        default:           return `Allarme su ${what}`;
    }
};

export function TagAlarmsTab({ tagId, dataType }: Props) {
    const [alarms, setAlarms] = useState<AlarmDefinition[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isSaving, setIsSaving] = useState(false);

    // Valore corrente del tag — mostrato in cima per dare all'operatore
    // un riferimento mentre imposta le soglie ("ora siamo a 47.2, soglia
    // alta a 80 è giusta?").
    const { data: current } = useQuery({
        queryKey: ['tag-current', tagId],
        queryFn: () => tagsApi.getCurrentValue(tagId),
        enabled: !!tagId,
        refetchInterval: 5_000,
    });

    useEffect(() => {
        const fetchAlarms = async () => {
            if (!tagId) return;
            try {
                const data = await tagsApi.getTagAlarms(tagId);
                setAlarms(data || []);
            } catch (err) {
                console.error("Failed to fetch alarms", err);
            } finally {
                setIsLoading(false);
            }
        };
        fetchAlarms();
    }, [tagId]);

    const handleSave = async () => {
        // Validazione minima: condizioni non-bool richiedono soglia numerica.
        for (const a of alarms) {
            const needsThreshold = a.alarm_type !== 'bool_true' && a.alarm_type !== 'bool_false';
            if (needsThreshold && (a.threshold === null || a.threshold === undefined || Number.isNaN(a.threshold))) {
                toast.error(`Soglia mancante per "${ALARM_TYPE_LABELS[a.alarm_type]?.label ?? a.alarm_type}".`);
                return;
            }
        }
        setIsSaving(true);
        try {
            await tagsApi.saveTagAlarms(tagId, alarms);
            toast.success("Allarmi salvati con successo!");
        } catch (err) {
            console.error("Failed to save alarms", err);
            toast.error("Errore nel salvataggio degli allarmi.");
        } finally {
            setIsSaving(false);
        }
    };

    const addAlarm = () => {
        const defType = dataType === 'BOOL' ? 'bool_true' : 'high';
        const seedThreshold = dataType === 'BOOL' ? null : (typeof current?.value === 'number' ? current.value : 100);
        setAlarms([...alarms, {
            tag_id: tagId,
            alarm_type: defType,
            threshold: seedThreshold,
            deadband: 0,
            delay_seconds: 5,
            severity: 'warning',
            message: defaultMessage(defType, '', seedThreshold),
            enabled: true,
        }]);
    };

    const removeAlarm = (index: number) => {
        setAlarms(alarms.filter((_, i) => i !== index));
    };

    // Aggiornamento del singolo campo. Se cambia alarm_type o threshold,
    // ri-genera il messaggio default *solo se* l'operatore non l'ha già
    // editato a mano (i.e. il messaggio attuale combacia col default
    // calcolato dai valori precedenti — niente sovrascritture aggressive).
    const updateAlarm = (index: number, field: keyof AlarmDefinition, value: AlarmDefinition[keyof AlarmDefinition]) => {
        const cur = alarms[index];
        const newAlarm = { ...cur, [field]: value } as AlarmDefinition;

        const wasDefault = cur.message === defaultMessage(cur.alarm_type, '', cur.threshold);
        if (wasDefault && (field === 'alarm_type' || field === 'threshold')) {
            newAlarm.message = defaultMessage(newAlarm.alarm_type, '', newAlarm.threshold);
        }
        // Switching to/from BOOL types resets threshold appropriately
        if (field === 'alarm_type') {
            const isBoolType = value === 'bool_true' || value === 'bool_false';
            if (isBoolType) newAlarm.threshold = null;
            else if (cur.threshold === null) newAlarm.threshold = typeof current?.value === 'number' ? current.value : 0;
        }

        const next = [...alarms];
        next[index] = newAlarm;
        setAlarms(next);
    };

    if (isLoading) return <div className="p-4 text-center text-sm text-muted-foreground">Caricamento allarmi...</div>;

    const currentValueDisplay = (() => {
        if (current === undefined) return '—';
        if (current.value === null || current.value === undefined) return '—';
        if (typeof current.value === 'boolean') return current.value ? 'TRUE' : 'FALSE';
        if (typeof current.value === 'number') return current.value.toFixed(2);
        return String(current.value);
    })();

    return (
        <div className="space-y-4 py-4">
            {/* Riferimento valore corrente — sempre visibile in cima */}
            <div className="flex items-center justify-between gap-3 px-3 py-2 rounded-md border bg-muted/30 text-sm">
                <div className="flex items-center gap-2 text-muted-foreground">
                    <Activity size={14} />
                    <span>Valore attuale del tag:</span>
                </div>
                <span className="font-mono font-bold text-base">{currentValueDisplay}</span>
            </div>

            <div className="flex justify-between items-center">
                <p className="text-sm text-muted-foreground">
                    Definisci le condizioni che fanno scattare un allarme. Puoi creare più regole indipendenti
                    (es. soglia "Alto" a 80 + "Critico alto" a 95).
                </p>
                <Button onClick={addAlarm} size="sm" variant="outline" className="gap-2 flex-shrink-0">
                    <Plus size={16} /> Nuovo Allarme
                </Button>
            </div>

            {alarms.length === 0 ? (
                <div className="text-center p-8 border border-dashed rounded-md bg-muted/20 text-muted-foreground">
                    <AlertTriangle size={24} className="mx-auto mb-2 opacity-50" />
                    <p className="text-sm">Nessun allarme configurato per questo tag.</p>
                    <p className="text-xs mt-1">Click "Nuovo Allarme" per creare la prima regola.</p>
                </div>
            ) : (
                <div className="space-y-4 max-h-[500px] overflow-y-auto pr-2">
                    {alarms.map((alarm, idx) => {
                        const meta = ALARM_TYPE_LABELS[alarm.alarm_type];
                        const needsThreshold = alarm.alarm_type !== 'bool_true' && alarm.alarm_type !== 'bool_false';
                        return (
                            <div key={idx} className="p-4 border rounded-md shadow-sm space-y-3 bg-card relative">
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="absolute top-2 right-2 h-6 w-6 text-red-500 hover:bg-red-50"
                                    onClick={() => removeAlarm(idx)}
                                >
                                    <Trash2 size={14} />
                                </Button>

                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pr-8">
                                    <div className="space-y-1">
                                        <Label>Condizione</Label>
                                        <Select
                                            value={alarm.alarm_type}
                                            onValueChange={(v) => updateAlarm(idx, 'alarm_type', v)}
                                        >
                                            <SelectTrigger>
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                {dataType === 'BOOL' ? (
                                                    <>
                                                        <SelectItem value="bool_true">{ALARM_TYPE_LABELS.bool_true.label}</SelectItem>
                                                        <SelectItem value="bool_false">{ALARM_TYPE_LABELS.bool_false.label}</SelectItem>
                                                    </>
                                                ) : (
                                                    <>
                                                        <SelectItem value="high">{ALARM_TYPE_LABELS.high.label}</SelectItem>
                                                        <SelectItem value="low">{ALARM_TYPE_LABELS.low.label}</SelectItem>
                                                        <SelectItem value="high_high">{ALARM_TYPE_LABELS.high_high.label}</SelectItem>
                                                        <SelectItem value="low_low">{ALARM_TYPE_LABELS.low_low.label}</SelectItem>
                                                    </>
                                                )}
                                            </SelectContent>
                                        </Select>
                                        {meta?.helper && (
                                            <p className="text-[11px] text-muted-foreground">{meta.helper}</p>
                                        )}
                                    </div>

                                    {needsThreshold && (
                                        <div className="space-y-1">
                                            <Label>{meta?.thresholdLabel || 'Soglia'}</Label>
                                            <Input
                                                type="number"
                                                value={alarm.threshold ?? ''}
                                                onChange={(e) => {
                                                    const v = e.target.value;
                                                    updateAlarm(idx, 'threshold', v === '' ? null : parseFloat(v));
                                                }}
                                                placeholder="es. 80"
                                            />
                                            {typeof current?.value === 'number' && (
                                                <p className="text-[11px] text-muted-foreground">
                                                    Valore attuale: <span className="font-mono">{current.value.toFixed(2)}</span>
                                                </p>
                                            )}
                                        </div>
                                    )}

                                    <div className="space-y-1">
                                        <Label>Ritardo (secondi)</Label>
                                        <Input
                                            type="number"
                                            min="0"
                                            value={alarm.delay_seconds}
                                            onChange={(e) => updateAlarm(idx, 'delay_seconds', parseInt(e.target.value || '0'))}
                                        />
                                        <p className="text-[11px] text-muted-foreground">
                                            Condizione deve resistere {alarm.delay_seconds}s prima di scattare (anti-rimbalzo).
                                        </p>
                                    </div>

                                    {needsThreshold && (
                                        <div className="space-y-1">
                                            <Label>Deadband (isteresi)</Label>
                                            <Input
                                                type="number"
                                                min="0"
                                                value={alarm.deadband}
                                                onChange={(e) => updateAlarm(idx, 'deadband', parseFloat(e.target.value || '0'))}
                                                placeholder="0 = nessuna isteresi"
                                            />
                                            <p className="text-[11px] text-muted-foreground">
                                                Per rientrare, il valore deve scostarsi di almeno {alarm.deadband || 0} dalla soglia.
                                            </p>
                                        </div>
                                    )}

                                    <div className="space-y-1">
                                        <Label>Gravità</Label>
                                        <Select
                                            value={alarm.severity}
                                            onValueChange={(v) => updateAlarm(idx, 'severity', v)}
                                        >
                                            <SelectTrigger>
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="info">{SEVERITY_LABELS.info}</SelectItem>
                                                <SelectItem value="warning">{SEVERITY_LABELS.warning}</SelectItem>
                                                <SelectItem value="critical">{SEVERITY_LABELS.critical}</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>

                                    <div className="space-y-1 md:col-span-2">
                                        <Label>Messaggio di allarme</Label>
                                        <Input
                                            value={alarm.message}
                                            placeholder="Verrà mostrato in allarmi attivi e nelle notifiche"
                                            onChange={(e) => updateAlarm(idx, 'message', e.target.value)}
                                        />
                                        <p className="text-[11px] text-muted-foreground">
                                            Auto-generato dalla condizione; puoi personalizzarlo (es. "Pompa P1 in blocco").
                                        </p>
                                    </div>

                                    <div className="md:col-span-2 flex items-center justify-between pt-2 border-t mt-1">
                                        <div className="flex items-center gap-2">
                                            <Switch
                                                checked={alarm.enabled}
                                                onCheckedChange={(v) => updateAlarm(idx, 'enabled', v)}
                                            />
                                            <Label>Abilita Allarme</Label>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        );
                    })}
                </div>
            )}

            <div className="flex justify-end pt-4 border-t">
                <Button onClick={handleSave} disabled={isSaving}>
                    {isSaving ? 'Salvataggio...' : 'Salva Allarmi'}
                </Button>
            </div>
        </div>
    );
}
