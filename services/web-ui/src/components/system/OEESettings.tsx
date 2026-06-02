import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
    Gauge, Loader2, CheckCircle2, AlertCircle, Info, Sparkles, Activity, X,
} from 'lucide-react';

import { systemApi, GlobalSettings } from '@/api/system';
import { oeeApi, OEETagTestResult } from '@/api/dashboard';
import { tagsApi } from '@/api/tags';
import type { Tag } from '@/types';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';

// OEESettings — versione "user-friendly": invece di 3 dropdown grezzi con
// TUTTI i tag mescolati, ognuna delle 3 voci OEE è una piccola card guidata:
//   1. picker filtrato per tipo (BOOL per running, INT/DINT per i counter)
//   2. auto-detect: se l'alias contiene parole tipo "running" / "counter" /
//      "good", il tag viene preselezionato (l'admin può sempre cambiare)
//   3. badge live verde/giallo/rosso che chiama /api/oee/test-tag e dice
//      "questo tag funziona come counter monotono? valore attuale 1234,
//      delta +54 ultimi minuti ✓"
// Il caporeparto vede colonne verde = ok, salva, e basta.

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

const NONE = '0';

// Filtra i tag candidati per ruolo. running = BOOL, counter = INT/DINT/REAL
// (REAL ammesso perché alcuni PLC usano REAL come pseudo-counter cumulativo).
const candidatesFor = (tags: Tag[], role: 'running' | 'counter'): Tag[] => {
    if (role === 'running') {
        return tags.filter((t) => t.data_type === 'BOOL');
    }
    return tags.filter((t) => ['INT', 'DINT', 'REAL'].includes(t.data_type));
};

// Auto-detect: cerca un tag dall'alias/codice. Le parole sono in IT e EN
// perché in fabbrica si trovano nomi misti.
const autoDetect = (tags: Tag[], keywords: string[]): number => {
    for (const kw of keywords) {
        const found = tags.find((t) => {
            const s = `${t.alias ?? ''} ${t.code}`.toLowerCase();
            return s.includes(kw);
        });
        if (found) return found.id;
    }
    return 0;
};

