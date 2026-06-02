import { useEffect, useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
    Gauge, Plus, Pencil, Trash2, Sparkles, CheckCircle2, AlertCircle,
    Loader2, X, Activity, Power, PowerOff,
} from 'lucide-react';
import { toast } from 'sonner';

import {
    oeeApi, OEEProfile, OEEProfileRequest, OEETagTestResult,
} from '@/api/dashboard';
import { areasApi } from '@/api/areas';
import { tagsApi } from '@/api/tags';
import type { Tag } from '@/types';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription,
} from '@/components/ui/dialog';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs';
import { OEEHistoryChart } from '@/components/oee/OEEHistoryChart';
import { OEEShiftMatrix } from '@/components/oee/OEEShiftMatrix';

// OEEProfilesPage — gestione multi-profilo OEE.
// Vista a 2 livelli: lista profili (table) + dialog editor (wizard riusato
// dal pannello System → OEE legacy).
//
// Concetto: ogni profilo è una "unità di misura OEE" indipendente — una
// linea, una macchina, un reparto. La dashboard mostra una card per profilo
// abilitato + un rollup di fabbrica.

const WINDOW_PRESETS = [
    { value: 60,   label: '1 ora' },
    { value: 240,  label: '4 ore' },
    { value: 480,  label: '8 ore (un turno)' },
    { value: 720,  label: '12 ore' },
    { value: 1440, label: '24 ore' },
];

const candidatesFor = (tags: Tag[], role: 'running' | 'counter'): Tag[] => {
    if (role === 'running') return tags.filter((t) => t.data_type === 'BOOL');
    return tags.filter((t) => ['INT', 'DINT', 'REAL'].includes(t.data_type));
};

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

