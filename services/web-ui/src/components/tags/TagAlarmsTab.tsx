import { useState, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Trash2, Plus, Activity, AlertTriangle } from 'lucide-react';
import { tagsApi } from '@/api/tags';
import { ALARM_TYPE_LABELS, NO_THRESHOLD_TYPES, HEALTH_TYPES } from '@/lib/alarmTypes';
import { toast } from 'sonner';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';

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
    onSave?: () => void;
}

// i18n keys, resolved with t() at render.
const SEVERITY_LABELS: Record<string, string> = {
    info:     'tagAlarms.severity_info',
    warning:  'tagAlarms.severity_warning',
    critical: 'tagAlarms.severity_critical',
};

// Genera un messaggio default sensato in base alla condizione + valore-soglia.
const defaultMessage = (alarm_type: string, alias: string, threshold: number | null): string => {
    const what = alias || i18n.t('tagAlarms.msg_default_subject');
    const gt = threshold !== null ? ` (> ${threshold})` : '';
    const lt = threshold !== null ? ` (< ${threshold})` : '';
    switch (alarm_type) {
        case 'bool_true':  return i18n.t('tagAlarms.msg_bool_true', { what });
        case 'bool_false': return i18n.t('tagAlarms.msg_bool_false', { what });
        case 'high':       return i18n.t('tagAlarms.msg_high', { what }) + gt;
        case 'low':        return i18n.t('tagAlarms.msg_low', { what }) + lt;
        case 'high_high':  return i18n.t('tagAlarms.msg_high_high', { what }) + gt;
        case 'low_low':    return i18n.t('tagAlarms.msg_low_low', { what }) + lt;
        case 'comm_loss':  return i18n.t('tagAlarms.msg_comm_loss', { what });
        case 'frozen':     return i18n.t('tagAlarms.msg_frozen', { what });
        default:           return i18n.t('tagAlarms.msg_generic', { what });
    }
};

