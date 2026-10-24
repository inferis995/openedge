import { useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
    Building2, ChevronRight, Cpu, Factory, ListChecks, Lock, MapPin, Search, Sparkles, TrendingUp,
} from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { SettingsCard } from '@/components/settings/SettingsCard';
import { UPlotChart, type PenSeries } from '@/components/historian/UPlotChart';
import { ForecastProfileForm } from '@/components/forecast/ForecastProfileForm';
import { TagForecastTab } from '@/components/tags/TagForecastTab';
import { FORECAST_STATUS_VARIANT, forecastProfileKey, isSerious, tagForecastKey } from '@/lib/forecastUi';
import { ForecastRiskList } from '@/components/forecast/ForecastRisks';
import { MachinesSection } from '@/components/forecast/MachinesSection';
import { ProductionSection } from '@/components/forecast/ProductionSection';
import { SetupGuide } from '@/components/forecast/SetupGuide';
import { setupProgress, useSetupSteps } from '@/hooks/useForecastSetup';
import {
    forecastApi, forecastOverviewApi, type ForecastOverviewTag, type ForecastScope, type ForecastStatus,
} from '@/api/forecast';
import { historyApi } from '@/api/history';
import { tagsApi } from '@/api/tags';
import { alignPens } from '@/lib/trendAlign';
import { forecastEnd, forecastPenId, forecastPoints, isDrawable, lastReading } from '@/lib/forecastTrend';
import { formatLiveValue } from '@/lib/format';
import { timeAgo } from '@/lib/relativeTime';
import { useAuthStore } from '@/stores/useAuthStore';
import { useNavigationStore } from '@/stores/useNavigationStore';
import type { TrendDataPoint } from '@/types/trend';

type StatusFilter = 'all' | ForecastStatus | 'other' | 'at_risk' | 'anomalous';
const OTHER: ForecastStatus[] = ['disabled', 'error', 'pending'];

/**
 * Forecasts, for the organization: where every tag is going, how far that can
 * be trusted, and the settings that shape them, level by level.
 */
export default function ForecastsPage() {
    const { t } = useTranslation();
    const { selectedOrgId } = useNavigationStore();
    const [params, setParams] = useSearchParams();
    const tab = ['settings', 'machines', 'production'].includes(params.get('tab') ?? '') ? params.get('tab') as string : 'tags';
    const setTab = (v: string) => setParams(p => { p.set('tab', v); return p; }, { replace: true });

    const { data, isLoading, isError } = useQuery({
        queryKey: ['forecast-overview', selectedOrgId],
        queryFn: () => forecastOverviewApi.get(),
        enabled: !!selectedOrgId,
        refetchInterval: 60_000,
    });
    const steps = useSetupSteps(data);
    const progress = setupProgress(steps);
    const { isAdmin } = useAuthStore();

    if (!selectedOrgId) {
        return <EmptyState text={t('forecast.select_org')} />;
    }

    const hasModel = (data?.engines ?? []).some(e => e !== 'statistical');
    return (
        <div className="space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div>
                    <h1 className="text-2xl font-semibold tracking-tight">{t('forecast.page_title')}</h1>
                    <p className="text-sm text-muted-foreground">{t('forecast.page_subtitle')}</p>
                </div>
                {data && (
                    <div className="flex h-8 w-fit items-center gap-2 rounded-full border bg-card px-3 text-xs text-muted-foreground"
                        title={hasModel ? undefined : t('forecast.no_model')}>
                        <Sparkles className="size-3.5" />
                        {t('forecast.engines')}: {data.engines.map(e => t(`forecast.engine_${e}`, { defaultValue: e })).join(' · ') || '—'}
                    </div>
                )}
            </div>

            <Tabs value={tab} onValueChange={setTab}>
                <TabsList>
                    <TabsTrigger value="tags">{t('forecast.tab_tags')}</TabsTrigger>
                    <TabsTrigger value="machines">{t('forecast.tab_machines')}</TabsTrigger>
                    <TabsTrigger value="production">{t('forecast.tab_production')}</TabsTrigger>
                    <TabsTrigger value="settings">{t('forecast.tab_settings')}</TabsTrigger>
                </TabsList>
                <TabsContent value="tags" className="mt-6 space-y-6">
                    {isAdmin() && progress.total > 0 && progress.done < progress.total && (
                        <div className="flex flex-col gap-3 rounded-lg border border-amber-500/30 bg-amber-500/5 p-4 sm:flex-row sm:items-center sm:justify-between">
                            <div className="flex items-start gap-3">
                                <ListChecks className="mt-0.5 size-5 shrink-0 text-amber-600 dark:text-amber-400" />
                                <div>
                                    <p className="text-sm font-medium">{t('forecast.setup_banner', progress)}</p>
                                    <p className="text-xs text-muted-foreground">
                                        {steps.filter(st => st.state === 'todo').map(st => st.title).join(' · ')}
                                    </p>
                                </div>
                            </div>
                            <Button size="sm" variant="outline" className="shrink-0" onClick={() => setTab('settings')}>{t('forecast.setup_complete')}</Button>
                        </div>
                    )}
                    {isLoading && <p className="text-sm text-muted-foreground">{t('forecast.loading')}</p>}
                    {isError && <p className="text-sm text-destructive">{t('forecast.failed')}</p>}
                    {data && <TagsSection tags={data.tags} />}
                </TabsContent>
                <TabsContent value="machines" className="mt-6">
                    <MachinesSection tags={data?.tags ?? []} />
                </TabsContent>
                <TabsContent value="production" className="mt-6">
                    <ProductionSection tags={data?.tags ?? []} />
                </TabsContent>
                <TabsContent value="settings" className="mt-6 space-y-6">
                    <SetupGuide steps={steps} onTab={setTab} />
                    <SettingsSection orgId={selectedOrgId} tags={data?.tags ?? []} />
                </TabsContent>
            </Tabs>
        </div>
    );
}

