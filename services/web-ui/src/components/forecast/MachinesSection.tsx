import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { Cog, Factory, Pencil, Plus, Search, Trash2, Wand2 } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { assetsApi, type Asset, type AssetInput, type ForecastOverviewTag } from '@/api/forecast';
import { ForecastRiskList } from '@/components/forecast/ForecastRisks';
import { FORECAST_STATUS_VARIANT, HEALTH_TONE } from '@/lib/forecastUi';
import { showApiError } from '@/lib/api-error-handler';
import { confirmAction } from '@/lib/confirm';
import { useAuthStore } from '@/stores/useAuthStore';
import { useNavigationStore } from '@/stores/useNavigationStore';

const NONE = 'none';

/**
 * Machines: the tags of one machine together, with a health from their
 * forecasts — 100 minus the worst problem among its parts. A running-state
 * tag, when set, makes the members' forecasts learn from running time only.
 */
export function MachinesSection({ tags }: { tags: ForecastOverviewTag[] }) {
    const { t } = useTranslation();
    const qc = useQueryClient();
    const { isAdmin } = useAuthStore();
    const { selectedOrgId } = useNavigationStore();
    const { data: assets, isLoading } = useQuery({
        queryKey: ['forecast-assets', selectedOrgId],
        queryFn: () => assetsApi.list(),
        refetchInterval: 60_000,
    });
    const [open, setOpen] = useState<Asset | null>(null);
    const [editing, setEditing] = useState<Asset | 'new' | null>(null);
    const [busy, setBusy] = useState(false);

    // Worst first: what needs looking at comes first.
    const sorted = useMemo(() => [...(assets ?? [])].sort((a, b) =>
        (a.health.score ?? 101) - (b.health.score ?? 101) || a.name.localeCompare(b.name)), [assets]);

    const refresh = () => qc.invalidateQueries({ queryKey: ['forecast-assets'] });
    const fromGateways = async () => {
        setBusy(true);
        try {
            const r = await assetsApi.fromGateways();
            toast.success(t('forecast.machines_created', { count: r.created }));
            await refresh();
        } catch (e) { showApiError(e, t('forecast.failed')); } finally { setBusy(false); }
    };

    return (
        <div className="space-y-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <p className="max-w-2xl text-sm text-muted-foreground">{t('forecast.machines_intro')}</p>
                {isAdmin() && (
                    <div className="flex shrink-0 flex-col gap-2 sm:flex-row">
                        <Button variant="outline" className="gap-1.5" disabled={busy} onClick={() => void fromGateways()}>
                            <Wand2 className="size-4" /> {t('forecast.machines_from_gateways')}
                        </Button>
                        <Button className="gap-1.5" onClick={() => setEditing('new')}>
                            <Plus className="size-4" /> {t('forecast.machines_new')}
                        </Button>
                    </div>
                )}
            </div>

            {isLoading && <p className="text-sm text-muted-foreground">{t('forecast.loading')}</p>}
            {!isLoading && sorted.length === 0 && (
                <div className="rounded-lg border border-dashed p-10 text-center text-sm text-muted-foreground">
                    <Factory className="mx-auto mb-3 size-8 opacity-40" />
                    {t('forecast.machines_empty')}
                </div>
            )}

            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {sorted.map(a => <MachineCard key={a.id} asset={a} onOpen={() => setOpen(a)} />)}
            </div>

            <Dialog open={!!open} onOpenChange={o => { if (!o) setOpen(null); }}>
                {open && (
                    <DialogContent className="max-w-3xl">
                        <DialogHeader>
                            <DialogTitle>{open.name}</DialogTitle>
                            <DialogDescription>{open.description || t('forecast.machine_members', { count: open.members.length })}</DialogDescription>
                        </DialogHeader>
                        <MachineDetail asset={open} />
                        {isAdmin() && (
                            <DialogFooter>
                                <Button variant="ghost" className="gap-1.5 text-destructive" onClick={async () => {
                                    if (!(await confirmAction({ title: t('forecast.machine_delete_q', { name: open.name }), destructive: true }))) return;
                                    try { await assetsApi.remove(open.id); setOpen(null); await refresh(); } catch (e) { showApiError(e, t('forecast.failed')); }
                                }}><Trash2 className="size-4" /> {t('forecast.machine_delete')}</Button>
                                <Button className="gap-1.5" onClick={() => { setEditing(open); setOpen(null); }}>
                                    <Pencil className="size-4" /> {t('forecast.machine_edit')}
                                </Button>
                            </DialogFooter>
                        )}
                    </DialogContent>
                )}
            </Dialog>

            <Dialog open={!!editing} onOpenChange={o => { if (!o) setEditing(null); }}>
                {editing && (
                    <DialogContent className="max-w-2xl">
                        <MachineForm key={editing === 'new' ? 'new' : editing.id} asset={editing === 'new' ? null : editing} tags={tags}
                            onDone={async () => { setEditing(null); await refresh(); }} />
                    </DialogContent>
                )}
            </Dialog>
        </div>
    );
}