export function TagAlarmsTab({ tagId, dataType, onSave }: Props) {
    const { t } = useTranslation();
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
            const needsThreshold = !NO_THRESHOLD_TYPES.has(a.alarm_type);
            if (needsThreshold && (a.threshold === null || a.threshold === undefined || Number.isNaN(a.threshold))) {
                toast.error(t('tagAlarms.threshold_missing', { condition: ALARM_TYPE_LABELS[a.alarm_type]?.label ?? a.alarm_type }));
                return;
            }
        }
        setIsSaving(true);
        try {
            await tagsApi.saveTagAlarms(tagId, alarms);
            toast.success(t('tagAlarms.saved'));
            onSave?.();
        } catch (err) {
            console.error("Failed to save alarms", err);
            toast.error(t('tagAlarms.save_failed'));
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
            const hasNoThreshold = NO_THRESHOLD_TYPES.has(value as string);
            if (hasNoThreshold) newAlarm.threshold = null;
            else if (cur.threshold === null) newAlarm.threshold = typeof current?.value === 'number' ? current.value : 0;
        }

        const next = [...alarms];
        next[index] = newAlarm;
        setAlarms(next);
    };

    if (isLoading) return <div className="p-4 text-center text-sm text-muted-foreground">{t('tagAlarms.loading')}</div>;

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
                    <span>{t('tagAlarms.current_value')}</span>
                </div>
                <span className="font-mono font-bold text-base">{currentValueDisplay}</span>
            </div>

            <div className="flex justify-between items-center">
                <p className="text-sm text-muted-foreground">
                    {t('tagAlarms.intro')}
                </p>
                <Button onClick={addAlarm} size="sm" variant="outline" className="gap-2 shrink-0">
                    <Plus size={16} /> {t('tagAlarms.add')}
                </Button>
            </div>

            {alarms.length === 0 ? (
                <div className="text-center p-8 border border-dashed rounded-md bg-muted/20 text-muted-foreground">
                    <AlertTriangle size={24} className="mx-auto mb-2 opacity-50" />
                    <p className="text-sm">{t('tagAlarms.empty_title')}</p>
                    <p className="text-xs mt-1">{t('tagAlarms.empty_hint')}</p>
                </div>
            ) : (
                <div className="space-y-4 max-h-[500px] overflow-y-auto pr-2">
                    {alarms.map((alarm, idx) => {
                        const meta = ALARM_TYPE_LABELS[alarm.alarm_type];
                        const needsThreshold = !NO_THRESHOLD_TYPES.has(alarm.alarm_type);
                        const isHealth = HEALTH_TYPES.has(alarm.alarm_type);
                        return (
                            <div key={idx} className="p-4 border rounded-md shadow-xs space-y-3 bg-card relative">
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="absolute top-2 right-2 h-9 sm:h-6 w-9 sm:w-6 text-red-500 hover:bg-red-50"
                                    onClick={() => removeAlarm(idx)}
                                    aria-label={t('tagAlarms.remove')}
                                >
                                    <Trash2 size={14} />
                                </Button>

                                <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pr-8">
                                    <div className="space-y-1">
                                        <Label>{t('tagAlarms.condition')}</Label>
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
                                                {/* Valgono per qualsiasi tipo di dato: guardano il
                                                    collegamento, non il valore. */}
                                                <SelectItem value="comm_loss">{ALARM_TYPE_LABELS.comm_loss.label}</SelectItem>
                                                <SelectItem value="frozen">{ALARM_TYPE_LABELS.frozen.label}</SelectItem>
                                            </SelectContent>
                                        </Select>
                                        {meta?.helper && (
                                            <p className="text-[11px] text-muted-foreground">{meta.helper}</p>
                                        )}
                                    </div>

                                    {needsThreshold && (
                                        <div className="space-y-1">
                                            <Label>{meta?.thresholdLabel || t('tagAlarms.threshold')}</Label>
                                            <Input
                                                type="number"
                                                value={alarm.threshold ?? ''}
                                                onChange={(e) => {
                                                    const v = e.target.value;
                                                    updateAlarm(idx, 'threshold', v === '' ? null : parseFloat(v));
                                                }}
                                                placeholder={t('tagAlarms.threshold_placeholder')}
                                            />
                                            {typeof current?.value === 'number' && (
                                                <p className="text-[11px] text-muted-foreground">
                                                    {t('tagAlarms.current_value_short')} <span className="font-mono">{current.value.toFixed(2)}</span>
                                                </p>
                                            )}
                                        </div>
                                    )}

                                    <div className="space-y-1">
                                        <Label>{t('tagAlarms.delay')}</Label>
                                        <Input
                                            type="number"
                                            min="0"
                                            value={alarm.delay_seconds}
                                            onChange={(e) => updateAlarm(idx, 'delay_seconds', parseInt(e.target.value || '0'))}
                                        />
                                        <p className="text-[11px] text-muted-foreground">
                                            {isHealth
                                                ? (alarm.alarm_type === 'comm_loss'
                                                    ? t('tagAlarms.delay_help_comm_loss', { seconds: alarm.delay_seconds || 60 })
                                                    : t('tagAlarms.delay_help_frozen', { seconds: alarm.delay_seconds || 60 }))
                                                : t('tagAlarms.delay_help', { seconds: alarm.delay_seconds })}
                                        </p>
                                    </div>

                                    {/* La deadband serve anche a "valore bloccato": li' dice
                                        quanto rumore NON va considerato movimento. */}
                                    {(needsThreshold || alarm.alarm_type === 'frozen') && (
                                        <div className="space-y-1">
                                            <Label>{t('tagAlarms.deadband')}</Label>
                                            <Input
                                                type="number"
                                                min="0"
                                                value={alarm.deadband}
                                                onChange={(e) => updateAlarm(idx, 'deadband', parseFloat(e.target.value || '0'))}
                                                placeholder={t('tagAlarms.deadband_placeholder')}
                                            />
                                            <p className="text-[11px] text-muted-foreground">
                                                {alarm.alarm_type === 'frozen'
                                                    ? t('tagAlarms.deadband_help_frozen', { value: alarm.deadband || 0 })
                                                    : t('tagAlarms.deadband_help', { value: alarm.deadband || 0 })}
                                            </p>
                                        </div>
                                    )}

                                    <div className="space-y-1">
                                        <Label>{t('tagAlarms.severity')}</Label>
                                        <Select
                                            value={alarm.severity}
                                            onValueChange={(v) => updateAlarm(idx, 'severity', v)}
                                        >
                                            <SelectTrigger>
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="info">{t(SEVERITY_LABELS.info)}</SelectItem>
                                                <SelectItem value="warning">{t(SEVERITY_LABELS.warning)}</SelectItem>
                                                <SelectItem value="critical">{t(SEVERITY_LABELS.critical)}</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>

                                    <div className="space-y-1 md:col-span-2">
                                        <Label>{t('tagAlarms.message')}</Label>
                                        <Input
                                            value={alarm.message}
                                            placeholder={t('tagAlarms.message_placeholder')}
                                            onChange={(e) => updateAlarm(idx, 'message', e.target.value)}
                                        />
                                        <p className="text-[11px] text-muted-foreground">
                                            {t('tagAlarms.message_help')}
                                        </p>
                                    </div>

                                    <div className="md:col-span-2 flex items-center justify-between pt-2 border-t mt-1">
                                        <div className="flex items-center gap-2">
                                            <Switch
                                                checked={alarm.enabled}
                                                onCheckedChange={(v) => updateAlarm(idx, 'enabled', v)}
                                            />
                                            <Label>{t('tagAlarms.enabled')}</Label>
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
                    {isSaving ? t('tagAlarms.saving') : t('tagAlarms.save')}
                </Button>
            </div>
        </div>
    );
}