function EmptyState({ text }: { text: string }) {
    return (
        <div className="rounded-lg border border-dashed p-10 text-center text-sm text-muted-foreground">{text}</div>
    );
}

// ── Tags ─────────────────────────────────────────────────────────────────────

function TagsSection({ tags }: { tags: ForecastOverviewTag[] }) {
    const { t, i18n } = useTranslation();
    const [query, setQuery] = useState('');
    const [site, setSite] = useState('all');
    const [status, setStatus] = useState<StatusFilter>('all');
    const [open, setOpen] = useState<ForecastOverviewTag | null>(null);

    const sites = useMemo(() => {
        const m = new Map<number, string>();
        for (const tg of tags) m.set(tg.site_id, tg.site_name);
        return [...m.entries()].sort((a, b) => a[1].localeCompare(b[1]));
    }, [tags]);
    const inSite = useMemo(() => tags.filter(tg => site === 'all' || String(tg.site_id) === site), [tags, site]);
    const count = (s: StatusFilter) => inSite.filter(tg => matchFilter(tg, s)).length;
    const shown = useMemo(() => {
        const q = query.trim().toLowerCase();
        return inSite.filter(tg => matchFilter(tg, status) && (!q
            || tg.alias.toLowerCase().includes(q) || tg.gateway_name.toLowerCase().includes(q)
            || tg.area_name.toLowerCase().includes(q)));
    }, [inSite, status, query]);

    if (tags.length === 0) return <EmptyState text={t('forecast.empty')} />;

    const tiles: { key: StatusFilter; label: string; tone: string }[] = [
        { key: 'at_risk', label: t('forecast.tile_at_risk'), tone: 'bg-red-500' },
        { key: 'anomalous', label: t('forecast.tile_anomalous'), tone: 'bg-orange-500' },
        { key: 'ok', label: t('forecast.tile_ok'), tone: 'bg-emerald-500' },
        { key: 'unreliable', label: t('forecast.tile_unreliable'), tone: 'bg-amber-500' },
        { key: 'learning', label: t('forecast.tile_learning'), tone: 'bg-sky-500' },
        { key: 'other', label: t('forecast.tile_other'), tone: 'bg-muted-foreground/50' },
    ];

    return (
        <>
            {/* Summary: each tile filters the list */}
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
                {tiles.map(tile => (
                    <button key={tile.key} type="button" onClick={() => setStatus(s => (s === tile.key ? 'all' : tile.key))}
                        aria-pressed={status === tile.key}
                        className={`rounded-lg border bg-card p-4 text-left transition-colors hover:bg-muted/50 ${status === tile.key ? 'border-foreground/40 ring-1 ring-foreground/20' : ''}`}>
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                            <span className={`size-2 rounded-full ${tile.tone}`} />{tile.label}
                        </div>
                        <div className="mt-1 text-2xl font-semibold tabular-nums">{count(tile.key)}</div>
                    </button>
                ))}
            </div>

            {/* Filters */}
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <div className="relative sm:max-w-xs sm:flex-1">
                    <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input value={query} onChange={e => setQuery(e.target.value)} placeholder={t('forecast.search')}
                        className="pl-9" aria-label={t('forecast.search')} />
                </div>
                {sites.length > 1 && (
                    <Select value={site} onValueChange={setSite}>
                        <SelectTrigger className="sm:w-56" aria-label={t('forecast.level_site')}><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value="all">{t('forecast.all_sites')}</SelectItem>
                            {sites.map(([id, name]) => <SelectItem key={id} value={String(id)}>{name}</SelectItem>)}
                        </SelectContent>
                    </Select>
                )}
                <Select value={status} onValueChange={v => setStatus(v as StatusFilter)}>
                    <SelectTrigger className="sm:w-56" aria-label={t('forecast.col_status')}><SelectValue /></SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">{t('forecast.all_statuses')} ({inSite.length})</SelectItem>
                        {(['ok', 'unreliable', 'learning', 'disabled', 'error', 'pending'] as ForecastStatus[]).map(s => (
                            <SelectItem key={s} value={s}>{t(`forecast.status_${s}`)} ({count(s)})</SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            </div>

            {shown.length === 0 ? <EmptyState text={t('forecast.empty_filtered')} /> : (<>
                {/* Phone: one card per tag */}
                <ul className="space-y-2 sm:hidden">
                    {shown.map(tg => (
                        <li key={tg.tag_id}>
                            <button type="button" onClick={() => setOpen(tg)}
                                className="w-full rounded-lg border bg-card p-3 text-left active:bg-muted/60">
                                <div className="flex items-start justify-between gap-3">
                                    <div className="min-w-0">
                                        <div className="truncate font-medium">{tg.alias}</div>
                                        <div className="truncate text-xs text-muted-foreground">{tg.area_name} · {tg.gateway_name}</div>
                                    </div>
                                    <Badge variant={FORECAST_STATUS_VARIANT[tg.status]} className="shrink-0">{t(`forecast.status_${tg.status}`)}</Badge>
                                </div>
                                {(tg.risks ?? []).length > 0 && <div className="mt-2"><RiskCell tag={tg} /></div>}
                                <div className="mt-2 flex items-end justify-between gap-3">
                                    <SkillBar skill={tg.skill} />
                                    <span className="text-right tabular-nums"><Expected tag={tg} /></span>
                                </div>
                            </button>
                        </li>
                    ))}
                </ul>
                <div className="hidden rounded-lg border bg-card sm:block">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>{t('forecast.col_tag')}</TableHead>
                                <TableHead>{t('forecast.col_kind')}</TableHead>
                                <TableHead>{t('forecast.col_status')}</TableHead>
                                <TableHead>{t('forecast.col_risk')}</TableHead>
                                <TableHead className="text-right">{t('forecast.col_skill')}</TableHead>
                                <TableHead className="text-right">{t('forecast.col_expected')}</TableHead>
                                <TableHead>{t('forecast.col_generated')}</TableHead>
                                <TableHead className="w-8" />
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {shown.map(tg => (
                                <TableRow key={tg.tag_id} className="cursor-pointer" onClick={() => setOpen(tg)}>
                                    <TableCell>
                                        <div className="font-medium">{tg.alias}</div>
                                        <div className="text-xs text-muted-foreground">{tg.site_name} · {tg.area_name} · {tg.gateway_name}</div>
                                    </TableCell>
                                    <TableCell className="text-sm">{tg.kind ? t(`forecast.kind_short_${tg.kind}`) : '—'}</TableCell>
                                    <TableCell>
                                        <Badge variant={FORECAST_STATUS_VARIANT[tg.status]} title={tg.reason}>{t(`forecast.status_${tg.status}`)}</Badge>
                                    </TableCell>
                                    <TableCell><RiskCell tag={tg} /></TableCell>
                                    <TableCell className="text-right"><SkillBar skill={tg.skill} /></TableCell>
                                    <TableCell className="text-right tabular-nums">
                                        <Expected tag={tg} />
                                    </TableCell>
                                    <TableCell className="text-sm text-muted-foreground">
                                        {tg.generated_at ? timeAgo(Date.parse(tg.generated_at), i18n.language) : '—'}
                                    </TableCell>
                                    <TableCell><ChevronRight className="size-4 text-muted-foreground" /></TableCell>
                                </TableRow>
                            ))}
                        </TableBody>
                    </Table>
                </div>
            </>)}

            <Dialog open={!!open} onOpenChange={o => { if (!o) setOpen(null); }}>
                {open && (
                    <DialogContent className="max-w-3xl">
                        <DialogHeader>
                            <DialogTitle>{open.alias}</DialogTitle>
                            <DialogDescription>{open.site_name} · {open.area_name} · {open.gateway_name}</DialogDescription>
                        </DialogHeader>
                        <ForecastChart tag={open} />
                        <TagForecastTab tagId={open.tag_id} unit={open.unit} />
                    </DialogContent>
                )}
            </Dialog>
        </>
    );
}

function matchFilter(tg: ForecastOverviewTag, f: StatusFilter) {
    if (f === 'at_risk') return (tg.risks ?? []).some(isSerious);
    if (f === 'anomalous') return (tg.anomaly?.outside ?? 0) > 0;
    return f === 'all' || (f === 'other' ? OTHER.includes(tg.status) : tg.status === f);
}

/** The soonest rule the forecast may trip, and how many more. */
function RiskCell({ tag }: { tag: ForecastOverviewTag }) {
    const { t } = useTranslation();
    const risks = tag.risks ?? [];
    const anomalous = (tag.anomaly?.outside ?? 0) > 0;
    if (risks.length === 0) {
        return anomalous
            ? <Badge variant="warning">{t('forecast.anomaly_badge')}</Badge>
            : <span className="text-muted-foreground">—</span>;
    }
    return (
        <div className="space-y-1">
            <ForecastRiskList risks={risks.slice(0, 1)} unit={tag.unit} compact />
            {risks.length > 1 && <span className="text-xs text-muted-foreground">{t('forecast.risks_more', { count: risks.length - 1 })}</span>}
            {anomalous && <Badge variant="warning">{t('forecast.anomaly_badge')}</Badge>}
        </div>
    );
}

/** How much better than repeating the last value, as a bar: 0 to 100%. */
function SkillBar({ skill }: { skill?: number }) {
    if (skill === undefined) return <span className="text-muted-foreground">—</span>;
    const p = Math.max(0, Math.min(1, skill));
    return (
        <span className="inline-flex items-center gap-2">
            <span className="h-1.5 w-14 overflow-hidden rounded-full bg-muted">
                <span className={`block h-full ${skill > 0 ? 'bg-emerald-500' : 'bg-amber-500'}`} style={{ width: `${Math.round(p * 100)}%` }} />
            </span>
            <span className="w-9 text-right tabular-nums text-sm">{Math.round(skill * 100)}%</span>
        </span>
    );
}

function Expected({ tag }: { tag: ForecastOverviewTag }) {
    const { t, i18n } = useTranslation();
    const e = tag.end;
    if (tag.status !== 'ok' || !e || e.q50 === null) return <span className="text-muted-foreground">—</span>;
    const fmt = (v: number | null) => (v === null ? '—' : formatLiveValue(v, undefined, i18n.language));
    return (
        <span className="inline-flex flex-col items-end">
            <span className="font-medium">{fmt(e.q50)}{tag.unit ? ` ${tag.unit}` : ''}</span>
            <span className="text-xs text-muted-foreground">{fmt(e.q10)}–{fmt(e.q90)} · {t('forecast.expected_in', { h: tag.horizon_hours })}</span>
        </span>
    );
}

/** The tag's recent history with its forecast after now. */
function ForecastChart({ tag }: { tag: ForecastOverviewTag }) {
    const { t } = useTranslation();
    const [window] = useState(() => {
        const now = Date.now();
        const span = Math.min(30, Math.max(1, (tag.horizon_hours * 3) / 24)) * 86400e3;
        return { from: now - span, to: now };
    });
    const { data: fc } = useQuery({
        queryKey: tagForecastKey(tag.tag_id),
        queryFn: () => forecastApi.getTagForecast(tag.tag_id),
    });
    const { data: hist } = useQuery({
        queryKey: ['forecast-history', tag.tag_id, window.from],
        queryFn: () => historyApi.batchQuery({
            tag_ids: [tag.tag_id], start: new Date(window.from).toISOString(), end: new Date(window.to).toISOString(),
            agg: 'mean', points: 400,
        }),
    });
    const color = '#2563eb';
    const { pens, timestamps, values, bands, ahead } = useMemo(() => {
        const series = (hist as Record<number, TrendDataPoint[]> | undefined)?.[tag.tag_id] ?? [];
        const data: Record<number, TrendDataPoint[]> = { [tag.tag_id]: series };
        const ps: PenSeries[] = [{ tagId: tag.tag_id, label: tag.alias, unit: tag.unit, color, visible: true, step: tag.data_type === 'BOOL' }];
        let ahd = 0;
        if (isDrawable(fc)) {
            data[forecastPenId(tag.tag_id)] = forecastPoints(fc, lastReading(series));
            ps.push({ tagId: forecastPenId(tag.tag_id), label: `${tag.alias} (${t('trendPage.forecast_suffix')})`, unit: tag.unit, color, visible: true, ghost: true });
            ahd = Math.max(0, forecastEnd([fc]) - window.to);
        }
        return { pens: ps, ahead: ahd, ...alignPens(ps, data) };
    }, [hist, fc, tag, t, window.to]);

    const trendLink = `/trend?pens=${tag.tag_id}:${color.slice(1)}&range=${tag.horizon_hours > 48 ? '7d' : '24h'}&live=1&fc=1`;
    return (
        <section className="space-y-2">
            <div className="flex items-center justify-between gap-2">
                <h3 className="text-sm font-semibold">{t('forecast.chart_title')}</h3>
                <Button variant="outline" size="sm" className="gap-1.5" asChild>
                    <Link to={trendLink}><TrendingUp className="size-4" /> {t('forecast.open_trend')}</Link>
                </Button>
            </div>
            <div className="h-64 rounded-lg border bg-card p-2">
                <UPlotChart timestamps={timestamps} values={values} bands={bands} pens={pens} range={window} ahead={ahead} />
            </div>
        </section>
    );
}

// ── Settings by level ──────────────────────────────────────────────────────────

interface Level {
    scope: ForecastScope;
    id: number;
    name: string;
    depth: number;
    /** The names of the levels above, for the header. */
    path: string[];
}

function SettingsSection({ orgId, tags }: { orgId: number; tags: ForecastOverviewTag[] }) {
    const { t } = useTranslation();
    const { isAdmin } = useAuthStore();
    const { data: hier } = useQuery({
        queryKey: ['tags-hierarchy'],
        queryFn: () => tagsApi.getHierarchy(),
        staleTime: 60_000,
    });
    const levels = useMemo<Level[]>(() => {
        const org = hier?.organizations?.find(o => o.id === orgId) ?? hier?.organizations?.[0];
        if (!org) return [];
        const out: Level[] = [{ scope: 'org', id: org.id, name: org.name, depth: 0, path: [] }];
        for (const s of org.sites ?? []) {
            out.push({ scope: 'site', id: s.id, name: s.name, depth: 1, path: [org.name] });
            for (const a of s.areas ?? []) {
                out.push({ scope: 'area', id: a.id, name: a.name, depth: 2, path: [org.name, s.name] });
                for (const g of a.gateways ?? []) {
                    out.push({ scope: 'gateway', id: g.id, name: g.name, depth: 3, path: [org.name, s.name, a.name] });
                }
            }
        }
        return out;
    }, [hier, orgId]);
    const { data: custom } = useQuery({
        queryKey: ['forecast-custom-profiles', orgId],
        queryFn: () => forecastApi.customProfiles(),
        staleTime: 60_000,
    });
    const own = useMemo(() => new Set((custom ?? []).map(c => `${c.scope}:${c.scope_id}`)), [custom]);
    const [selKey, setSelKey] = useState<string | null>(null);
    const formRef = useRef<HTMLDivElement>(null);
    const select = (key: string) => {
        setSelKey(key);
        // On a phone the form is under the list: bring it into view.
        if (window.matchMedia('(max-width: 1023px)').matches) {
            requestAnimationFrame(() => formRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }));
        }
    };
    const sel = levels.find(l => `${l.scope}:${l.id}` === selKey) ?? levels[0];

    const { data: profile } = useQuery({
        queryKey: sel ? forecastProfileKey(sel.scope, sel.id) : ['forecast-profile', 'none'],
        queryFn: () => (sel ? forecastApi.getProfile(sel.scope, sel.id) : Promise.reject(new Error('no level'))),
        enabled: !!sel,
    });

    if (!sel) return <p className="text-sm text-muted-foreground">{t('forecast.loading')}</p>;

    const applies = tags.filter(tg => sel.scope === 'org'
        || (sel.scope === 'site' && tg.site_id === sel.id)
        || (sel.scope === 'area' && tg.area_id === sel.id)
        || (sel.scope === 'gateway' && tg.gateway_id === sel.id)).length;
    const icon = { org: Building2, site: Factory, area: MapPin, gateway: Cpu, tag: Cpu }[sel.scope];
    const Icon = icon;

    return (
        <div className="grid gap-6 lg:grid-cols-[280px_minmax(0,1fr)] lg:items-start">
            <nav aria-label={t('forecast.tab_settings')} className="rounded-lg border bg-card p-2 lg:sticky lg:top-4">
                <p className="flex items-center justify-between gap-2 px-2 pb-2 pt-1 text-xs font-medium text-muted-foreground">
                    {t('forecast.levels')}
                    {own.size > 0 && <span className="inline-flex items-center gap-1 font-normal"><span className="size-1.5 rounded-full bg-primary" />{t('forecast.level_custom')}</span>}
                </p>
                <ul className="max-h-72 space-y-0.5 overflow-y-auto lg:max-h-[70vh]">
                    {levels.map(l => {
                        const LIcon = { org: Building2, site: Factory, area: MapPin, gateway: Cpu, tag: Cpu }[l.scope];
                        const active = l === sel;
                        return (
                            <li key={`${l.scope}:${l.id}`}>
                                <button type="button" onClick={() => select(`${l.scope}:${l.id}`)} aria-current={active ? 'true' : undefined}
                                    className={`flex w-full min-w-0 items-center gap-2 rounded-md py-1.5 pr-2 text-left text-sm transition-colors ${active ? 'bg-muted font-medium text-foreground' : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'}`}
                                    style={{ paddingLeft: `${0.5 + l.depth * 0.9}rem` }}>
                                    <LIcon className="size-4 shrink-0" />
                                    <span className="truncate">{l.name}</span>
                                    {own.has(`${l.scope}:${l.id}`) && (
                                        <span className="ml-auto size-1.5 shrink-0 rounded-full bg-primary" title={t('forecast.level_custom')} aria-label={t('forecast.level_custom')} />
                                    )}
                                </button>
                            </li>
                        );
                    })}
                </ul>
            </nav>

            <div ref={formRef} className="min-w-0 scroll-mt-4 space-y-4">
                <p className="text-sm text-muted-foreground">{t('forecast.settings_intro')}</p>
                <SettingsCard
                    title={<span className="inline-flex items-center gap-2"><Icon className="size-4" />{t(`forecast.level_${sel.scope}`)}: {sel.name}</span>}
                    description={<>
                        {sel.path.length > 0 && <>{t('forecast.inherits_from', { path: sel.path.join(' › ') })} · </>}
                        {t('forecast.applies_to', { count: applies })}
                    </>}
                >
                    {!isAdmin() && (
                        <p className="mb-4 flex items-center gap-2 text-xs text-muted-foreground"><Lock className="size-3.5" />{t('forecast.readonly')}</p>
                    )}
                    {profile
                        ? <ForecastProfileForm key={`${sel.scope}:${sel.id}:${JSON.stringify(profile.settings)}`}
                            scope={sel.scope} scopeId={sel.id} profile={profile} canEdit={isAdmin()} />
                        : <p className="text-sm text-muted-foreground">{t('forecast.loading')}</p>}
                </SettingsCard>
            </div>
        </div>
    );
}
