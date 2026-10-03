import { useEffect, useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
    Gauge, Plus, Pencil, Trash2, Sparkles, CheckCircle2, AlertCircle,
    Loader2, X, Activity, Power, PowerOff, Monitor, Download,
} from 'lucide-react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
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
import { OEELossPareto } from '@/components/oee/OEELossPareto';
import { OEEAlertRules } from '@/components/oee/OEEAlertRules';
import { OEEHierarchyView } from '@/components/oee/OEEHierarchyView';
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';

// OEEProfilesPage — gestione multi-profilo OEE.
// Vista a 2 livelli: lista profili (table) + dialog editor (wizard riusato
// dal pannello System → OEE legacy).
//
// Concetto: ogni profilo è una "unità di misura OEE" indipendente — una
// linea, una macchina, un reparto. La dashboard mostra una card per profilo
// abilitato + un rollup di fabbrica.

// label = i18n key, resolved with tr() at render.
const WINDOW_PRESETS = [
    { value: 60,   label: 'oeeProfiles.window_1h' },
    { value: 240,  label: 'oeeProfiles.window_4h' },
    { value: 480,  label: 'oeeProfiles.window_8h' },
    { value: 720,  label: 'oeeProfiles.window_12h' },
    { value: 1440, label: 'oeeProfiles.window_24h' },
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
    const { t: tr } = useTranslation();
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
                        {optional && <span className="text-[10px] text-muted-foreground ml-2 font-normal">{tr('oeeProfiles.optional')}</span>}
                    </p>
                    <p className="text-[11px] text-muted-foreground">{hint}</p>
                </div>
                {tagId > 0 && (
                    <button
                        type="button"
                        onClick={() => onChange(null)}
                        className="text-[10px] text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
                    >
                        <X size={10} /> {tr('oeeProfiles.remove')}
                    </button>
                )}
            </div>

            <Select
                value={tagId > 0 ? String(tagId) : 'none'}
                onValueChange={(v) => onChange(v === 'none' ? null : parseInt(v, 10))}
            >
                <SelectTrigger>
                    <SelectValue placeholder={role === 'running' ? tr('oeeProfiles.pick_tag_bool') : tr('oeeProfiles.pick_tag_numeric')} />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="none">
                        <span className="text-muted-foreground">{tr('oeeProfiles.tag_not_configured')}</span>
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
    const { t: tr } = useTranslation();
    if (loading && !result) {
        return (
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Loader2 size={12} className="animate-spin" /> {tr('oeeProfiles.checking')}
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
                            ? (role === 'running' ? tr('oeeProfiles.verdict_bool_ok') : tr('oeeProfiles.verdict_counter_ok'))
                            : (role === 'running' ? tr('oeeProfiles.verdict_bool_bad') : tr('oeeProfiles.verdict_counter_bad'))}
                    </span>
                </div>
                <span className="font-mono text-[10px] opacity-70">{tr('oeeProfiles.samples', { count: result.samples_count })}</span>
            </div>
            <div className="flex items-center gap-3 text-[11px] font-mono opacity-90">
                <span>{tr('oeeProfiles.now')} <strong>{result.current_value.toFixed(role === 'running' ? 0 : 1)}</strong></span>
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
    open, onClose, initial, areas, onSaved,
}: {
    open: boolean;
    onClose: () => void;
    initial: OEEProfile | null;
    areas: { id: number; name: string }[];
    onSaved: () => void;
}) => {
    const { t: tr } = useTranslation();
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

    const { data: filteredTags } = useQuery({
        queryKey: ['tags-area', areaId],
        queryFn: () => tagsApi.getAll(null, areaId),
        enabled: !!areaId,
    });
    const allTags = filteredTags ?? [];

    // Reset the form whenever the dialog opens or the edited profile changes,
    // adjusting state during render rather than in an effect.
    const [syncedWith, setSyncedWith] = useState<{ open: boolean; initial: OEEProfile | null }>({ open: false, initial: null });
    if (syncedWith.open !== open || syncedWith.initial !== initial) {
        setSyncedWith({ open, initial });
        if (open) {
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
        }
    }

    const handleSave = async () => {
        if (!name.trim()) {
            toast.error(tr('oeeProfiles.name_required'));
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
                toast.success(tr('oeeProfiles.updated'));
            } else {
                await oeeApi.createProfile(payload);
                toast.success(tr('oeeProfiles.created'));
            }
            onSaved();
            onClose();
        } catch (e: unknown) {
            toast.error(tr('oeeProfiles.save_failed', { msg: (e as Error)?.message ?? tr('oee.unknown_error') }));
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
                        <Gauge size={18} /> {initial ? tr('oeeProfiles.edit_title') : tr('oeeProfiles.new_title')}
                    </DialogTitle>
                    <DialogDescription>
                        {tr('oeeProfiles.editor_desc')}
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4 py-3">
                    {/* Nome + area */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <Label>{tr('oeeProfiles.name_required_label')}</Label>
                            <Input
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                                placeholder={tr('oeeProfiles.name_placeholder')}
                            />
                        </div>
                        <div className="space-y-1">
                            <Label>{tr('oeeProfiles.area_optional')}</Label>
                            <Select
                                value={areaId ? String(areaId) : 'none'}
                                onValueChange={(v) => setAreaId(v === 'none' ? null : parseInt(v, 10))}
                            >
                                <SelectTrigger><SelectValue placeholder={tr('oeeProfiles.none_f')} /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="none">{tr('oeeProfiles.no_area')}</SelectItem>
                                    {areas.map((a) => (
                                        <SelectItem key={a.id} value={String(a.id)}>{a.name}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    <div className="space-y-1">
                        <Label>{tr('oeeProfiles.description')}</Label>
                        <Input
                            value={description}
                            onChange={(e) => setDescription(e.target.value)}
                            placeholder={tr('oeeProfiles.description_placeholder')}
                        />
                    </div>

                    {/* Window + target */}
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                        <div className="space-y-1">
                            <Label>{tr('oeeProfiles.window')}</Label>
                            <Select value={String(windowMin)} onValueChange={(v) => setWindowMin(parseInt(v, 10))}>
                                <SelectTrigger><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    {WINDOW_PRESETS.map((p) => (
                                        <SelectItem key={p.value} value={String(p.value)}>{tr(p.label)}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-1">
                            <Label>{tr('oeeProfiles.target_oee')}</Label>
                            <Input
                                type="number" min={0} max={100} step={1}
                                value={targetOEE}
                                onChange={(e) => setTargetOEE(parseFloat(e.target.value) || 0)}
                            />
                        </div>
                        <div className="flex items-end justify-between gap-2 pb-2">
                            <div className="flex items-center gap-2">
                                <Switch checked={enabled} onCheckedChange={setEnabled} />
                                <Label className="cursor-pointer">{tr('oeeProfiles.active')}</Label>
                            </div>
                            <p className="text-[10px] text-muted-foreground text-right">
                                {tr('oeeProfiles.active_hint')}
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
                                    {tr('oeeProfiles.respect_shifts')}
                                </Label>
                                <p className="text-[11px] text-muted-foreground">
                                    {tr('oeeProfiles.respect_shifts_hint')}
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
                                    {tr('oeeProfiles.respect_maintenance')}
                                </Label>
                                <p className="text-[11px] text-muted-foreground">
                                    {tr('oeeProfiles.respect_maintenance_hint')}
                                </p>
                            </div>
                        </div>
                    </div>

                    {/* Banner auto-detect quando tutto è ancora vuoto */}
                    {usingFallback && allTags.length > 0 && (
                        <div className="flex items-start gap-2 text-xs px-3 py-2 rounded border border-primary/20 bg-primary/5">
                            <Sparkles size={14} className="text-primary mt-0.5 shrink-0" />
                            <span>
                                {tr('oeeProfiles.autodetect')}
                                <code className="mx-1 px-1 bg-muted rounded">running</code> /
                                <code className="mx-1 px-1 bg-muted rounded">counter</code> /
                                <code className="mx-1 px-1 bg-muted rounded">good</code>.
                            </span>
                        </div>
                    )}

                    {/* 3 tag slot */}
                    <div className="space-y-3 pt-2 border-t border-border">
                        <h4 className="text-sm font-semibold flex items-center gap-2">
                            <Activity size={14} /> {tr('oeeProfiles.production_tags')}
                        </h4>

                        <TagSlot
                            title={tr('oeeProfiles.slot_running')}
                            hint={tr('oeeProfiles.slot_running_hint')}
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
                                    title={tr('oeeProfiles.slot_produced')}
                                    hint={tr('oeeProfiles.slot_produced_hint')}
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
                                <Label>{tr('oeeProfiles.target_pph')}</Label>
                                <Input
                                    type="number" min={0} step={1}
                                    value={pph}
                                    onChange={(e) => setPph(parseFloat(e.target.value) || 0)}
                                    placeholder={tr('oeeProfiles.target_pph_placeholder')}
                                />
                                <p className="text-[10px] text-muted-foreground">
                                    {tr('oeeProfiles.target_pph_hint')}
                                </p>
                            </div>
                        </div>

                        <TagSlot
                            title={tr('oeeProfiles.slot_good')}
                            hint={tr('oeeProfiles.slot_good_hint')}
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
                    <Button variant="outline" onClick={onClose} disabled={saving}>{tr('common.cancel')}</Button>
                    <Button onClick={handleSave} disabled={saving}>
                        {saving && <Loader2 size={14} className="mr-1 animate-spin" />}
                        {initial ? tr('oeeProfiles.save_changes') : tr('oeeProfiles.create_profile')}
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
    const { t: tr } = useTranslation();
    const [editorOpen, setEditorOpen]   = useState(false);
    const [editing, setEditing]         = useState<OEEProfile | null>(null);
    const queryClient = useQueryClient();

    const { data: profiles, isLoading } = useQuery({
        queryKey: ['oee-profiles'],
        queryFn: oeeApi.listProfiles,
    });

    const { data: areas } = useQuery({
        queryKey: ['areas-all'],
        queryFn: () => areasApi.getAll(),
    });

    const handleDelete = async (p: OEEProfile) => {
        if (!(await confirmAction({ title: i18n.t('ask.delete_named', { name: p.name }), description: i18n.t('ask.irreversible'), destructive: true }))) return;
        try {
            await oeeApi.deleteProfile(p.id);
            toast.success(tr('oeeProfiles.deleted'));
            queryClient.invalidateQueries({ queryKey: ['oee-profiles'] });
            queryClient.invalidateQueries({ queryKey: ['oee-snapshot'] });
        } catch (e: unknown) {
            toast.error(tr('oee.error_with_msg', { msg: (e as Error)?.message ?? tr('oee.unknown_error') }));
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
                // Sent back as they are: the server defaults an absent value
                // to true, so switching a profile on or off used to turn
                // "respect shifts/maintenance" back on for a profile that had
                // them off.
                respect_shifts: p.respect_shifts,
                respect_maintenance: p.respect_maintenance,
                enabled: !p.enabled,
            });
            queryClient.invalidateQueries({ queryKey: ['oee-profiles'] });
            queryClient.invalidateQueries({ queryKey: ['dashboard-overview'] });
        } catch (e: unknown) {
            toast.error(tr('oee.error_with_msg', { msg: (e as Error)?.message ?? tr('oee.unknown_error') }));
        }
    };

    const onSaved = () => {
        queryClient.invalidateQueries({ queryKey: ['oee-profiles'] });
        queryClient.invalidateQueries({ queryKey: ['dashboard-overview'] });
    };

    return (
        <div className="space-y-4">
            <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3">
                <div className="min-w-0">
                    <h2 className="text-2xl sm:text-3xl font-bold tracking-tight flex items-center gap-2">
                        <Gauge size={28} /> {tr('oeeProfiles.title')}
                    </h2>
                    <p className="text-muted-foreground text-sm">
                        {tr('oeeProfiles.subtitle')}
                    </p>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                    <Link to="/tv/oee" target="_blank">
                        <Button variant="outline" size="sm" title={tr('oeeProfiles.tv_mode_hint')}>
                            <Monitor size={14} className="mr-1.5" /> {tr('oeeProfiles.tv_mode')}
                        </Button>
                    </Link>
                    <Button onClick={() => { setEditing(null); setEditorOpen(true); }} size="sm">
                        <Plus size={16} className="mr-1.5" /> {tr('oeeProfiles.new_profile')}
                    </Button>
                </div>
            </div>

            <Tabs defaultValue="profiles">
                <TabsList className="flex-wrap h-auto">
                    <TabsTrigger value="profiles">{tr('oeeProfiles.tab_profiles')}</TabsTrigger>
                    <TabsTrigger value="history">{tr('oeeProfiles.tab_history')}</TabsTrigger>
                    <TabsTrigger value="by-shift">{tr('oeeProfiles.tab_by_shift')}</TabsTrigger>
                    <TabsTrigger value="losses">{tr('oeeProfiles.tab_losses')}</TabsTrigger>
                    <TabsTrigger value="alerts">{tr('oeeProfiles.tab_alerts')}</TabsTrigger>
                    <TabsTrigger value="hierarchy">{tr('oeeProfiles.tab_hierarchy')}</TabsTrigger>
                </TabsList>

                <TabsContent value="profiles" className="space-y-4 mt-4">
                    <Card>
                        <CardHeader className="pb-3">
                            <div className="flex items-center justify-between">
                                <div>
                                    <CardTitle className="text-base">{tr('oeeProfiles.configured')}</CardTitle>
                                    <CardDescription className="text-xs">
                                        {tr('oeeProfiles.count', { count: profiles?.length ?? 0 })}
                                        {(profiles?.length ?? 0) === 0 && ` ${tr('oeeProfiles.count_zero_hint')}`}
                                    </CardDescription>
                                </div>
                                {(profiles?.length ?? 0) > 0 && (
                                    <Button variant="outline" size="sm"
                                        onClick={() => oeeApi.exportCSV('profiles', {}).catch(() => toast.error(tr('oeeProfiles.export_failed')))}>
                                        <Download size={14} className="mr-1" /> CSV
                                    </Button>
                                )}
                            </div>
                        </CardHeader>
                        <CardContent>
                            {isLoading ? (
                                <div className="py-8 text-center text-muted-foreground text-sm">{tr('common.loading')}</div>
                            ) : (profiles?.length ?? 0) === 0 ? (
                                <div className="py-12 text-center border border-dashed rounded-md">
                                    <Gauge size={32} className="mx-auto opacity-30 mb-2" />
                                    <p className="text-sm text-muted-foreground">{tr('oeeProfiles.empty')}</p>
                                    <p className="text-xs text-muted-foreground mt-1">
                                        {tr('oeeProfiles.empty_hint')}
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

                <TabsContent value="losses" className="space-y-4 mt-4">
                    <LossesTab profiles={profiles ?? []} />
                </TabsContent>

                <TabsContent value="alerts" className="space-y-4 mt-4">
                    <OEEAlertRules profiles={profiles ?? []} />
                </TabsContent>

                <TabsContent value="hierarchy" className="space-y-4 mt-4">
                    <OEEHierarchyView />
                </TabsContent>
            </Tabs>

            <ProfileEditor
                open={editorOpen}
                onClose={() => setEditorOpen(false)}
                initial={editing}
                areas={areas ?? []}
                onSaved={onSaved}
            />
        </div>
    );
};

// Tab Storia: selector profilo + chart storico via OEEHistoryChart.
// Profilo "Rollup fabbrica" = profileId null (riga aggregata del cron).
const HistoryTab = ({ profiles }: { profiles: OEEProfile[] }) => {
    const { t: tr } = useTranslation();
    const [selected, setSelected] = useState<number | null>(null);
    const target = selected === null
        ? undefined
        : profiles.find((p) => p.id === selected)?.target_oee;

    return (
        <div className="space-y-3">
            <div className="flex items-center gap-3 flex-wrap">
                <Label className="text-xs">{tr('oeeProfiles.profile_label')}</Label>
                <Select
                    value={selected === null ? 'rollup' : String(selected)}
                    onValueChange={(v) => setSelected(v === 'rollup' ? null : parseInt(v, 10))}
                >
                    <SelectTrigger className="w-full sm:w-72"><SelectValue /></SelectTrigger>
                    <SelectContent>
                        <SelectItem value="rollup">
                            <span className="font-medium">{tr('oeeProfiles.overall')}</span>
                            <span className="text-muted-foreground ml-2 text-xs">{tr('oeeProfiles.overall_avg')}</span>
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
                    {tr('oeeProfiles.history_hint')}
                </p>
                <Button variant="outline" size="sm" className="ml-auto"
                    onClick={() => {
                        const now = new Date();
                        const from = new Date(now); from.setDate(from.getDate() - 30);
                        oeeApi.exportCSV('history', {
                            ...(selected ? { profile_id: String(selected) } : {}),
                            from: from.toISOString().split('T')[0],
                            to: now.toISOString().split('T')[0],
                        }).catch(() => toast.error(tr('oeeProfiles.export_failed')));
                    }}>
                    <Download size={14} className="mr-1" /> {tr('oeeProfiles.export_csv')}
                </Button>
            </div>

            <OEEHistoryChart profileId={selected} target={target} />
        </div>
    );
};

// Tab Per turno: matrice turni × giorni con OEE colorato.
const ByShiftTab = ({ profiles }: { profiles: OEEProfile[] }) => {
    const { t: tr } = useTranslation();
    const [selected, setSelected] = useState<number | null>(null);
    return (
        <div className="space-y-3">
            <div className="flex items-center gap-3 flex-wrap">
                <Label className="text-xs">{tr('oeeProfiles.profile_label')}</Label>
                <Select
                    value={selected === null ? 'rollup' : String(selected)}
                    onValueChange={(v) => setSelected(v === 'rollup' ? null : parseInt(v, 10))}
                >
                    <SelectTrigger className="w-full sm:w-72"><SelectValue /></SelectTrigger>
                    <SelectContent>
                        <SelectItem value="rollup">
                            <span className="font-medium">{tr('oeeProfiles.overall')}</span>
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
                    {tr('oeeProfiles.by_shift_hint')}
                </p>
                <Button variant="outline" size="sm" className="ml-auto"
                    onClick={() => {
                        const now = new Date();
                        const from = new Date(now); from.setDate(from.getDate() - 30);
                        oeeApi.exportCSV('by-shift', {
                            ...(selected ? { profile_id: String(selected) } : {}),
                            from: from.toISOString().split('T')[0],
                            to: now.toISOString().split('T')[0],
                        }).catch(() => toast.error(tr('oeeProfiles.export_failed')));
                    }}>
                    <Download size={14} className="mr-1" /> {tr('oeeProfiles.export_csv')}
                </Button>
            </div>

            <OEEShiftMatrix profileId={selected} />
        </div>
    );
};

// Tab Loss & Pareto: richiede un profilo specifico (non rollup, perché
// le perdite sono per profilo). Mostra MTBF/MTTR + Pareto delle 6 cause.
const LossesTab = ({ profiles }: { profiles: OEEProfile[] }) => {
    const { t: tr } = useTranslation();
    const [picked, setSelected] = useState<number | null>(null);
    // Until the user picks one, the first profile — whenever the list arrives.
    // Initialising the state from profiles[0] only worked if the profiles had
    // loaded before the tab mounted; otherwise nothing was ever selected.
    const selected = picked ?? (profiles.length > 0 ? profiles[0].id : null);
    const selectedProfile = profiles.find((p) => p.id === selected);

    if (profiles.length === 0) {
        return (
            <div className="py-12 text-center text-sm text-muted-foreground border border-dashed rounded-md">
                {tr('oeeProfiles.losses_need_profile')}
            </div>
        );
    }

    return (
        <div className="space-y-3">
            <div className="flex items-center gap-3 flex-wrap">
                <Label className="text-xs">{tr('oeeProfiles.profile_label')}</Label>
                <Select
                    value={selected !== null ? String(selected) : ''}
                    onValueChange={(v) => setSelected(parseInt(v, 10))}
                >
                    <SelectTrigger className="w-full sm:w-72"><SelectValue /></SelectTrigger>
                    <SelectContent>
                        {profiles.map((p) => (
                            <SelectItem key={p.id} value={String(p.id)}>
                                {p.name}
                                {p.area_name && <span className="text-muted-foreground ml-2 text-xs">[{p.area_name}]</span>}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                <p className="text-[11px] text-muted-foreground">
                    {tr('oeeProfiles.losses_hint')}
                </p>
                {selectedProfile && (
                    <Button variant="outline" size="sm" className="ml-auto"
                        onClick={() => {
                            const now = new Date();
                            const from = new Date(now); from.setDate(from.getDate() - 30);
                            oeeApi.exportCSV('losses', {
                                profile_id: String(selectedProfile.id),
                                from: from.toISOString().split('T')[0],
                                to: now.toISOString().split('T')[0],
                            }).catch(() => toast.error(tr('oeeProfiles.export_failed')));
                        }}>
                        <Download size={14} className="mr-1" /> {tr('oeeProfiles.export_csv')}
                    </Button>
                )}
            </div>

            {selectedProfile && (
                <OEELossPareto profileId={selectedProfile.id} profileName={selectedProfile.name} />
            )}
        </div>
    );
};

const ProfileRow = ({
    p, onEdit, onDelete, onToggle,
}: { p: OEEProfile; onEdit: () => void; onDelete: () => void; onToggle: () => void }) => {
    const { t: tr } = useTranslation();
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
                        <Badge variant="outline" className="text-[10px] border-amber-500/40 text-amber-500">{tr('oeeProfiles.fallback')}</Badge>
                    ) : (
                        <Badge variant="outline" className="text-[10px] border-emerald-500/40 text-emerald-500">
                            {tr('oeeProfiles.tags_configured', { n: tagsConfigured })}
                        </Badge>
                    )}
                    {!p.enabled && (
                        <Badge variant="outline" className="text-[10px] border-muted">{tr('oeeProfiles.disabled')}</Badge>
                    )}
                </div>
                {p.description && <p className="text-xs text-muted-foreground truncate">{p.description}</p>}
                <p className="text-[10px] text-muted-foreground mt-0.5">
                    {tr('oeeProfiles.row_summary', { hours: (p.window_minutes / 60).toFixed(0), target: p.target_oee })}
                    {p.target_pieces_per_hour > 0 && ` · ${tr('oeeProfiles.row_pph', { n: p.target_pieces_per_hour })}`}
                </p>
            </div>
            <div className="flex items-center gap-1">
                <Button variant="ghost" size="icon" onClick={onToggle} title={p.enabled ? tr('oee.disable') : tr('oee.enable')}>
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