// ────────────────────────────────────────────────────────────────────────
// Tag picker con verdetto live + auto-detect (replicato da OEESettings).
// ────────────────────────────────────────────────────────────────────────
const TagSlot = ({
    title, hint, role, allTags, currentId, onChange, keywords, optional = false,
}: {
    title: string;
    hint: string;
    role: 'running' | 'counter';
    allTags: Tag[];
    currentId: number | null;
    onChange: (v: number | null) => void;
    keywords: string[];
    optional?: boolean;
}) => {
    const candidates = useMemo(() => candidatesFor(allTags, role), [allTags, role]);

    // Auto-detect alla prima apertura senza tag.
    useEffect(() => {
        if (currentId !== null && currentId > 0) return;
        const guess = autoDetect(candidates, keywords);
        if (guess > 0) onChange(guess);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [candidates.length]);

    const tagId = currentId ?? 0;
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
                    <p className="text-sm font-semibold">
                        {title}
                        {optional && <span className="text-[10px] text-muted-foreground ml-2 font-normal">(opzionale)</span>}
                    </p>
                    <p className="text-[11px] text-muted-foreground">{hint}</p>
                </div>
                {tagId > 0 && (
                    <button
                        type="button"
                        onClick={() => onChange(null)}
                        className="text-[10px] text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
                    >
                        <X size={10} /> rimuovi
                    </button>
                )}
            </div>

            <Select
                value={tagId > 0 ? String(tagId) : 'none'}
                onValueChange={(v) => onChange(v === 'none' ? null : parseInt(v, 10))}
            >
                <SelectTrigger>
                    <SelectValue placeholder={`Scegli tag ${role === 'running' ? 'BOOL' : 'numerico'}`} />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="none">
                        <span className="text-muted-foreground">— Non configurato (usa fallback) —</span>
                    </SelectItem>
                    {candidates.map((t) => (
                        <SelectItem key={t.id} value={String(t.id)}>
                            <span className="font-medium">{t.alias || t.code}</span>
                            <span className="text-muted-foreground ml-2 text-xs">({t.data_type})</span>
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>

            {tagId > 0 && <TagVerdict role={role} result={testRes} loading={isFetching} />}
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
    const okClass   = 'border-emerald-500/40 bg-emerald-500/5 text-emerald-500';
    const warnClass = 'border-amber-500/40 bg-amber-500/5 text-amber-500';
    const errClass  = 'border-red-500/40 bg-red-500/5 text-red-500';
    const wrap = result.ok ? okClass : result.warnings.length > 0 ? warnClass : errClass;

    return (
        <div className={`rounded border p-2 space-y-1 text-xs ${wrap}`}>
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-1.5">
                    {result.ok ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                    <span className="font-semibold">
                        {result.ok
                            ? (role === 'running' ? 'BOOL valido' : 'Counter monotono')
                            : (role === 'running' ? 'Non sembra BOOL' : 'Non sembra counter')}
                    </span>
                </div>
                <span className="font-mono text-[10px] opacity-70">{result.samples_count} campioni</span>
            </div>
            <div className="flex items-center gap-3 text-[11px] font-mono opacity-90">
                <span>ora: <strong>{result.current_value.toFixed(role === 'running' ? 0 : 1)}</strong></span>
                {role === 'counter' && (
                    <>
                        <span>Δ: {result.delta >= 0 ? '+' : ''}{result.delta.toFixed(0)}</span>
                    </>
                )}
            </div>
            {result.warnings.map((w, i) => (<p key={i} className="text-[11px] opacity-85">⚠ {w}</p>))}
        </div>
    );
};

// ────────────────────────────────────────────────────────────────────────
// Editor dialog — create/edit di un profilo.
// ────────────────────────────────────────────────────────────────────────
const ProfileEditor = ({
    open, onClose, initial, allTags, areas, onSaved,
}: {
    open: boolean;
    onClose: () => void;
    initial: OEEProfile | null;
    allTags: Tag[];
    areas: { id: number; name: string }[];
    onSaved: () => void;
}) => {
    const [name, setName]                 = useState('');
    const [description, setDescription]   = useState('');
    const [areaId, setAreaId]             = useState<number | null>(null);
    const [runId, setRunId]               = useState<number | null>(null);
    const [prodId, setProdId]             = useState<number | null>(null);
    const [goodId, setGoodId]             = useState<number | null>(null);
    const [pph, setPph]                   = useState(0);
    const [windowMin, setWindowMin]       = useState(480);
    const [targetOEE, setTargetOEE]       = useState(85);
    const [enabled, setEnabled]           = useState(true);
    const [respectShifts, setRespectShifts]           = useState(true);
    const [respectMaintenance, setRespectMaintenance] = useState(true);
    const [saving, setSaving]             = useState(false);

    useEffect(() => {
        if (!open) return;
        if (initial) {
            setName(initial.name);
            setDescription(initial.description ?? '');
            setAreaId(initial.area_id ?? null);
            setRunId(initial.run_time_tag_id ?? null);
            setProdId(initial.produced_tag_id ?? null);
            setGoodId(initial.good_tag_id ?? null);
            setPph(initial.target_pieces_per_hour);
            setWindowMin(initial.window_minutes);
            setTargetOEE(initial.target_oee);
            setEnabled(initial.enabled);
            setRespectShifts(initial.respect_shifts ?? true);
            setRespectMaintenance(initial.respect_maintenance ?? true);
        } else {
            setName(''); setDescription(''); setAreaId(null);
            setRunId(null); setProdId(null); setGoodId(null);
            setPph(0); setWindowMin(480); setTargetOEE(85); setEnabled(true);
            setRespectShifts(true); setRespectMaintenance(true);
        }
    }, [open, initial]);

    const handleSave = async () => {
        if (!name.trim()) {
            toast.error('Il nome è obbligatorio.');
            return;
        }
        setSaving(true);
        try {
            const payload: OEEProfileRequest = {
                name: name.trim(),
                description: description.trim() || undefined,
                area_id: areaId,
                run_time_tag_id: runId,
                produced_tag_id: prodId,
                good_tag_id: goodId,
                target_pieces_per_hour: pph,
                window_minutes: windowMin,
                target_oee: targetOEE,
                display_order: initial?.display_order ?? 0,
                enabled,
                respect_shifts: respectShifts,
                respect_maintenance: respectMaintenance,
            };
            if (initial) {
                await oeeApi.updateProfile(initial.id, payload);
                toast.success('Profilo OEE aggiornato.');
            } else {
                await oeeApi.createProfile(payload);
                toast.success('Profilo OEE creato.');
            }
            onSaved();
            onClose();
        } catch (e: unknown) {
            toast.error(`Save failed: ${(e as Error)?.message ?? 'unknown'}`);
        } finally {
            setSaving(false);
        }
    };

    const usingFallback = !runId && !prodId && !goodId;

    return (
        <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
            <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <Gauge size={18} /> {initial ? 'Modifica profilo OEE' : 'Nuovo profilo OEE'}
                    </DialogTitle>
                    <DialogDescription>
                        Un profilo OEE è un'unità di misura indipendente (una linea, una macchina, un reparto).
                        Lascia i tag a "Non configurato" per usare il fallback euristico.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4 py-3">
                    {/* Nome + area */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <Label>Nome *</Label>
                            <Input
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                                placeholder="es. Linea Assemblaggio 1"
                            />
                        </div>
                        <div className="space-y-1">
                            <Label>Area (opzionale)</Label>
                            <Select
                                value={areaId ? String(areaId) : 'none'}
                                onValueChange={(v) => setAreaId(v === 'none' ? null : parseInt(v, 10))}
                            >
                                <SelectTrigger><SelectValue placeholder="Nessuna" /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="none">— Nessuna area —</SelectItem>
                                    {areas.map((a) => (
                                        <SelectItem key={a.id} value={String(a.id)}>{a.name}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    <div className="space-y-1">
                        <Label>Descrizione</Label>
                        <Input
                            value={description}
                            onChange={(e) => setDescription(e.target.value)}
                            placeholder="es. Linea principale assemblaggio motori, turno 8h"
                        />
                    </div>

                    {/* Window + target */}
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                        <div className="space-y-1">
                            <Label>Finestra di calcolo</Label>
                            <Select value={String(windowMin)} onValueChange={(v) => setWindowMin(parseInt(v, 10))}>
                                <SelectTrigger><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    {WINDOW_PRESETS.map((p) => (
                                        <SelectItem key={p.value} value={String(p.value)}>{p.label}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-1">
                            <Label>Target OEE (%)</Label>
                            <Input
                                type="number" min={0} max={100} step={1}
                                value={targetOEE}
                                onChange={(e) => setTargetOEE(parseFloat(e.target.value) || 0)}
                            />
                        </div>
                        <div className="flex items-end justify-between gap-2 pb-2">
                            <div className="flex items-center gap-2">
                                <Switch checked={enabled} onCheckedChange={setEnabled} />
                                <Label className="cursor-pointer">Attivo</Label>
                            </div>
                            <p className="text-[10px] text-muted-foreground text-right">
                                Disattivato = non appare in dashboard
                            </p>
                        </div>
                    </div>

                    {/* Planned Production Time — toggles per Availability "vera"
                        (esclude tempo fuori turno + finestre di manutenzione) */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3 p-3 border border-dashed border-border rounded-md bg-muted/20">
                        <div className="flex items-start gap-3">
                            <Switch
                                id="respect-shifts"
                                checked={respectShifts}
                                onCheckedChange={setRespectShifts}
                            />
                            <div>
                                <Label htmlFor="respect-shifts" className="cursor-pointer">
                                    Considera turni
                                </Label>
                                <p className="text-[11px] text-muted-foreground">
                                    Availability divide per Planned Production Time, non per wall clock.
                                    Linee ferme di domenica non vengono penalizzate.
                                </p>
                            </div>
                        </div>
                        <div className="flex items-start gap-3">
                            <Switch
                                id="respect-maint"
                                checked={respectMaintenance}
                                onCheckedChange={setRespectMaintenance}
                            />
                            <div>
                                <Label htmlFor="respect-maint" className="cursor-pointer">
                                    Considera manutenzioni
                                </Label>
                                <p className="text-[11px] text-muted-foreground">
                                    Finestre di manutenzione programmata sottratte dal PPT — non
                                    abbassano l'OEE.
                                </p>
                            </div>
                        </div>
                    </div>

                    {/* Banner auto-detect quando tutto è ancora vuoto */}
                    {usingFallback && allTags.length > 0 && (
                        <div className="flex items-start gap-2 text-xs px-3 py-2 rounded border border-primary/20 bg-primary/5">
                            <Sparkles size={14} className="text-primary mt-0.5 flex-shrink-0" />
                            <span>
                                Auto-detect: cerco tag con alias che contengono
                                <code className="mx-1 px-1 bg-muted rounded">running</code> /
                                <code className="mx-1 px-1 bg-muted rounded">counter</code> /
                                <code className="mx-1 px-1 bg-muted rounded">good</code>.
                            </span>
                        </div>
                    )}

                    {/* 3 tag slot */}
                    <div className="space-y-3 pt-2 border-t border-border">
                        <h4 className="text-sm font-semibold flex items-center gap-2">
                            <Activity size={14} /> Tag di produzione
                        </h4>

                        <TagSlot
                            title="1. Macchina in marcia"
                            hint="Tag BOOL del PLC per Availability."
                            role="running"
                            allTags={allTags}
                            currentId={runId}
                            onChange={setRunId}
                            keywords={[
                                // IT — comuni in fabbriche italiane
                                'marcia', 'in_marcia', 'macchina_on', 'linea_on', 'stato_macchina', 'stato', 'attiva', 'attivo', 'avvio', 'start',
                                // EN — convenzioni internazionali
                                'running', 'run_state', 'machine_run', 'plc_run', 'is_running', 'in_run', 'run_ok', 'enable',
                                // Abbreviazioni PLC
                                's1', 's2', 'st1', 'st2', 'st_run', 'on_off',
                            ]}
                        />

                        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                            <div className="md:col-span-2">
                                <TagSlot
                                    title="2. Contatore pezzi prodotti"
                                    hint="Counter monotono crescente per Performance."
                                    role="counter"
                                    allTags={allTags}
                                    currentId={prodId}
                                    onChange={setProdId}
                                    keywords={[
                                        // IT
                                        'pezzi_prodotti', 'totalizzatore', 'totale', 'produzione', 'prodotti', 'pz_tot', 'tot_pz', 'pz_prodotti',
                                        // EN
                                        'produced', 'production_count', 'pieces_count', 'piece_count', 'totalcount', 'total_pieces', 'pcs_produced',
                                        // Abbreviazioni
                                        'cont', 'counter', 'count', 'tot', 'pcs', 'pz', 'qty_tot',
                                    ]}
                                />
                            </div>
                            <div className="space-y-1">
                                <Label>Target pezzi/ora</Label>
                                <Input
                                    type="number" min={0} step={1}
                                    value={pph}
                                    onChange={(e) => setPph(parseFloat(e.target.value) || 0)}
                                    placeholder="es. 120"
                                />
                                <p className="text-[10px] text-muted-foreground">
                                    Rate atteso (richiede tag produced).
                                </p>
                            </div>
                        </div>

                        <TagSlot
                            title="3. Contatore pezzi buoni"
                            hint="Counter dei conformi per Quality = good / produced."
                            role="counter"
                            allTags={allTags}
                            currentId={goodId}
                            onChange={setGoodId}
                            keywords={[
                                // IT
                                'pezzi_buoni', 'buoni', 'conformi', 'pz_ok', 'ok_pz', 'pz_buoni', 'buoni_ok', 'qualita_ok',
                                // EN
                                'good', 'good_pieces', 'good_count', 'goodpiecescount', 'good_pcs', 'pcs_good', 'ok_count', 'pass_count',
                                // Abbreviazioni
                                'ok', 'pass',
                            ]}
                            optional
                        />
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={onClose} disabled={saving}>Annulla</Button>
                    <Button onClick={handleSave} disabled={saving}>
                        {saving && <Loader2 size={14} className="mr-1 animate-spin" />}
                        {initial ? 'Salva modifiche' : 'Crea profilo'}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
};

// ────────────────────────────────────────────────────────────────────────
// Pagina principale: lista profili.
// ────────────────────────────────────────────────────────────────────────
const OEEProfilesPage = () => {
    const [editorOpen, setEditorOpen]   = useState(false);
    const [editing, setEditing]         = useState<OEEProfile | null>(null);
    const queryClient = useQueryClient();

    const { data: profiles, isLoading } = useQuery({
        queryKey: ['oee-profiles'],
        queryFn: oeeApi.listProfiles,
    });

    const { data: tags } = useQuery({
        queryKey: ['all-tags'],
        queryFn: () => tagsApi.getAllTags(),
    });

    const { data: areas } = useQuery({
        queryKey: ['areas-all'],
        queryFn: () => areasApi.getAll(),
    });

    const handleDelete = async (p: OEEProfile) => {
        if (!confirm(`Eliminare il profilo "${p.name}"? L'azione è irreversibile.`)) return;
        try {
            await oeeApi.deleteProfile(p.id);
            toast.success('Profilo eliminato.');
            queryClient.invalidateQueries({ queryKey: ['oee-profiles'] });
            queryClient.invalidateQueries({ queryKey: ['oee-snapshot'] });
        } catch (e: unknown) {
            toast.error(`Errore: ${(e as Error)?.message ?? 'unknown'}`);
        }
    };

    const handleToggle = async (p: OEEProfile) => {
        try {
            await oeeApi.updateProfile(p.id, {
                name: p.name,
                description: p.description,
                area_id: p.area_id ?? null,
                gateway_id: p.gateway_id ?? null,
                run_time_tag_id: p.run_time_tag_id ?? null,
                produced_tag_id: p.produced_tag_id ?? null,
                good_tag_id: p.good_tag_id ?? null,
                target_pieces_per_hour: p.target_pieces_per_hour,
                window_minutes: p.window_minutes,
                target_oee: p.target_oee,
                display_order: p.display_order,
                enabled: !p.enabled,
            });
            queryClient.invalidateQueries({ queryKey: ['oee-profiles'] });
            queryClient.invalidateQueries({ queryKey: ['dashboard-overview'] });
        } catch (e: unknown) {
            toast.error(`Errore: ${(e as Error)?.message ?? 'unknown'}`);
        }
    };

    const onSaved = () => {
        queryClient.invalidateQueries({ queryKey: ['oee-profiles'] });
        queryClient.invalidateQueries({ queryKey: ['dashboard-overview'] });
    };

    return (
        <div className="space-y-4">
            <div className="flex items-start justify-between">
                <div>
                    <h2 className="text-3xl font-bold tracking-tight flex items-center gap-2">
                        <Gauge size={28} /> Profili OEE
                    </h2>
                    <p className="text-muted-foreground">
                        Multi-linea / multi-reparto. Ogni profilo è un'unità di misura OEE indipendente
                        che appare in dashboard come card separata.
                    </p>
                </div>
                <Button onClick={() => { setEditing(null); setEditorOpen(true); }}>
                    <Plus size={16} className="mr-2" /> Nuovo profilo
                </Button>
            </div>

            <Tabs defaultValue="profiles">
                <TabsList>
                    <TabsTrigger value="profiles">Profili</TabsTrigger>
                    <TabsTrigger value="history">Storia</TabsTrigger>
                    <TabsTrigger value="by-shift">Per turno</TabsTrigger>
                </TabsList>

                <TabsContent value="profiles" className="space-y-4 mt-4">
                    <Card>
                        <CardHeader className="pb-3">
                            <CardTitle className="text-base">Profili configurati</CardTitle>
                            <CardDescription className="text-xs">
                                {profiles?.length ?? 0} profili.
                                {(profiles?.length ?? 0) === 0 && ' Senza profili la dashboard usa il calcolo OEE legacy (fallback euristico).'}
                            </CardDescription>
                        </CardHeader>
                        <CardContent>
                            {isLoading ? (
                                <div className="py-8 text-center text-muted-foreground text-sm">Caricamento…</div>
                            ) : (profiles?.length ?? 0) === 0 ? (
                                <div className="py-12 text-center border border-dashed rounded-md">
                                    <Gauge size={32} className="mx-auto opacity-30 mb-2" />
                                    <p className="text-sm text-muted-foreground">Nessun profilo OEE configurato.</p>
                                    <p className="text-xs text-muted-foreground mt-1">
                                        Crea il primo profilo per attivare la dashboard multi-linea.
                                    </p>
                                </div>
                            ) : (
                                <div className="space-y-2">
                                    {profiles!.map((p) => (
                                        <ProfileRow
                                            key={p.id}
                                            p={p}
                                            onEdit={() => { setEditing(p); setEditorOpen(true); }}
                                            onDelete={() => handleDelete(p)}
                                            onToggle={() => handleToggle(p)}
                                        />
                                    ))}
                                </div>
                            )}
                        </CardContent>
                    </Card>
                </TabsContent>

                <TabsContent value="history" className="space-y-4 mt-4">
                    <HistoryTab profiles={profiles ?? []} />
                </TabsContent>

                <TabsContent value="by-shift" className="space-y-4 mt-4">
                    <ByShiftTab profiles={profiles ?? []} />
                </TabsContent>
            </Tabs>

            <ProfileEditor
                open={editorOpen}
                onClose={() => setEditorOpen(false)}
                initial={editing}
                allTags={tags ?? []}
                areas={areas ?? []}
                onSaved={onSaved}
            />
        </div>
    );
};

// Tab Storia: selector profilo + chart storico via OEEHistoryChart.
// Profilo "Rollup fabbrica" = profileId null (riga aggregata del cron).
const HistoryTab = ({ profiles }: { profiles: OEEProfile[] }) => {
    const [selected, setSelected] = useState<number | null>(null);
    const target = selected === null
        ? undefined
        : profiles.find((p) => p.id === selected)?.target_oee;

    return (
        <div className="space-y-3">
            <div className="flex items-center gap-3 flex-wrap">
                <Label className="text-xs">Profilo:</Label>
                <Select
                    value={selected === null ? 'rollup' : String(selected)}
                    onValueChange={(v) => setSelected(v === 'rollup' ? null : parseInt(v, 10))}
                >
                    <SelectTrigger className="w-72"><SelectValue /></SelectTrigger>
                    <SelectContent>
                        <SelectItem value="rollup">
                            <span className="font-medium">Rollup Fabbrica</span>
                            <span className="text-muted-foreground ml-2 text-xs">(media tra profili)</span>
                        </SelectItem>
                        {profiles.map((p) => (
                            <SelectItem key={p.id} value={String(p.id)}>
                                {p.name}
                                {p.area_name && <span className="text-muted-foreground ml-2 text-xs">[{p.area_name}]</span>}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                <p className="text-[11px] text-muted-foreground">
                    Snapshot orari salvati dal cron worker. Primi dati disponibili dopo 1 ora di runtime.
                </p>
            </div>

            <OEEHistoryChart profileId={selected} target={target} />
        </div>
    );
};

// Tab Per turno: matrice turni × giorni con OEE colorato.
const ByShiftTab = ({ profiles }: { profiles: OEEProfile[] }) => {
    const [selected, setSelected] = useState<number | null>(null);
    return (
        <div className="space-y-3">
            <div className="flex items-center gap-3 flex-wrap">
                <Label className="text-xs">Profilo:</Label>
                <Select
                    value={selected === null ? 'rollup' : String(selected)}
                    onValueChange={(v) => setSelected(v === 'rollup' ? null : parseInt(v, 10))}
                >
                    <SelectTrigger className="w-72"><SelectValue /></SelectTrigger>
                    <SelectContent>
                        <SelectItem value="rollup">
                            <span className="font-medium">Rollup Fabbrica</span>
                        </SelectItem>
                        {profiles.map((p) => (
                            <SelectItem key={p.id} value={String(p.id)}>
                                {p.name}
                                {p.area_name && <span className="text-muted-foreground ml-2 text-xs">[{p.area_name}]</span>}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                <p className="text-[11px] text-muted-foreground">
                    Confronto OEE tra turni — la KPI più richiesta dalla direzione.
                    I dati appaiono dopo che il cron ha popolato almeno un'ora dentro un turno attivo.
                </p>
            </div>

            <OEEShiftMatrix profileId={selected} />
        </div>
    );
};

const ProfileRow = ({
    p, onEdit, onDelete, onToggle,
}: { p: OEEProfile; onEdit: () => void; onDelete: () => void; onToggle: () => void }) => {
    const tagsConfigured =
        (p.run_time_tag_id ? 1 : 0) +
        (p.produced_tag_id ? 1 : 0) +
        (p.good_tag_id ? 1 : 0);
    const allFallback = tagsConfigured === 0;

    return (
        <div className={`flex items-center gap-3 p-3 border rounded-md hover:bg-muted/30 transition-colors ${
            !p.enabled ? 'opacity-50' : ''
        }`}>
            <div className={`p-2 rounded-md ${allFallback ? 'bg-amber-500/10' : 'bg-emerald-500/10'}`}>
                <Gauge size={18} className={allFallback ? 'text-amber-500' : 'text-emerald-500'} />
            </div>
            <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-semibold">{p.name}</span>
                    {p.area_name && (
                        <Badge variant="outline" className="text-[10px]">{p.area_name}</Badge>
                    )}
                    {allFallback ? (
                        <Badge variant="outline" className="text-[10px] border-amber-500/40 text-amber-500">fallback</Badge>
                    ) : (
                        <Badge variant="outline" className="text-[10px] border-emerald-500/40 text-emerald-500">
                            {tagsConfigured}/3 tag
                        </Badge>
                    )}
                    {!p.enabled && (
                        <Badge variant="outline" className="text-[10px] border-muted">disabilitato</Badge>
                    )}
                </div>
                {p.description && <p className="text-xs text-muted-foreground truncate">{p.description}</p>}
                <p className="text-[10px] text-muted-foreground mt-0.5">
                    Finestra {(p.window_minutes / 60).toFixed(0)}h · Target {p.target_oee}%
                    {p.target_pieces_per_hour > 0 && ` · ${p.target_pieces_per_hour} pz/h`}
                </p>
            </div>
            <div className="flex items-center gap-1">
                <Button variant="ghost" size="icon" onClick={onToggle} title={p.enabled ? 'Disabilita' : 'Abilita'}>
                    {p.enabled ? <Power size={16} /> : <PowerOff size={16} />}
                </Button>
                <Button variant="ghost" size="icon" onClick={onEdit}>
                    <Pencil size={16} />
                </Button>
                <Button variant="ghost" size="icon" onClick={onDelete} className="text-red-500 hover:text-red-600">
                    <Trash2 size={16} />
                </Button>
            </div>
        </div>
    );
};

export default OEEProfilesPage;