function HealthBar({ score, level }: { score: number | null; level: Asset['health']['level'] }) {
    const tone = HEALTH_TONE[level];
    return (
        <div className="space-y-1">
            <div className="h-2 overflow-hidden rounded-full bg-muted">
                <div className={`h-full ${tone.bar}`} style={{ width: `${score ?? 0}%` }} />
            </div>
        </div>
    );
}

function MachineCard({ asset, onOpen }: { asset: Asset; onOpen: () => void }) {
    const { t } = useTranslation();
    const h = asset.health;
    const tone = HEALTH_TONE[h.level];
    return (
        <button type="button" onClick={onOpen}
            className="flex flex-col gap-3 rounded-lg border bg-card p-4 text-left transition-colors hover:bg-muted/40">
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                    <div className="truncate font-medium">{asset.name}</div>
                    <div className="text-xs text-muted-foreground">
                        {t('forecast.machine_members', { count: h.members })} · {t('forecast.machine_known', { known: h.known })}
                    </div>
                </div>
                <div className="text-right">
                    <div className={`text-2xl font-semibold tabular-nums ${tone.text}`}>{h.score ?? '—'}</div>
                    <div className={`text-xs ${tone.text}`}>{t(`forecast.health_${h.level}`)}</div>
                </div>
            </div>
            <HealthBar score={h.score} level={h.level} />
            {h.issues.length > 0 ? (
                <ul className="space-y-1 text-xs">
                    {h.issues.slice(0, 2).map((i, k) => (
                        <li key={k} className="truncate">
                            <span className="font-medium">{i.alias}</span>{' — '}
                            {i.kind === 'risk' && i.risk
                                ? `${t(`forecast.limit_${i.risk.type}`)} · ${t(`forecast.risk_${i.risk.likelihood}`).toLowerCase()}`
                                : t('forecast.issue_anomaly', { outside: i.outside, checked: i.checked })}
                        </li>
                    ))}
                    {h.issues.length > 2 && <li className="text-muted-foreground">{t('forecast.issues_more', { count: h.issues.length - 2 })}</li>}
                </ul>
            ) : <p className="text-xs text-muted-foreground">{h.known > 0 ? t('forecast.machine_no_issues') : t('forecast.machine_unknown')}</p>}
            {asset.state_alias && (
                <Badge variant="muted" className="w-fit gap-1"><Cog className="size-3" />{t('forecast.machine_state', { alias: asset.state_alias })}</Badge>
            )}
        </button>
    );
}