// Piccola "card-row" per un singolo ruolo OEE (running / produced / good).
// Mostra: descrizione, picker filtrato, valore live, verdetto.
const TagSlot = ({
    title, hint, role, allTags, currentId, onChange, keywords, optional = false,
}: {
    title: string;
    hint: string;
    role: 'running' | 'counter';
    allTags: Tag[];
    currentId: string;
    onChange: (v: string) => void;
    keywords: string[];
    optional?: boolean;
}) => {
    const candidates = useMemo(() => candidatesFor(allTags, role), [allTags, role]);

    // Auto-detect quando l'admin apre per la prima volta e non c'è nulla.
    useEffect(() => {
        if (currentId !== NONE) return;
        const guess = autoDetect(candidates, keywords);
        if (guess > 0) onChange(String(guess));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [candidates.length]);

    // Test live: solo se un tag reale è selezionato.
    const tagId = parseInt(currentId, 10);
    const { data: testRes, isFetching } = useQuery({
        queryKey: ['oee-test', tagId, role],
        queryFn: () => oeeApi.testTag(tagId, role),
        enabled: tagId > 0,
        refetchInterval: 15_000,
    });

    return (
        <div className="border border-border rounded-md p-3 space-y-2">
            <div className="flex items-baseline justify-between gap-2">
                <div>
                    <p className="text-sm font-semibold">{title}
                        {optional && <span className="text-[10px] text-muted-foreground ml-2 font-normal">(opzionale)</span>}
                    </p>
                    <p className="text-[11px] text-muted-foreground">{hint}</p>
                </div>
                {currentId !== NONE && (
                    <button
                        type="button"
                        onClick={() => onChange(NONE)}
                        className="text-[10px] text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
                    >
                        <X size={10} /> rimuovi
                    </button>
                )}
            </div>

            <Select value={currentId} onValueChange={onChange}>
                <SelectTrigger>
                    <SelectValue placeholder={`Scegli tag ${role === 'running' ? 'BOOL' : 'numerico'}`} />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value={NONE}>
                        <span className="text-muted-foreground">— Non configurato (usa fallback) —</span>
                    </SelectItem>
                    {candidates.length === 0 && (
                        <SelectItem value="__none" disabled>
                            Nessun tag {role === 'running' ? 'BOOL' : 'INT/DINT/REAL'} disponibile
                        </SelectItem>
                    )}
                    {candidates.map((t) => (
                        <SelectItem key={t.id} value={String(t.id)}>
                            <span className="font-medium">{t.alias || t.code}</span>
                            <span className="text-muted-foreground ml-2 text-xs">({t.data_type})</span>
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>

            {/* Verdetto live */}
            {tagId > 0 && (
                <TagVerdict role={role} result={testRes} loading={isFetching} />
            )}
        </div>
    );
};

const TagVerdict = ({
    role, result, loading,
}: { role: 'running' | 'counter'; result?: OEETagTestResult; loading: boolean }) => {
    if (loading && !result) {
        return (
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Loader2 size={12} className="animate-spin" /> Verifico…
            </div>
        );
    }
    if (!result) return null;

    const okClass    = 'border-emerald-500/40 bg-emerald-500/5 text-emerald-500';
    const warnClass  = 'border-amber-500/40 bg-amber-500/5 text-amber-500';
    const errClass   = 'border-red-500/40 bg-red-500/5 text-red-500';
    const wrapClass  = result.ok ? okClass : result.warnings.length > 0 ? warnClass : errClass;

    return (
        <div className={`rounded border p-2 space-y-1 text-xs ${wrapClass}`}>
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-1.5">
                    {result.ok
                        ? <CheckCircle2 size={14} />
                        : <AlertCircle size={14} />}
                    <span className="font-semibold">
                        {result.ok
                            ? (role === 'running' ? 'BOOL valido — il tag oscilla 0/1' : 'Counter valido — monotono crescente')
                            : (role === 'running' ? 'Non sembra un BOOL on/off' : 'Non sembra un counter')}
                    </span>
                </div>
                <span className="font-mono text-[10px] opacity-70">
                    {result.samples_count} campioni
                </span>
            </div>
            <div className="flex items-center gap-3 text-[11px] font-mono opacity-90">
                <span>ora: <strong>{result.current_value.toFixed(role === 'running' ? 0 : 1)}</strong></span>
                {role === 'counter' && (
                    <>
                        <span>min: {result.min_value.toFixed(0)}</span>
                        <span>max: {result.max_value.toFixed(0)}</span>
                        <span>Δ: {result.delta >= 0 ? '+' : ''}{result.delta.toFixed(0)}</span>
                    </>
                )}
                {role === 'running' && (
                    <span>range: {result.min_value.toFixed(0)}–{result.max_value.toFixed(0)}</span>
                )}
            </div>
            {result.warnings.map((w, i) => (
                <p key={i} className="text-[11px] opacity-85">⚠ {w}</p>
            ))}
        </div>
    );
};

const OEESettings = ({ initial, onSaved }: Props) => {
    const [windowMin, setWindowMin]     = useState('480');
    const [target, setTarget]           = useState('85');
    const [runTimeTag, setRunTimeTag]   = useState(NONE);
    const [producedTag, setProducedTag] = useState(NONE);
    const [goodTag, setGoodTag]         = useState(NONE);
    const [pphTarget, setPphTarget]     = useState('0');
    const [saving, setSaving]           = useState(false);
    const [toast, setToast]             = useState<Toast>(null);

    useEffect(() => {
        if (!initial) return;
        setWindowMin(initial.oee_window_minutes ?? '480');
        setTarget(initial.oee_target ?? '85');
        setRunTimeTag(initial.oee_run_time_tag ?? NONE);
        setProducedTag(initial.oee_produced_tag ?? NONE);
        setGoodTag(initial.oee_good_tag ?? NONE);
        setPphTarget(initial.oee_target_pieces_per_hour ?? '0');
    }, [initial]);

    const { data: tags } = useQuery({
        queryKey: ['oee-tags-picker'],
        queryFn: () => tagsApi.getAllTags(),
    });
    const allTags = tags ?? [];

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

    const usingFallback =
        runTimeTag === NONE && producedTag === NONE && goodTag === NONE;

    const configuredCount =
        (runTimeTag !== NONE ? 1 : 0) +
        (producedTag !== NONE ? 1 : 0) +
        (goodTag !== NONE ? 1 : 0);

    return (
        <Card className="border-border shadow-sm bg-card">
            <CardHeader className="pb-4 border-b border-border">
                <div className="flex items-center gap-3">
                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                        <Gauge className="h-4 w-4 text-primary" />
                    </div>
                    <div className="flex-1">
                        <CardTitle className="text-base text-foreground flex items-center gap-2">
                            OEE — Overall Equipment Effectiveness
                            <span className="text-xs font-normal text-muted-foreground">
                                ({configuredCount}/3 tag configurati)
                            </span>
                        </CardTitle>
                        <CardDescription className="text-xs mt-0.5">
                            Funziona senza configurazione (fallback euristici). Configura i tag per OEE reale.
                        </CardDescription>
                    </div>
                </div>
            </CardHeader>
            <CardContent className="pt-5 space-y-5">
                {/* Window + target — header semplice */}
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div className="space-y-1">
                        <Label className="text-xs">Finestra di calcolo</Label>
                        <Select value={windowMin} onValueChange={setWindowMin}>
                            <SelectTrigger>
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {WINDOW_PRESETS.map((p) => (
                                    <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                    <div className="space-y-1">
                        <Label htmlFor="oee-target" className="text-xs">
                            Target OEE (%) <span className="text-muted-foreground ml-1">— 85 = world-class</span>
                        </Label>
                        <Input
                            id="oee-target"
                            type="number" min={0} max={100} step="1"
                            value={target}
                            onChange={(e) => setTarget(e.target.value)}
                        />
                    </div>
                </div>

                {/* Banner auto-detect */}
                {usingFallback && allTags.length > 0 && (
                    <div className="flex items-start gap-2 text-xs px-3 py-2 rounded border border-primary/20 bg-primary/5 text-foreground">
                        <Sparkles size={14} className="text-primary mt-0.5 flex-shrink-0" />
                        <span>
                            Selezionerò automaticamente i tag se trovo alias che contengono parole come
                            <code className="mx-1 px-1 bg-muted rounded">running</code>,
                            <code className="mx-1 px-1 bg-muted rounded">counter</code>,
                            <code className="mx-1 px-1 bg-muted rounded">good</code>.
                            Puoi sempre cambiare a mano.
                        </span>
                    </div>
                )}

                {/* 3 tag slot con verdetto live */}
                <div className="space-y-3 pt-2 border-t border-border">
                    <h4 className="text-sm font-semibold flex items-center gap-2">
                        <Activity size={14} /> Tag di produzione
                    </h4>

                    <TagSlot
                        title="1. Macchina in marcia"
                        hint="Tag BOOL del PLC che dice se la linea sta lavorando (per Availability)."
                        role="running"
                        allTags={allTags}
                        currentId={runTimeTag}
                        onChange={setRunTimeTag}
                        keywords={['running', 'marcia', 'run_state', 'in_marcia', 'machine_run', 'plc_run']}
                    />

                    <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                        <div className="md:col-span-2">
                            <TagSlot
                                title="2. Contatore pezzi prodotti"
                                hint="Counter monotono crescente: delta sulla finestra = pezzi prodotti (per Performance)."
                                role="counter"
                                allTags={allTags}
                                currentId={producedTag}
                                onChange={setProducedTag}
                                keywords={['produced', 'pezzi_prodotti', 'counter', 'pieces_count', 'production_count', 'totalcount']}
                            />
                        </div>
                        <div className="space-y-1">
                            <Label htmlFor="oee-pph" className="text-xs font-semibold">Target pezzi/ora</Label>
                            <Input
                                id="oee-pph"
                                type="number" min={0} step="1"
                                value={pphTarget}
                                onChange={(e) => setPphTarget(e.target.value)}
                                placeholder="es. 120"
                            />
                            <p className="text-[10px] text-muted-foreground">
                                Rate atteso; serve col tag "produced" per la Performance reale.
                            </p>
                        </div>
                    </div>

                    <TagSlot
                        title="3. Contatore pezzi buoni"
                        hint="Counter monotono dei pezzi conformi (per Quality = good / produced)."
                        role="counter"
                        allTags={allTags}
                        currentId={goodTag}
                        onChange={setGoodTag}
                        keywords={['good', 'pezzi_buoni', 'ok_count', 'conformi', 'good_pieces', 'goodpiecescount']}
                        optional
                    />
                </div>

                {usingFallback && (
                    <div className="flex items-start gap-2 text-xs px-3 py-2 rounded border border-dashed border-amber-500/40 bg-amber-500/5 text-amber-500/90">
                        <Info size={14} className="mt-0.5 flex-shrink-0" />
                        <span>
                            Modalità <strong>fallback</strong>: OEE calcolato da allarmi + quality dei sensori.
                            Va bene come "indicatore di salute", non come KPI di produzione reale.
                        </span>
                    </div>
                )}

                <div className="flex items-center gap-3 pt-3 border-t">
                    <Button onClick={handleSave} disabled={saving}>
                        {saving && <Loader2 size={16} className="mr-2 animate-spin" />}
                        {saving ? 'Saving...' : 'Salva configurazione'}
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
