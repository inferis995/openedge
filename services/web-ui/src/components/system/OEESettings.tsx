import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Gauge, Loader2, CheckCircle2, AlertCircle, Info } from 'lucide-react';

import { systemApi, GlobalSettings } from '@/api/system';
import { tagsApi } from '@/api/tags';
import type { Tag } from '@/types';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';

// OEE configurabile da UI. La logica della card OEE in dashboard funziona
// "out-of-the-box" anche senza nulla di configurato qui (fallback euristici);
// quando l'admin sceglie i tag di produzione, il calcolo passa in modalità
// reale e i numeri diventano quelli veri della fabbrica.

interface Props {
    initial?: GlobalSettings | null;
    onSaved?: () => void;
}

type Toast = { kind: 'success' | 'error'; text: string } | null;

const WINDOW_PRESETS = [
    { value: '60',   label: '1 ora' },
    { value: '240',  label: '4 ore' },
    { value: '480',  label: '8 ore (un turno)' },
    { value: '720',  label: '12 ore' },
    { value: '1440', label: '24 ore' },
];

const NONE_TAG = '0'; // marker "nessun tag" — riconduce al fallback euristico

const OEESettings = ({ initial, onSaved }: Props) => {
    const [windowMin, setWindowMin]   = useState('480');
    const [target, setTarget]         = useState('85');
    const [runTimeTag, setRunTimeTag] = useState(NONE_TAG);
    const [producedTag, setProducedTag] = useState(NONE_TAG);
    const [goodTag, setGoodTag]       = useState(NONE_TAG);
    const [pphTarget, setPphTarget]   = useState('0');
    const [saving, setSaving]         = useState(false);
    const [toast, setToast]           = useState<Toast>(null);

    useEffect(() => {
        if (!initial) return;
        setWindowMin(initial.oee_window_minutes ?? '480');
        setTarget(initial.oee_target ?? '85');
        setRunTimeTag(initial.oee_run_time_tag ?? NONE_TAG);
        setProducedTag(initial.oee_produced_tag ?? NONE_TAG);
        setGoodTag(initial.oee_good_tag ?? NONE_TAG);
        setPphTarget(initial.oee_target_pieces_per_hour ?? '0');
    }, [initial]);

    const { data: tags } = useQuery({
        queryKey: ['oee-tags-picker'],
        queryFn: () => tagsApi.getAllTags(),
    });

    const tagOptions = useMemo(() => tags ?? [], [tags]);

    const handleSave = async () => {
        setSaving(true);
        setToast(null);
        try {
            await systemApi.updateOEE({
                oee_window_minutes:         windowMin,
                oee_target:                 target,
                oee_run_time_tag:           runTimeTag,
                oee_produced_tag:           producedTag,
                oee_good_tag:               goodTag,
                oee_target_pieces_per_hour: pphTarget,
            });
            setToast({ kind: 'success', text: 'OEE config salvata. La dashboard si aggiorna al prossimo refresh.' });
            onSaved?.();
        } catch (e: unknown) {
            setToast({ kind: 'error', text: `Save failed: ${(e as Error)?.message ?? 'unknown error'}` });
        } finally {
            setSaving(false);
        }
    };

    const renderTagSelect = (
        id: string, value: string, onChange: (v: string) => void, placeholder: string,
    ) => (
        <Select value={value} onValueChange={onChange}>
            <SelectTrigger id={id}>
                <SelectValue placeholder={placeholder} />
            </SelectTrigger>
            <SelectContent>
                <SelectItem value={NONE_TAG}>— Fallback (euristico) —</SelectItem>
                {tagOptions.map((t: Tag) => (
                    <SelectItem key={t.id} value={String(t.id)}>
                        {t.alias || t.code} <span className="text-muted-foreground ml-1">({t.data_type})</span>
                    </SelectItem>
                ))}
            </SelectContent>
        </Select>
    );

    const usingFallback =
        runTimeTag === NONE_TAG && producedTag === NONE_TAG && goodTag === NONE_TAG;

    return (
        <Card className="border-border shadow-sm bg-card">
            <CardHeader className="pb-4 border-b border-border">
                <div className="flex items-center gap-3">
                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                        <Gauge className="h-4 w-4 text-primary" />
                    </div>
                    <div>
                        <CardTitle className="text-base text-foreground">OEE — Overall Equipment Effectiveness</CardTitle>
                        <CardDescription className="text-xs mt-0.5">
                            Availability × Performance × Quality (ISO 22400). La card OEE in dashboard
                            funziona anche senza tag configurati (fallback euristici).
                        </CardDescription>
                    </div>
                </div>
            </CardHeader>
            <CardContent className="pt-5 space-y-5">
                {/* Window + target */}
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div className="space-y-1">
                        <Label className="text-xs">Finestra di calcolo</Label>
                        <Select value={windowMin} onValueChange={setWindowMin}>
                            <SelectTrigger>
                                <SelectValue placeholder="Seleziona finestra" />
                            </SelectTrigger>
                            <SelectContent>
                                {WINDOW_PRESETS.map((p) => (
                                    <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        <p className="text-[11px] text-muted-foreground">
                            Default 8h = un turno. Più corta = numeri più reattivi ma rumorosi.
                        </p>
                    </div>
                    <div className="space-y-1">
                        <Label htmlFor="oee-target" className="text-xs">
                            Target OEE (%) <span className="text-muted-foreground ml-1">— 85% = world-class</span>
                        </Label>
                        <Input
                            id="oee-target"
                            type="number"
                            min={0}
                            max={100}
                            step="1"
                            value={target}
                            onChange={(e) => setTarget(e.target.value)}
                        />
                    </div>
                </div>

                {/* Tag pickers */}
                <div className="space-y-3 pt-3 border-t border-border">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <Info size={12} />
                        Lascia "Fallback" su tutti per usare il calcolo euristico (downtime + quality
                        samples). Configura i tag reali per OEE di produzione vero.
                    </div>

                    <div className="grid gap-3">
                        <div className="space-y-1">
                            <Label htmlFor="oee-run-tag" className="text-xs font-semibold">
                                Availability — Tag "macchina in marcia" (BOOL / 0-1)
                            </Label>
                            {renderTagSelect('oee-run-tag', runTimeTag, setRunTimeTag, 'Nessun tag')}
                            <p className="text-[11px] text-muted-foreground">
                                Quando configurato: % campioni con valore &gt; 0. Altrimenti fallback su downtime allarmi critical.
                            </p>
                        </div>

                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                            <div className="space-y-1">
                                <Label htmlFor="oee-produced-tag" className="text-xs font-semibold">
                                    Performance — Contatore pezzi prodotti
                                </Label>
                                {renderTagSelect('oee-produced-tag', producedTag, setProducedTag, 'Nessun tag')}
                                <p className="text-[11px] text-muted-foreground">
                                    Contatore monotono crescente; il delta nella finestra = pezzi prodotti.
                                </p>
                            </div>
                            <div className="space-y-1">
                                <Label htmlFor="oee-pph" className="text-xs font-semibold">
                                    Target pezzi/ora
                                </Label>
                                <Input
                                    id="oee-pph"
                                    type="number"
                                    min={0}
                                    step="1"
                                    value={pphTarget}
                                    onChange={(e) => setPphTarget(e.target.value)}
                                    placeholder="es. 120"
                                />
                                <p className="text-[11px] text-muted-foreground">
                                    Rate atteso; serve insieme al "produced" per calcolare Performance reale.
                                </p>
                            </div>
                        </div>

                        <div className="space-y-1">
                            <Label htmlFor="oee-good-tag" className="text-xs font-semibold">
                                Quality — Contatore pezzi buoni
                            </Label>
                            {renderTagSelect('oee-good-tag', goodTag, setGoodTag, 'Nessun tag')}
                            <p className="text-[11px] text-muted-foreground">
                                Quando configurato + tag "produced" presente: Quality = good / produced × 100.
                            </p>
                        </div>
                    </div>
                </div>

                {usingFallback && (
                    <div className="text-xs px-3 py-2 rounded border border-dashed border-amber-500/40 bg-amber-500/5 text-amber-500/90">
                        Modalità <strong>fallback</strong> attiva: OEE calcolato da allarmi e quality dei sensori,
                        non da contatori di produzione reali. Va bene per "indicatori di salute" ma non per KPI di produzione.
                    </div>
                )}

                <div className="flex items-center gap-3 pt-3 border-t">
                    <Button onClick={handleSave} disabled={saving}>
                        {saving && <Loader2 size={16} className="mr-2 animate-spin" />}
                        {saving ? 'Saving...' : 'Save'}
                    </Button>
                    {toast && (
                        <span className={`text-sm flex items-center gap-1 ${
                            toast.kind === 'success' ? 'text-emerald-500' : 'text-red-500'
                        }`}>
                            {toast.kind === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                            {toast.text}
                        </span>
                    )}
                </div>
            </CardContent>
        </Card>
    );
};

export default OEESettings;