function MachineDetail({ asset }: { asset: Asset }) {
    const { t } = useTranslation();
    const h = asset.health;
    return (
        <div className="space-y-4">
            <div className="flex items-center gap-4 rounded-lg border bg-card p-4">
                <div className={`text-4xl font-semibold tabular-nums ${HEALTH_TONE[h.level].text}`}>{h.score ?? '—'}</div>
                <div className="min-w-0 flex-1 space-y-1.5">
                    <div className={`text-sm font-medium ${HEALTH_TONE[h.level].text}`}>{t(`forecast.health_${h.level}`)}</div>
                    <HealthBar score={h.score} level={h.level} />
                    <p className="text-xs text-muted-foreground">{t('forecast.health_hint')}</p>
                </div>
            </div>
            {asset.state_alias && <p className="text-sm text-muted-foreground">{t('forecast.machine_state_hint', { alias: asset.state_alias })}</p>}
            <div className="rounded-lg border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>{t('forecast.col_tag')}</TableHead>
                            <TableHead>{t('forecast.col_status')}</TableHead>
                            <TableHead>{t('forecast.col_risk')}</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {asset.members.map(m => (
                            <TableRow key={m.tag_id}>
                                <TableCell>
                                    <div className="font-medium">{m.alias}</div>
                                    <div className="text-xs text-muted-foreground">{m.gateway_name}</div>
                                </TableCell>
                                <TableCell><Badge variant={FORECAST_STATUS_VARIANT[m.status]}>{t(`forecast.status_${m.status}`)}</Badge></TableCell>
                                <TableCell>
                                    {(m.risks ?? []).length > 0
                                        ? <ForecastRiskList risks={m.risks ?? []} unit={m.unit} compact />
                                        : (m.anomaly?.outside ?? 0) > 0
                                            ? <Badge variant="warning">{t('forecast.anomaly_badge')}</Badge>
                                            : <span className="text-muted-foreground">—</span>}
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        </div>
    );
}

function MachineForm({ asset, tags, onDone }: { asset: Asset | null; tags: ForecastOverviewTag[]; onDone: () => Promise<void> }) {
    const { t } = useTranslation();
    const [name, setName] = useState(asset?.name ?? '');
    const [description, setDescription] = useState(asset?.description ?? '');
    const [stateTag, setStateTag] = useState<string>(asset?.state_tag_id ? String(asset.state_tag_id) : NONE);
    const [members, setMembers] = useState<Set<number>>(() => new Set(asset?.members.map(m => m.tag_id) ?? []));
    const [query, setQuery] = useState('');
    const [busy, setBusy] = useState(false);

    const shown = useMemo(() => {
        const q = query.trim().toLowerCase();
        return tags.filter(tg => !q || tg.alias.toLowerCase().includes(q) || tg.gateway_name.toLowerCase().includes(q)
            || tg.area_name.toLowerCase().includes(q)).slice(0, 300);
    }, [tags, query]);
    const bools = useMemo(() => tags.filter(tg => tg.data_type === 'BOOL'), [tags]);
    const toggle = (id: number, on: boolean) => setMembers(m => { const n = new Set(m); if (on) n.add(id); else n.delete(id); return n; });

    const save = async () => {
        const input: AssetInput = {
            name: name.trim(), description: description.trim(),
            state_tag_id: stateTag === NONE ? null : Number(stateTag), tag_ids: [...members],
        };
        setBusy(true);
        try {
            if (asset) await assetsApi.update(asset.id, input); else await assetsApi.create(input);
            toast.success(t('forecast.machine_saved'));
            await onDone();
        } catch (e) { showApiError(e, t('forecast.failed')); } finally { setBusy(false); }
    };

    return (
        <>
            <DialogHeader>
                <DialogTitle>{asset ? t('forecast.machine_edit_title') : t('forecast.machines_new')}</DialogTitle>
                <DialogDescription>{t('forecast.machine_form_hint')}</DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
                <div className="grid gap-4 sm:grid-cols-2">
                    <div className="grid gap-1.5">
                        <Label htmlFor="m-name">{t('common.name')}</Label>
                        <Input id="m-name" value={name} onChange={e => setName(e.target.value)} maxLength={120} />
                    </div>
                    <div className="grid gap-1.5">
                        <Label htmlFor="m-state">{t('forecast.machine_state_field')}</Label>
                        <Select value={stateTag} onValueChange={setStateTag}>
                            <SelectTrigger id="m-state"><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value={NONE}>{t('forecast.machine_state_none')}</SelectItem>
                                {bools.map(tg => <SelectItem key={tg.tag_id} value={String(tg.tag_id)}>{tg.alias} · {tg.gateway_name}</SelectItem>)}
                            </SelectContent>
                        </Select>
                    </div>
                </div>
                <div className="grid gap-1.5">
                    <Label htmlFor="m-desc">{t('forecast.machine_description')}</Label>
                    <Input id="m-desc" value={description} onChange={e => setDescription(e.target.value)} />
                </div>
                <div className="space-y-2">
                    <div className="flex items-center justify-between gap-2">
                        <Label>{t('forecast.machine_tags', { count: members.size })}</Label>
                    </div>
                    <div className="relative">
                        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                        <Input value={query} onChange={e => setQuery(e.target.value)} placeholder={t('forecast.search')} className="pl-9" />
                    </div>
                    <ul className="max-h-64 space-y-1 overflow-y-auto rounded-md border p-2">
                        {shown.map(tg => (
                            <li key={tg.tag_id}>
                                <label className="flex cursor-pointer items-center gap-3 rounded px-2 py-1.5 text-sm hover:bg-muted/50">
                                    <Checkbox checked={members.has(tg.tag_id)} onCheckedChange={v => toggle(tg.tag_id, v === true)} />
                                    <span className="min-w-0 flex-1 sm:flex sm:items-baseline sm:gap-3">
                                        <span className="block truncate sm:flex-1">{tg.alias}</span>
                                        <span className="block truncate text-xs text-muted-foreground sm:max-w-[45%]">{tg.gateway_name}</span>
                                    </span>
                                </label>
                            </li>
                        ))}
                    </ul>
                </div>
            </div>
            <DialogFooter>
                <Button onClick={() => void save()} disabled={busy || !name.trim()}>{t('forecast.save')}</Button>
            </DialogFooter>
        </>
    );
}
