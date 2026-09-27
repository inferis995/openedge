import { useEffect, useMemo, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Pencil, Trash2, PlayCircle, History as HistoryIcon, ChefHat, Search } from 'lucide-react';

import { recipesApi, Recipe, RecipeRun, LoadResult } from '@/api/recipes';
import { tagsApi } from '@/api/tags';
import { TagWithHierarchy } from '@/types/trend';
import { useAuthStore } from '@/stores/useAuthStore';
import { showApiError, showApiSuccess } from '@/lib/api-error-handler';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog';
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';
import { EmptyState } from '@/components/ui/empty-state';
import { ChefHat as ChefHatIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';

// Status badge for recipe runs. Maps the backend status enum to a
// neutral Tailwind palette so future statuses fall through to "slate".
const statusBadge = (s: string): { label: string; cls: string } => {
    switch (s) {
        case 'success': return { label: i18n.t('recipesPage.status_success'), cls: 'bg-green-500/10 text-green-500 border-none' };
        case 'partial': return { label: i18n.t('recipesPage.status_partial'), cls: 'bg-orange-500/10 text-orange-500 border-none' };
        case 'failed':  return { label: i18n.t('recipesPage.status_failed'),  cls: 'bg-red-500/10 text-red-500 border-none' };
        case 'pending': return { label: i18n.t('recipesPage.status_pending'), cls: 'bg-slate-500/10 text-slate-300 border-none' };
        default:        return { label: s,          cls: 'bg-slate-500/10 text-slate-300 border-none' };
    }
};

interface ValueRow { tag_id: number; value: string; }

const RecipesPage = () => {
    const { t: tr } = useTranslation();
    const queryClient = useQueryClient();
    const { isAdmin } = useAuthStore();

    const { data: recipes = [], isLoading } = useQuery({
        queryKey: ['recipes'],
        queryFn: recipesApi.getAll,
    });
    const { data: hierarchy } = useQuery({
        queryKey: ['tags-with-hierarchy'],
        queryFn: tagsApi.getAllWithHierarchy,
        staleTime: 60_000,
    });
    const tagsById = useMemo<Record<number, TagWithHierarchy>>(() => {
        const m: Record<number, TagWithHierarchy> = {};
        (hierarchy ?? []).forEach((t) => { m[t.id] = t; });
        return m;
    }, [hierarchy]);

    // Editor state. editingId === null → create; number → update.
    const [editorOpen, setEditorOpen] = useState(false);
    const [editingId, setEditingId] = useState<number | null>(null);
    const [name, setName] = useState('');
    const [description, setDescription] = useState('');
    const [values, setValues] = useState<ValueRow[]>([]);
    const [filter, setFilter] = useState('');

    const openCreate = () => {
        setEditingId(null);
        setName(''); setDescription(''); setValues([]);
        setFilter('');
        setEditorOpen(true);
    };
    const openEdit = async (r: Recipe) => {
        try {
            const full = await recipesApi.get(r.id);
            setEditingId(r.id);
            setName(full.name);
            setDescription(full.description ?? '');
            setValues((full.values ?? []).map((v) => ({ tag_id: v.tag_id, value: v.value })));
            setFilter('');
            setEditorOpen(true);
        } catch (e) { showApiError(e, tr('recipesPage.load_recipe_failed')); }
    };

    const saveMutation = useMutation({
        mutationFn: async () => {
            const payload = { name, description, values };
            if (editingId !== null) await recipesApi.update(editingId, payload);
            else await recipesApi.create(payload);
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['recipes'] });
            showApiSuccess(tr('recipesPage.saved'));
            setEditorOpen(false);
        },
        onError: (e) => showApiError(e, tr('recipesPage.save_failed')),
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => recipesApi.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['recipes'] });
            showApiSuccess(tr('recipesPage.deleted'));
        },
        onError: (e) => showApiError(e, tr('recipesPage.delete_failed')),
    });

    const handleDelete = async (r: Recipe) => {
        if (await confirmAction({ title: i18n.t('ask.delete_named', { name: r.name }), description: i18n.t('ask.delete_recipe_desc'), destructive: true })) {
            deleteMutation.mutate(r.id);
        }
    };

    // ── Load flow (with confirmation + per-tag results) ──────────────────
    const [loadingRecipe, setLoadingRecipe] = useState<Recipe | null>(null);
    const [loadResult, setLoadResult] = useState<LoadResult | null>(null);
    const [loadInflight, setLoadInflight] = useState(false);

    const beginLoad = async (r: Recipe) => {
        try {
            const full = await recipesApi.get(r.id);
            setLoadingRecipe(full);
            setLoadResult(null);
        } catch (e) { showApiError(e, tr('recipesPage.load_recipe_failed')); }
    };
    const confirmLoad = async () => {
        if (!loadingRecipe) return;
        setLoadInflight(true);
        try {
            const res = await recipesApi.load(loadingRecipe.id);
            setLoadResult(res);
            const msg = tr('recipesPage.load_summary', { written: res.written, failed: res.failed });
            if (res.status === 'success') showApiSuccess(tr('recipes.load_success'), msg);
            else if (res.status === 'partial') showApiError(new Error(tr('recipesPage.partial_detail', { msg })), tr('recipes.load_partial'));
            else showApiError(new Error(msg), tr('recipes.load_failed'));
        } catch (e) { showApiError(e, tr('recipes.load_failed')); }
        finally { setLoadInflight(false); }
    };

    // ── Runs history (lazy per recipe) ───────────────────────────────────
    const [runsRecipe, setRunsRecipe] = useState<Recipe | null>(null);
    const [runs, setRuns] = useState<RecipeRun[]>([]);
    const [runsLoading, setRunsLoading] = useState(false);
    const openRuns = async (r: Recipe) => {
        setRunsRecipe(r);
        setRuns([]);
        setRunsLoading(true);
        try {
            const rows = await recipesApi.runs(r.id);
            setRuns(rows);
        } catch (e) { showApiError(e, tr('recipesPage.history_failed')); }
        finally { setRunsLoading(false); }
    };

    // ── Tag picker helpers ───────────────────────────────────────────────
    const addedIds = useMemo(() => new Set(values.map((v) => v.tag_id)), [values]);
    const filteredTags = useMemo(() => {
        const q = filter.toLowerCase().trim();
        return (hierarchy ?? [])
            .filter((t) => !addedIds.has(t.id))
            .filter((t) => {
                if (!q) return true;
                return (
                    (t.alias ?? '').toLowerCase().includes(q) ||
                    t.code.toLowerCase().includes(q) ||
                    (t.gateway_name ?? '').toLowerCase().includes(q)
                );
            })
            .slice(0, 50); // cap so the picker stays responsive on large catalogs
    }, [hierarchy, filter, addedIds]);

    const addTag = (id: number) => setValues((v) => [...v, { tag_id: id, value: '' }]);
    const removeTag = (id: number) => setValues((v) => v.filter((r) => r.tag_id !== id));
    const updateValue = (id: number, value: string) =>
        setValues((v) => v.map((r) => (r.tag_id === id ? { ...r, value } : r)));

    return (
        <div className="space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight flex items-center gap-2">
                        <ChefHat size={22} /> {tr('recipes.title')}
                    </h2>
                    <p className="text-muted-foreground">
                        {tr('recipes.subtitle')}
                    </p>
                </div>
                {isAdmin() && (
                    <Button className="gap-2" onClick={openCreate}>
                        <Plus size={16} /> {tr('recipes.new')}
                    </Button>
                )}
            </div>

            <div className="rounded-md border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[60px]">ID</TableHead>
                            <TableHead>{tr('common.name')}</TableHead>
                            <TableHead>{tr('recipesPage.description')}</TableHead>
                            <TableHead className="text-right">{tr('recipes.value_count')}</TableHead>
                            <TableHead>{tr('recipesPage.updated')}</TableHead>
                            <TableHead className="text-right">{tr('common.actions')}</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {isLoading ? (
                            <TableRow><TableCell colSpan={6} className="h-24 text-center">{tr('common.loading')}</TableCell></TableRow>
                        ) : recipes.length === 0 ? (
                            <TableRow><TableCell colSpan={6}>
                                <EmptyState
                                    icon={ChefHatIcon}
                                    title={tr('empty.recipes_title')}
                                    description={tr('empty.recipes_desc')}
                                    action={isAdmin() ? <Button onClick={openCreate}>{tr('empty.recipes_create')}</Button> : undefined}
                                />
                            </TableCell></TableRow>
                        ) : (
                            recipes.map((r) => (
                                <TableRow key={r.id}>
                                    <TableCell className="font-medium">{r.id}</TableCell>
                                    <TableCell className="font-semibold">{r.name}</TableCell>
                                    <TableCell className="text-sm text-muted-foreground">{r.description ?? ''}</TableCell>
                                    <TableCell className="text-right">{r.value_count}</TableCell>
                                    <TableCell className="text-sm">{new Date(r.updated_at).toLocaleString(i18n.language)}</TableCell>
                                    <TableCell className="text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <Button variant="ghost" size="icon" title={tr('recipes.load')}
                                                className="h-10 sm:h-8 w-10 sm:w-8 text-cyan-500 hover:bg-cyan-500/10"
                                                disabled={r.value_count === 0}
                                                onClick={() => beginLoad(r)}>
                                                <PlayCircle size={16} />
                                            </Button>
                                            <Button variant="ghost" size="icon" title={tr('recipes.history')}
                                                className="h-10 sm:h-8 w-10 sm:w-8 text-amber-500 hover:bg-amber-500/10"
                                                onClick={() => openRuns(r)}>
                                                <HistoryIcon size={16} />
                                            </Button>
                                            {isAdmin() && (
                                                <>
                                                    <Button variant="ghost" size="icon" title={tr('common.edit')}
                                                        className="h-10 sm:h-8 w-10 sm:w-8 text-blue-500 hover:bg-blue-500/10"
                                                        onClick={() => openEdit(r)}>
                                                        <Pencil size={16} />
                                                    </Button>
                                                    <Button variant="ghost" size="icon" title={tr('common.delete')}
                                                        className="h-10 sm:h-8 w-10 sm:w-8 text-red-500 hover:bg-red-500/10"
                                                        onClick={() => handleDelete(r)}>
                                                        <Trash2 size={16} />
                                                    </Button>
                                                </>
                                            )}
                                        </div>
                                    </TableCell>
                                </TableRow>
                            ))
                        )}
                    </TableBody>
                </Table>
            </div>

            {/* ── Editor ─────────────────────────────────────────────────── */}
            <Dialog open={editorOpen} onOpenChange={setEditorOpen}>
                <DialogContent className="max-w-4xl">
                    <DialogHeader>
                        <DialogTitle>{editingId !== null ? tr('recipesPage.edit_title') : tr('recipes.new')}</DialogTitle>
                        <DialogDescription>
                            {tr('recipesPage.editor_desc')}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-6 py-2">
                        {/* Left: header + selected values */}
                        <div className="space-y-3">
                            <div className="grid gap-1">
                                <Label htmlFor="rec-name">{tr('common.name')}</Label>
                                <Input id="rec-name" value={name} onChange={(e) => setName(e.target.value)}
                                    placeholder={tr('recipesPage.name_placeholder')} />
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="rec-desc">{tr('recipesPage.description_optional')}</Label>
                                <Input id="rec-desc" value={description} onChange={(e) => setDescription(e.target.value)}
                                    placeholder={tr('recipesPage.description_placeholder')} />
                            </div>
                            <div className="rounded-md border max-h-[40vh] overflow-auto">
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead>Tag</TableHead>
                                            <TableHead>{tr('recipesPage.value')}</TableHead>
                                            <TableHead className="w-12" />
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {values.length === 0 ? (
                                            <TableRow><TableCell colSpan={3} className="h-16 text-center text-muted-foreground">{tr('recipesPage.no_values')}</TableCell></TableRow>
                                        ) : values.map((row) => {
                                            const t = tagsById[row.tag_id];
                                            return (
                                                <TableRow key={row.tag_id}>
                                                    <TableCell className="font-mono text-xs">
                                                        <div>{t?.alias ?? tr('recipesPage.tag_n', { id: row.tag_id })}</div>
                                                        <div className="text-muted-foreground">{t?.gateway_name ?? ''} {t ? `(${t.data_type})` : ''}</div>
                                                    </TableCell>
                                                    <TableCell>
                                                        <Input value={row.value} onChange={(e) => updateValue(row.tag_id, e.target.value)} />
                                                    </TableCell>
                                                    <TableCell>
                                                        <Button variant="ghost" size="icon" className="h-9 sm:h-7 w-9 sm:w-7 text-red-500"
                                                            onClick={() => removeTag(row.tag_id)}>
                                                            <Trash2 size={14} />
                                                        </Button>
                                                    </TableCell>
                                                </TableRow>
                                            );
                                        })}
                                    </TableBody>
                                </Table>
                            </div>
                        </div>
                        {/* Right: tag picker */}
                        <div className="space-y-2">
                            <Label>{tr('recipesPage.add_tags')}</Label>
                            <div className="relative">
                                <Search size={14} className="absolute left-3 top-3 text-muted-foreground" />
                                <Input className="pl-8" placeholder={tr('recipesPage.filter_placeholder')}
                                    value={filter} onChange={(e) => setFilter(e.target.value)} />
                            </div>
                            <div className="rounded-md border max-h-[44vh] overflow-auto">
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead>Alias</TableHead>
                                            <TableHead>Gateway</TableHead>
                                            <TableHead>{tr('recipesPage.type')}</TableHead>
                                            <TableHead />
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {filteredTags.length === 0 ? (
                                            <TableRow><TableCell colSpan={4} className="h-16 text-center text-muted-foreground">{tr('recipesPage.no_match')}</TableCell></TableRow>
                                        ) : filteredTags.map((t) => (
                                            <TableRow key={t.id}>
                                                <TableCell className="font-mono text-xs">{t.alias ?? t.code}</TableCell>
                                                <TableCell className="text-xs">{t.gateway_name}</TableCell>
                                                <TableCell className="text-xs">{t.data_type}</TableCell>
                                                <TableCell className="text-right">
                                                    <Button size="sm" variant="outline" onClick={() => addTag(t.id)}>{tr('common.add')}</Button>
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </div>
                        </div>
                    </div>
                    <DialogFooter>
                        <Button disabled={!name || saveMutation.isPending} onClick={() => saveMutation.mutate()}>
                            {saveMutation.isPending ? tr('recipesPage.saving') : (editingId !== null ? tr('common.save') : tr('common.create'))}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* ── Load confirmation ──────────────────────────────────────── */}
            <Dialog open={!!loadingRecipe} onOpenChange={(open) => { if (!open) { setLoadingRecipe(null); setLoadResult(null); } }}>
                <DialogContent className="max-w-2xl">
                    <DialogHeader>
                        <DialogTitle>{tr('recipesPage.load_title')}{loadingRecipe && `: ${loadingRecipe.name}`}</DialogTitle>
                        <DialogDescription>
                            {tr('recipes.load_confirm', { count: loadingRecipe?.values?.length ?? 0 })}{' '}
                            {tr('recipesPage.load_audited')}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="max-h-[50vh] overflow-auto rounded-md border">
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>Tag</TableHead>
                                    <TableHead>{tr('recipesPage.value')}</TableHead>
                                    {loadResult && <TableHead>{tr('recipesPage.result')}</TableHead>}
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {(loadingRecipe?.values ?? []).map((v) => {
                                    const r = loadResult?.results.find((x) => x.tag_id === v.tag_id);
                                    return (
                                        <TableRow key={v.tag_id}>
                                            <TableCell className="font-mono text-xs">
                                                {v.tag_alias} <span className="text-muted-foreground">({v.data_type})</span>
                                            </TableCell>
                                            <TableCell className="font-mono">{v.value}</TableCell>
                                            {loadResult && (
                                                <TableCell>
                                                    {r ? (
                                                        r.ok
                                                            ? <Badge className="bg-green-500/10 text-green-500 border-none">OK</Badge>
                                                            : <span className="text-xs text-red-500">{r.error ?? tr('recipesPage.write_failed')}</span>
                                                    ) : '—'}
                                                </TableCell>
                                            )}
                                        </TableRow>
                                    );
                                })}
                            </TableBody>
                        </Table>
                    </div>
                    <DialogFooter>
                        {loadResult ? (
                            <Button onClick={() => { setLoadingRecipe(null); setLoadResult(null); }}>{tr('common.close')}</Button>
                        ) : (
                            <Button disabled={loadInflight} onClick={confirmLoad}>
                                {loadInflight ? tr('recipes.load_pending') : tr('recipesPage.confirm_load')}
                            </Button>
                        )}
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* ── Runs history ───────────────────────────────────────────── */}
            <Dialog open={!!runsRecipe} onOpenChange={(open) => { if (!open) setRunsRecipe(null); }}>
                <DialogContent className="max-w-3xl">
                    <DialogHeader>
                        <DialogTitle>{tr('recipesPage.runs_title')}{runsRecipe && `: ${runsRecipe.name}`}</DialogTitle>
                        <DialogDescription>
                            {tr('recipesPage.runs_desc')}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="max-h-[60vh] overflow-auto rounded-md border">
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>{tr('recipesPage.when_utc')}</TableHead>
                                    <TableHead>{tr('recipesPage.triggered_by')}</TableHead>
                                    <TableHead>{tr('recipesPage.status')}</TableHead>
                                    <TableHead>{tr('nav.tags')}</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {runsLoading ? (
                                    <TableRow><TableCell colSpan={4} className="h-20 text-center">{tr('common.loading')}</TableCell></TableRow>
                                ) : runs.length === 0 ? (
                                    <TableRow><TableCell colSpan={4} className="h-20 text-center">{tr('recipesPage.no_runs')}</TableCell></TableRow>
                                ) : runs.map((r) => {
                                    const sb = statusBadge(r.status);
                                    const ok = (r.results ?? []).filter((x) => x.ok).length;
                                    const failed = (r.results ?? []).length - ok;
                                    return (
                                        <TableRow key={r.id}>
                                            <TableCell className="font-mono text-xs">
                                                {new Date(r.triggered_at).toISOString().replace('T', ' ').slice(0, 19)}
                                            </TableCell>
                                            <TableCell className="text-sm">{r.triggered_username || tr('recipesPage.system_user')}</TableCell>
                                            <TableCell><Badge className={sb.cls}>{sb.label}</Badge></TableCell>
                                            <TableCell className="text-xs">{tr('recipesPage.run_summary', { ok, failed })}</TableCell>
                                        </TableRow>
                                    );
                                })}
                            </TableBody>
                        </Table>
                    </div>
                </DialogContent>
            </Dialog>
        </div>
    );
};

export default RecipesPage;

// silence unused-effect-import warning on intentionally bare effect-free file
void useEffect;
