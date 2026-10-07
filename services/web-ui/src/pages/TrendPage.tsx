import { useState, useMemo, useCallback, useEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useSearchParams } from 'react-router-dom';
import {
    Activity, Play, Pause, RefreshCw, Download, PanelLeft, PanelRight,
    BarChart2, Settings2, ZoomOut, ChevronLeft, ChevronRight, Ruler, GitCompareArrows, MoveVertical,
    Bookmark, Trash2,
} from 'lucide-react';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { toast } from 'sonner';
import { useTranslation } from 'react-i18next';

import { TagBrowser } from '@/components/trend/TagBrowser';
import { UPlotChart, type PenSeries, type ChartEvent } from '@/components/historian/UPlotChart';
import { HistorianDatePicker, type HistorianRange } from '@/components/historian/HistorianDatePicker';
import { PenPanel, type Pen, type PenStats } from '@/components/historian/PenPanel';

import { historyApi } from '@/api/history';
import { tagsApi } from '@/api/tags';
import { gatewaysApi } from '@/api/gateways';
import { alarmsApi } from '@/api/alarms';
import { trendViewsApi } from '@/api/trendViews';
import { alignPens, mergeSeries, realtimePoint, shiftPoints, valueAt } from '@/lib/trendAlign';
import { compareOffset, decodeView, encodeView, resolveRange, TREND_VIEW_KEY, type CompareMode, type TrendView, type YAxisMode } from '@/lib/trendView';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Input } from '@/components/ui/input';
import { useRealtime } from '@/hooks/useRealtime';
import { useAuthStore } from '@/stores/useAuthStore';
import { type TrendDataPoint, type AggregationType, type TagWithHierarchy } from '@/types/trend';

// ── Constants ────────────────────────────────────────────────────────────────

const PEN_COLORS = [
    '#3b82f6', '#ef4444', '#22c55e', '#f59e0b', '#8b5cf6',
    '#06b6d4', '#ec4899', '#f97316', '#6366f1', '#14b8a6',
    '#a855f7', '#eab308', '#84cc16', '#f43f5e', '#22d3ee',
];

const AGG_OPTIONS: { value: AggregationType; labelKey: string }[] = [
    { value: 'mean',  labelKey: 'trendPage.agg_mean'  },
    { value: 'max',   labelKey: 'trendPage.agg_max'   },
    { value: 'min',   labelKey: 'trendPage.agg_min'   },
    { value: 'last',  labelKey: 'trendPage.agg_last'  },
];

// ── Helpers ───────────────────────────────────────────────────────────────────

/** One axis in the axes popover: automatic, the tag's EU range, or fixed bounds. */
function AxisRow({ label, eu, mode, onChange }: {
    label: string;
    eu: { min: number; max: number } | null;
    mode: YAxisMode | undefined;
    onChange: (m: YAxisMode | 'auto') => void;
}) {
    const { t } = useTranslation();
    const kind = mode === undefined ? 'auto' : mode === 'eu' ? 'eu' : 'fixed';
    const fixed = mode && mode !== 'eu' ? mode : eu ?? { min: 0, max: 100 };
    const [lo, setLo] = useState(String(fixed.min));
    const [hi, setHi] = useState(String(fixed.max));
    const commit = () => {
        const a = Number(lo), b = Number(hi);
        if (Number.isFinite(a) && Number.isFinite(b) && b > a) onChange({ min: a, max: b });
    };
    return (
        <div className="space-y-1.5">
            <div className="flex items-center gap-2">
                <span className="text-xs font-medium flex-1 truncate">{label}</span>
                <Select value={kind} onValueChange={v => {
                    if (v === 'auto') onChange('auto');
                    else if (v === 'eu') onChange('eu');
                    else onChange({ min: Number(lo) || 0, max: Number(hi) > Number(lo) ? Number(hi) : (Number(lo) || 0) + 1 });
                }}>
                    <SelectTrigger className="h-7 w-40 text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>
                        <SelectItem value="auto">{t('trendPage.axis_auto')}</SelectItem>
                        <SelectItem value="eu" disabled={!eu}>
                            {eu ? t('trendPage.axis_eu', { min: eu.min, max: eu.max }) : t('trendPage.axis_eu_none')}
                        </SelectItem>
                        <SelectItem value="fixed">{t('trendPage.axis_fixed')}</SelectItem>
                    </SelectContent>
                </Select>
            </div>
            {kind === 'fixed' && (
                <div className="flex items-center gap-2 pl-1">
                    <Input className="h-7 text-xs" inputMode="decimal" value={lo} aria-label="min"
                        onChange={e => setLo(e.target.value)} onBlur={commit} onKeyDown={e => e.key === 'Enter' && commit()} />
                    <span className="text-xs text-muted-foreground">–</span>
                    <Input className="h-7 text-xs" inputMode="decimal" value={hi} aria-label="max"
                        onChange={e => setHi(e.target.value)} onBlur={commit} onKeyDown={e => e.key === 'Enter' && commit()} />
                </div>
            )}
        </div>
    );
}

/** "1 h 30 min", "45 s". */
function fmtDuration(ms: number): string {
    const s = Math.round(ms / 1000);
    const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
    return [d && `${d} d`, h && `${h} h`, m && `${m} min`, !d && !h && r && `${r} s`].filter(Boolean).join(' ') || '0 s';
}

/** Values the chart can show: about one per CSS pixel, within the server's bounds. */
function chartPoints(): number {
    return Math.max(300, Math.min(2000, Math.round(window.innerWidth)));
}

function savedView(): TrendView | null {
    try {
        const s = localStorage.getItem(TREND_VIEW_KEY);
        return s ? decodeView(new URLSearchParams(s)) : null;
    } catch {
        return null;
    }
}

function defaultRange(): HistorianRange {
    const to = new Date();
    const from = new Date(to.getTime() - 60 * 60_000); // last 1h
    return { from, to, preset: '1h' };
}

function computeStats(pts: TrendDataPoint[]): PenStats {
    // A loop, not Math.min(...vals): spreading a long series overflows the
    // call stack (a month of 1-second data is 2.6 million arguments).
    let min = Infinity, max = -Infinity, sum = 0, count = 0, last: number | null = null;
    for (const p of pts) {
        if (p.quality !== 0 || p.value === null) continue;
        const v = p.value;
        // A bucket's own extremes, not its average: a spike stays the max.
        if ((p.min ?? v) < min) min = p.min ?? v;
        if ((p.max ?? v) > max) max = p.max ?? v;
        sum += v; count++; last = v;
    }
    if (count === 0) return { min: null, max: null, avg: null, last: null, count: 0 };
    return { min, max, avg: sum / count, last, count };
}

/** Below this width the side panels float over the chart instead of squeezing it. */
const WIDE = '(min-width: 1024px)';
function useIsWide(): boolean {
    const [wide, setWide] = useState(() => typeof window === 'undefined' || window.matchMedia(WIDE).matches);
    useEffect(() => {
        const mq = window.matchMedia(WIDE);
        const on = () => setWide(mq.matches);
        mq.addEventListener('change', on);
        return () => mq.removeEventListener('change', on);
    }, []);
    return wide;
}

function exportCSV(pens: Pen[], timestamps: number[], values: (number | null | undefined)[][]): void {
    if (timestamps.length === 0) return;
    const headers = ['Timestamp', ...pens.map(p => p.label + (p.unit ? ` (${p.unit})` : ''))];
    const rows = timestamps.map((ts, i) => {
        const date = new Date(ts * 1000).toISOString();
        const vals = values.map(v => v[i] === null || v[i] === undefined ? '' : String(v[i]));
        return [date, ...vals].join(',');
    });
    const csv = [headers.join(','), ...rows].join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `historian_${new Date().toISOString().slice(0, 16)}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function TrendPage() {
    const { t: tr, i18n } = useTranslation();
    // The view comes from the address (a reload, a shared link), else from the
    // last one seen; it used to start empty on every reload.
    const [searchParams, setSearchParams] = useSearchParams();
    const [initialView] = useState<TrendView | null>(() => decodeView(searchParams) ?? savedView());
    const [timeRange, setTimeRange] = useState<HistorianRange>(() => initialView ? resolveRange(initialView.range) : defaultRange());
    const [liveMode, setLiveMode] = useState(() => initialView?.live ?? false);
    const [aggregation, setAggregation] = useState<AggregationType>(() => initialView?.agg ?? 'mean');
    const [compare, setCompare] = useState<CompareMode>(() => initialView?.cmp ?? 'off');
    const [yModes, setYModes] = useState<Record<string, YAxisMode>>(() => initialView?.y ?? {});
    const [measure, setMeasure] = useState(false);
    const [marks, setMarks] = useState<{ a: number | null; b: number | null }>({ a: null, b: null });
    // A first click places A, a second B, a third starts again.
    const onMeasureClick = useCallback((ms: number) => {
        setMarks(m => (m.a === null ? { a: ms, b: null } : m.b === null ? { a: m.a, b: ms } : { a: ms, b: null }));
    }, []);
    const [rawPens, setPens] = useState<Pen[]>(() => (initialView?.pens ?? []).map(p => ({
        tagId: p.tagId, label: `#${p.tagId}`, color: p.color, visible: p.visible,
    })));
    const isWide = useIsWide();
    // On a phone both panels start closed: open, they took the whole width and
    // left the chart none.
    const [sidebarOpen, setSidebarOpen] = useState(() => typeof window === 'undefined' || window.matchMedia(WIDE).matches);
    const [penPanelOpen, setPenPanelOpen] = useState(() => typeof window === 'undefined' || window.matchMedia(WIDE).matches);
    const [zoomStack, setZoomStack] = useState<HistorianRange[]>([]);
    const liveRef = useRef<ReturnType<typeof setInterval> | null>(null);

    // Keep the address (and the saved view) in step with what is shown.
    const viewQuery = useMemo(() => encodeView({
        pens: rawPens.map(p => ({ tagId: p.tagId, color: p.color, visible: p.visible })),
        range: timeRange.preset && timeRange.preset !== 'custom'
            ? { preset: timeRange.preset }
            : { from: timeRange.from.getTime(), to: timeRange.to.getTime() },
        agg: aggregation,
        live: liveMode,
        cmp: compare,
        y: yModes,
    }).toString(), [rawPens, timeRange, aggregation, liveMode, compare, yModes]);
    useEffect(() => {
        setSearchParams(new URLSearchParams(viewQuery), { replace: true });
        try { localStorage.setItem(TREND_VIEW_KEY, viewQuery); } catch { /* private mode: the address still has it */ }
    }, [viewQuery, setSearchParams]);

    // Load all tags for label lookup
    const { data: allTags } = useQuery({
        queryKey: ['tags-with-hierarchy'],
        queryFn: () => tagsApi.getAllWithHierarchy(),
        staleTime: 60_000,
    });

    // Derived, not copied into state by an effect.
    const tagMap = useMemo(() => {
        const m = new Map<number, TagWithHierarchy>();
        for (const t of allTags ?? []) m.set(t.id, t);
        return m;
    }, [allTags]);

    // Names and units come from the tags; a pen restored from the address
    // whose tag is gone (or not this user's) is dropped once the tags are in.
    const pens = useMemo<Pen[]>(() => {
        if (!allTags) return rawPens;
        return rawPens.flatMap(p => {
            const t = tagMap.get(p.tagId);
            return t ? [{ ...p, label: t.alias || t.code, unit: t.eu_unit?.trim() || undefined }] : [];
        });
    }, [rawPens, allTags, tagMap]);

    const penIds = useMemo(() => pens.map(p => p.tagId), [pens]);
    // A preset keeps its length ("1h"); "today" or a picked range keep theirs.
    const spanMs = presetMs(timeRange.preset ?? '') ?? (timeRange.to.getTime() - timeRange.from.getTime());

    // ── Live ────────────────────────────────────────────────────────────────
    // The window's history is fetched once, when live starts (or a pen is
    // added); from then on values arrive over the realtime socket, and a short
    // tail is refetched every 30 s in case the socket missed something. It
    // used to refetch the whole window every 10 s and jump by 10 s.
    const [liveBase, setLiveBase] = useState<{ from: Date; to: Date } | null>(() => {
        if (!initialView?.live) return null;
        const r = resolveRange(initialView.range);
        return { from: r.from, to: r.to };
    });
    const [rtSeries, setRtSeries] = useState<Record<number, TrendDataPoint[]>>({});
    const [lastLiveTs, setLastLiveTs] = useState<number | null>(null);
    const resetLive = useCallback((on: boolean, span: number) => {
        const to = new Date();
        setLiveBase(on ? { from: new Date(to.getTime() - span), to } : null);
        setRtSeries({});
        setLastLiveTs(null);
    }, []);
    const toggleLive = useCallback(() => {
        setLiveMode(!liveMode);
        resetLive(!liveMode, spanMs);
    }, [liveMode, resetLive, spanMs]);

    // The picker's text follows the clock (it is only a label while live).
    useEffect(() => {
        if (liveRef.current) clearInterval(liveRef.current);
        if (!liveMode) return;
        liveRef.current = setInterval(() => {
            const to = new Date();
            setTimeRange(r => ({ ...r, from: new Date(to.getTime() - spanMs), to }));
        }, 10_000);
        return () => { if (liveRef.current) clearInterval(liveRef.current); };
    }, [liveMode, spanMs]);

    const queryRange = liveMode && liveBase ? liveBase : timeRange;

    // Fetch historical data for all selected pens
    const { data: batchData, isFetching, refetch } = useQuery({
        queryKey: ['historian-batch', penIds, queryRange.from.toISOString(), queryRange.to.toISOString(), aggregation],
        queryFn: () => historyApi.batchQuery({
            tag_ids: penIds,
            start: queryRange.from.toISOString(),
            end: queryRange.to.toISOString(),
            agg: aggregation,
            // As many values as the screen can show: the server sizes its
            // buckets to this and sends each one's min and max.
            points: chartPoints(),
        }),
        enabled: penIds.length > 0 && (!liveMode || !!liveBase),
        staleTime: 5_000,
    });

    const { data: tailData } = useQuery({
        queryKey: ['historian-tail', penIds, aggregation],
        queryFn: () => {
            const to = new Date();
            const from = new Date(to.getTime() - 2 * 60_000);
            return historyApi.batchQuery({
                tag_ids: penIds, start: from.toISOString(), end: to.toISOString(),
                agg: aggregation, points: chartPoints(),
            });
        },
        enabled: liveMode && penIds.length > 0,
        refetchInterval: liveMode ? 30_000 : false,
    });

    // The socket follows the pens' organization (a global admin's pens carry it).
    const userOrg = useAuthStore(s => s.user?.org_id);
    const liveOrg = userOrg ?? (pens.length > 0 ? tagMap.get(pens[0].tagId)?.org_id : undefined);
    const penIdSet = useMemo(() => new Set(penIds), [penIds]);
    // Every value the socket delivers while live, per tag (the hook's own map
    // keeps only the latest one).
    const onRealtime = useCallback((batch: ReadonlyMap<number, { value: unknown; timestamp: number; quality: number }>) => {
        const add: [number, TrendDataPoint][] = [];
        for (const [id, v] of batch) {
            const p = realtimePoint(v);
            if (p) add.push([id, p]);
        }
        if (add.length === 0) return;
        setRtSeries(prev => {
            const next = { ...prev };
            for (const [id, p] of add) {
                const list = next[id] ?? [];
                const last = list[list.length - 1];
                if (!last || p.timestamp > last.timestamp) next[id] = [...list, p];
            }
            return next;
        });
        setLastLiveTs(t => Math.max(t ?? 0, ...add.map(([, p]) => p.timestamp)));
    }, []);
    const { connected: rtConnected } = useRealtime(liveMode ? liveOrg : undefined, penIdSet, onRealtime);

    // What is drawn: the history, plus — while live — the tail and the socket's
    // values, without what has scrolled out of the window.
    const series = useMemo<Record<number, TrendDataPoint[]> | undefined>(() => {
        if (!batchData) return undefined;
        if (!liveMode) return batchData as Record<number, TrendDataPoint[]>;
        const edge = Math.max(liveBase?.to.getTime() ?? 0, lastLiveTs ?? 0);
        return mergeSeries(penIds, [batchData as Record<number, TrendDataPoint[]>, tailData, rtSeries],
            edge - spanMs * 1.05);
    }, [batchData, tailData, rtSeries, liveMode, liveBase, lastLiveTs, penIds, spanMs]);

    // Build uPlot-aligned data (seconds timestamps)
    // ── Comparison: the same pens over an earlier window, laid over this one ──
    const cmpOffset = compareOffset(compare, timeRange.to.getTime() - timeRange.from.getTime());
    const { data: compareData } = useQuery({
        queryKey: ['historian-compare', penIds, queryRange.from.getTime() - cmpOffset, queryRange.to.getTime() - cmpOffset, aggregation],
        queryFn: () => historyApi.batchQuery({
            tag_ids: penIds,
            start: new Date(queryRange.from.getTime() - cmpOffset).toISOString(),
            end: new Date(queryRange.to.getTime() - cmpOffset).toISOString(),
            agg: aggregation,
            points: chartPoints(),
        }),
        enabled: compare !== 'off' && penIds.length > 0,
        staleTime: 60_000,
    });
    // Comparison pens carry the tag id negated; they are drawn dashed.
    const chartPens = useMemo(() => {
        if (compare === 'off' || !compareData) return pens;
        const suffix = tr(`trendPage.compare_suffix_${compare}`);
        return [...pens, ...pens.map(p => ({ ...p, tagId: -p.tagId, label: `${p.label} (${suffix})` }))];
    }, [pens, compare, compareData, tr]);
    const chartSeriesData = useMemo(() => {
        if (!series || compare === 'off' || !compareData) return series;
        const out: Record<number, TrendDataPoint[]> = { ...series };
        for (const p of pens) {
            // Values only: the earlier window's envelope would hide this one's.
            out[-p.tagId] = shiftPoints((compareData as Record<number, TrendDataPoint[]>)[p.tagId], cmpOffset)
                .map(({ timestamp, value, quality }) => ({ timestamp, value, quality }));
        }
        return out;
    }, [series, compare, compareData, pens, cmpOffset]);

    const { timestamps, values, bands } = useMemo<ReturnType<typeof alignPens>>(() => {
        if (!chartSeriesData || chartPens.length === 0) return { timestamps: [], values: [], bands: [] };
        return alignPens(chartPens, chartSeriesData);
    }, [chartSeriesData, chartPens]);

    // The pens' alarms in the window, for the time axis.
    const { data: alarmEvents } = useQuery({
        queryKey: ['trend-alarms', penIds, liveMode ? 'live' : timeRange.from.getTime(), liveMode ? 0 : timeRange.to.getTime()],
        queryFn: () => {
            const to = liveMode ? new Date() : timeRange.to;
            const from = new Date(to.getTime() - (timeRange.to.getTime() - timeRange.from.getTime()));
            return alarmsApi.getAlarmWindow(penIds, from, to);
        },
        enabled: penIds.length > 0,
        refetchInterval: liveMode ? 30_000 : false,
        staleTime: 15_000,
    });
    const chartEvents = useMemo<ChartEvent[]>(() => (alarmEvents ?? []).map(e => ({
        start: Date.parse(e.trigger_time),
        end: e.clear_time ? Date.parse(e.clear_time) : null,
        severity: e.severity,
        label: `${tagMap.get(e.tag_id)?.alias ?? `#${e.tag_id}`}: ${e.message}`,
    })).filter(e => Number.isFinite(e.start)), [alarmEvents, tagMap]);

    // Gateways' last contact, for the pen panel.
    const { data: gateways } = useQuery({
        queryKey: ['gateways', 'trend-contact'],
        queryFn: () => gatewaysApi.getAll(),
        enabled: pens.length > 0,
        refetchInterval: 60_000,
        staleTime: 30_000,
    });

    // Compute per-pen stats, data age and gateway contact
    const pensWithStats = useMemo<Pen[]>(() => {
        if (!series) return pens;
        return pens.map(pen => {
            const pts = series[pen.tagId] ?? [];
            let lastSampleAt: number | null = null;
            for (let i = pts.length - 1; i >= 0; i--) {
                if (pts[i].quality === 0 && pts[i].value !== null) { lastSampleAt = pts[i].timestamp; break; }
            }
            const gw = gateways?.find(g => g.id === tagMap.get(pen.tagId)?.gateway_id);
            const contact = gw ? { at: gw.last_seen ?? null, online: (gw.connection_status ?? gw.status) === 'online' } : null;
            return { ...pen, stats: computeStats(pts), lastSampleAt, contact };
        });
    }, [series, pens, gateways, tagMap]);

    // Build PenSeries for uPlot
    const penSeries = useMemo<PenSeries[]>(() =>
        chartPens.map(p => ({
            tagId: p.tagId,
            label: p.label,
            unit: p.unit,
            decimals: tagMap.get(Math.abs(p.tagId))?.eu_decimals ?? undefined,
            step: tagMap.get(Math.abs(p.tagId))?.data_type === 'BOOL',
            ghost: p.tagId < 0,
            color: p.color,
            visible: p.visible,
        })), [chartPens, tagMap]);

    // ── Y axes: one per unit; each automatic, the tag's range, or fixed ──
    const axisGroups = useMemo(() => {
        const groups = new Map<string, { key: string; label: string; eu: { min: number; max: number } | null }>();
        for (const p of pens) {
            const key = p.unit?.trim() || `#${p.tagId}`;
            const t = tagMap.get(p.tagId);
            const eu = t?.scaling_eu_min != null && t?.scaling_eu_max != null && t.scaling_eu_max > t.scaling_eu_min
                ? { min: t.scaling_eu_min, max: t.scaling_eu_max } : null;
            const g = groups.get(key);
            if (!g) groups.set(key, { key, label: p.unit?.trim() || p.label, eu });
            else if (!g.eu && eu) g.eu = eu;
        }
        return [...groups.values()];
    }, [pens, tagMap]);
    const yRanges = useMemo(() => {
        const out: Record<string, { min: number; max: number }> = {};
        for (const g of axisGroups) {
            const m = yModes[g.key];
            if (m === 'eu' && g.eu) out[`y:${g.key}`] = g.eu;
            else if (m && m !== 'eu') out[`y:${g.key}`] = m;
        }
        return out;
    }, [axisGroups, yModes]);
    const setYMode = useCallback((key: string, mode: YAxisMode | 'auto') => {
        setYModes(prev => {
            const next = { ...prev };
            if (mode === 'auto') delete next[key]; else next[key] = mode;
            return next;
        });
    }, []);

    // ── Saved views of the organization ──
    const authUser = useAuthStore(s => s.user);
    const { data: savedViews, refetch: refetchViews } = useQuery({
        queryKey: ['trend-views'],
        queryFn: trendViewsApi.list,
        staleTime: 30_000,
    });
    const [viewName, setViewName] = useState('');
    const applyView = useCallback((v: TrendView) => {
        const r = resolveRange(v.range);
        setPens(v.pens.map(p => ({ tagId: p.tagId, label: `#${p.tagId}`, color: p.color, visible: p.visible })));
        setTimeRange(r);
        setAggregation(v.agg);
        setCompare(v.cmp ?? 'off');
        setYModes(v.y ?? {});
        setZoomStack([]);
        setMeasure(false);
        setLiveMode(v.live);
        resetLive(v.live, r.to.getTime() - r.from.getTime());
    }, [resetLive]);
    const saveView = useCallback(async () => {
        const name = viewName.trim();
        if (!name || pens.length === 0) return;
        try {
            await trendViewsApi.save(name, viewQuery);
            toast.success(tr('trendPage.view_saved', { name }));
            setViewName('');
            refetchViews();
        } catch (e) {
            const msg = (e as { response?: { data?: { error?: string } } }).response?.data?.error;
            toast.error(msg ?? tr('trendPage.view_save_failed'));
        }
    }, [viewName, viewQuery, pens.length, tr, refetchViews]);
    const deleteView = useCallback(async (id: number) => {
        try {
            await trendViewsApi.remove(id);
            refetchViews();
        } catch (e) {
            const msg = (e as { response?: { data?: { error?: string } } }).response?.data?.error;
            toast.error(msg ?? tr('trendPage.view_delete_failed'));
        }
    }, [tr, refetchViews]);

    // ── Measurement: values at A and B, and the differences ──
    const measurement = useMemo(() => {
        if (!measure || marks.a === null || !series) return null;
        return pens.map(p => {
            const a = valueAt(series[p.tagId], marks.a as number);
            const b = marks.b === null ? null : valueAt(series[p.tagId], marks.b);
            return { pen: p, a, b, delta: a && b ? b.value - a.value : null };
        });
    }, [measure, marks, series, pens]);

    // ◀ ▶: the window moves by its own length.
    const pan = useCallback((dir: -1 | 1) => {
        const span = timeRange.to.getTime() - timeRange.from.getTime();
        const now = Date.now();
        const to = Math.min(now, timeRange.to.getTime() + dir * span);
        setTimeRange({ from: new Date(to - span), to: new Date(to), preset: 'custom' });
        setLiveMode(false);
        setZoomStack([]);
    }, [timeRange]);

    // Add tag as pen
    const addPen = useCallback((tagId: number) => {
        if (pens.some(p => p.tagId === tagId)) return;
        const tag = tagMap.get(tagId);
        if (!tag) return;
        const colorIdx = pens.length % PEN_COLORS.length;
        setPens(prev => [...prev, {
            tagId,
            label: tag.alias || tag.code,
            unit: tag.eu_unit?.trim() || undefined,
            color: PEN_COLORS[colorIdx],
            visible: true,
        }]);
        // On a phone the tag list covers the chart: close it once a tag is picked.
        if (!isWide) setSidebarOpen(false);
        // Live: the new pen needs the window's history up to now, not up to
        // when live started.
        if (liveMode) {
            const to = new Date();
            setLiveBase({ from: new Date(to.getTime() - spanMs), to });
        }
    }, [pens, tagMap, isWide, liveMode, spanMs]);

    const togglePenVisibility = useCallback((tagId: number) => {
        setPens(prev => prev.map(p => p.tagId === tagId ? { ...p, visible: !p.visible } : p));
    }, []);

    const removePen = useCallback((tagId: number) => {
        setPens(prev => prev.filter(p => p.tagId !== tagId));
    }, []);

    const changePenColor = useCallback((tagId: number, color: string) => {
        setPens(prev => prev.map(p => p.tagId === tagId ? { ...p, color } : p));
    }, []);

    // Zoom by drag-select on chart — push current range to stack first
    const handleZoom = useCallback((fromMs: number, toMs: number) => {
        setZoomStack(stack => [...stack, timeRange]);
        setTimeRange({ from: new Date(fromMs), to: new Date(toMs), preset: 'custom' });
        setLiveMode(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [timeRange]);

    const handleZoomOut = useCallback(() => {
        setZoomStack(stack => {
            const prev = stack[stack.length - 1];
            if (!prev) return stack;
            setTimeRange(prev);
            return stack.slice(0, -1);
        });
    }, []);

    const totalPoints = timestamps.length * pens.length;

    return (
        <div className="flex flex-col h-full overflow-hidden bg-background">
            {/* ── TOOLBAR ────────────────────────────────────────────── */}
            <div className="shrink-0 bg-card border-b px-3 py-2 space-y-2">
                {/* Row 1: title + actions */}
                <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between gap-2">
                    <div className="flex items-center gap-2">
                        <Button variant="ghost" size="icon" className="h-9 sm:h-7 w-9 sm:w-7"
                            onClick={() => { setSidebarOpen(o => !o); if (!isWide) setPenPanelOpen(false); }}
                            title={tr('trendPage.toggle_tags')} aria-label={tr('trendPage.toggle_tags')}>
                            <PanelLeft className={`w-4 h-4 ${sidebarOpen ? 'text-primary' : 'text-muted-foreground'}`} />
                        </Button>
                        <div className="p-1 bg-primary/10 rounded">
                            <Activity className="w-4 h-4 text-primary" />
                        </div>
                        <div>
                            <h1 className="text-sm font-semibold">{tr('trendPage.title')}</h1>
                            <p className="text-[10px] text-muted-foreground leading-none">
                                {tr('trendPage.pens_count', { count: pens.length })} · {tr('trendPage.points_count', { count: totalPoints, formatted: totalPoints.toLocaleString(i18n.language) })}
                                {isFetching && <span className="ml-1 text-primary">{tr('trendPage.loading')}</span>}
                                {liveMode && (rtConnected ? (
                                    <span className="ml-2 inline-flex items-center gap-1 text-green-600 dark:text-green-500">
                                        <span className="w-1.5 h-1.5 rounded-full animate-pulse bg-green-500 inline-block" />
                                        {tr('trendPage.live_badge')}
                                        {lastLiveTs !== null && ` · ${tr('trendPage.live_last', { time: new Date(lastLiveTs).toLocaleTimeString(i18n.language) })}`}
                                    </span>
                                ) : (
                                    <span className="ml-2 inline-flex items-center gap-1 text-amber-600 dark:text-amber-500" title={tr('trendPage.live_no_socket_hint')}>
                                        <span className="w-1.5 h-1.5 rounded-full bg-amber-500 inline-block" />
                                        {tr('trendPage.live_badge')} · {tr('trendPage.live_no_socket')}
                                    </span>
                                ))}
                            </p>
                        </div>
                    </div>

                    <div className="flex flex-wrap items-center gap-1.5">
                        {/* Aggregation */}
                        <Select value={aggregation} onValueChange={v => setAggregation(v as AggregationType)}>
                            <SelectTrigger className="h-9 sm:h-7 w-24 text-xs">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {AGG_OPTIONS.map(o => (
                                    <SelectItem key={o.value} value={o.value}>{tr(o.labelKey)}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>

                        {/* Live */}
                        <Button
                            size="sm"
                            variant={liveMode ? 'default' : 'outline'}
                            className="h-9 sm:h-7 text-xs gap-1"
                            onClick={toggleLive}
                            disabled={timeRange.preset === 'custom'}
                        >
                            {liveMode ? <><Pause className="w-3 h-3" /> {tr('trendPage.stop')}</> : <><Play className="w-3 h-3" /> {tr('trendPage.live')}</>}
                        </Button>

                        {/* Reset zoom */}
                        {zoomStack.length > 0 && (
                            <Button size="sm" variant="outline" className="h-9 sm:h-7 text-xs gap-1"
                                onClick={handleZoomOut} title={tr('trendPage.zoom_out')}>
                                <ZoomOut className="w-3 h-3" />
                                {zoomStack.length > 1 ? tr('trendPage.zoom_undo', { n: zoomStack.length }) : tr('trendPage.zoom_reset')}
                            </Button>
                        )}

                        {/* Pan */}
                        <div className="flex">
                            <Button size="icon" variant="outline" className="h-9 sm:h-7 w-9 sm:w-7 rounded-r-none"
                                onClick={() => pan(-1)} title={tr('trendPage.pan_back')} aria-label={tr('trendPage.pan_back')}>
                                <ChevronLeft className="w-3.5 h-3.5" />
                            </Button>
                            <Button size="icon" variant="outline" className="h-9 sm:h-7 w-9 sm:w-7 rounded-l-none border-l-0"
                                onClick={() => pan(1)} disabled={liveMode || timeRange.preset !== 'custom'}
                                title={tr('trendPage.pan_fwd')} aria-label={tr('trendPage.pan_fwd')}>
                                <ChevronRight className="w-3.5 h-3.5" />
                            </Button>
                        </div>

                        {/* Measure */}
                        <Button size="sm" variant={measure ? 'default' : 'outline'} className="h-9 sm:h-7 text-xs gap-1"
                            onClick={() => { setMeasure(m => !m); setMarks({ a: null, b: null }); }}
                            title={tr('trendPage.measure_hint')}>
                            <Ruler className="w-3 h-3" /> {tr('trendPage.measure')}
                        </Button>

                        {/* Compare */}
                        <Select value={compare} onValueChange={v => setCompare(v as CompareMode)}>
                            <SelectTrigger className="h-9 sm:h-7 w-auto gap-1 text-xs" aria-label={tr('trendPage.compare')}>
                                <GitCompareArrows className="w-3 h-3" />
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {(['off', 'prev', '1d', '7d'] as const).map(m => (
                                    <SelectItem key={m} value={m}>{tr(`trendPage.compare_${m}`)}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>

                        {/* Saved views */}
                        <Popover>
                            <PopoverTrigger asChild>
                                <Button size="sm" variant="outline" className="h-9 sm:h-7 text-xs gap-1">
                                    <Bookmark className="w-3 h-3" /> {tr('trendPage.views')}
                                </Button>
                            </PopoverTrigger>
                            <PopoverContent align="end" className="w-80 space-y-3">
                                <p className="text-sm font-semibold">{tr('trendPage.views_title')}</p>
                                <div className="max-h-60 overflow-y-auto divide-y divide-border">
                                    {(savedViews ?? []).length === 0 && (
                                        <p className="text-xs text-muted-foreground py-2">{tr('trendPage.views_empty')}</p>
                                    )}
                                    {(savedViews ?? []).map(v => {
                                        const parsed = decodeView(new URLSearchParams(v.query));
                                        const mine = authUser?.role === 'admin' || v.created_by === authUser?.id;
                                        return (
                                            <div key={v.id} className="flex items-center gap-2 py-1.5">
                                                <button className="flex-1 min-w-0 text-left" disabled={!parsed}
                                                    onClick={() => parsed && applyView(parsed)}>
                                                    <div className="text-xs font-medium truncate">{v.name}</div>
                                                    {v.author && <div className="text-[10px] text-muted-foreground truncate">{v.author}</div>}
                                                </button>
                                                {mine && (
                                                    <Button size="icon" variant="ghost" className="h-7 w-7 text-destructive/70 hover:text-destructive"
                                                        onClick={() => deleteView(v.id)} aria-label={tr('trendPage.view_delete')} title={tr('trendPage.view_delete')}>
                                                        <Trash2 className="w-3.5 h-3.5" />
                                                    </Button>
                                                )}
                                            </div>
                                        );
                                    })}
                                </div>
                                <div className="flex gap-2 pt-1 border-t">
                                    <Input className="h-8 text-xs" placeholder={tr('trendPage.view_name')} value={viewName} maxLength={100}
                                        onChange={e => setViewName(e.target.value)} onKeyDown={e => e.key === 'Enter' && saveView()} />
                                    <Button size="sm" className="h-8 text-xs" onClick={saveView} disabled={!viewName.trim() || pens.length === 0}>
                                        {tr('trendPage.view_save')}
                                    </Button>
                                </div>
                            </PopoverContent>
                        </Popover>

                        {/* Y axes */}
                        {axisGroups.length > 0 && (
                            <Popover>
                                <PopoverTrigger asChild>
                                    <Button size="sm" variant={Object.keys(yRanges).length ? 'default' : 'outline'} className="h-9 sm:h-7 text-xs gap-1">
                                        <MoveVertical className="w-3 h-3" /> {tr('trendPage.axes')}
                                    </Button>
                                </PopoverTrigger>
                                <PopoverContent align="end" className="w-80 space-y-3">
                                    <p className="text-sm font-semibold">{tr('trendPage.axes_title')}</p>
                                    {axisGroups.map(g => (
                                        <AxisRow key={g.key} label={g.label} eu={g.eu} mode={yModes[g.key]}
                                            onChange={m => setYMode(g.key, m)} />
                                    ))}
                                </PopoverContent>
                            </Popover>
                        )}

                        {/* Refresh */}
                        <Button size="icon" variant="outline" className="h-9 sm:h-7 w-9 sm:w-7"
                            onClick={() => refetch()} disabled={isFetching || penIds.length === 0}
                            title={tr('trendPage.refresh')} aria-label={tr('trendPage.refresh')}>
                            <RefreshCw className={`w-3 h-3 ${isFetching ? 'animate-spin' : ''}`} />
                        </Button>

                        {/* Export CSV */}
                        <Button size="sm" variant="outline" className="h-9 sm:h-7 text-xs gap-1"
                            onClick={() => {
                                if (timestamps.length === 0) { toast.info(tr('trendPage.no_data_export')); return; }
                                exportCSV(pens, timestamps, values);
                                toast.success(tr('trendPage.csv_exported'));
                            }}
                            disabled={timestamps.length === 0}>
                            <Download className="w-3 h-3" />
                            CSV
                        </Button>

                        {/* Pen panel toggle */}
                        <Button variant="ghost" size="icon" className="h-9 sm:h-7 w-9 sm:w-7"
                            onClick={() => { setPenPanelOpen(o => !o); if (!isWide) setSidebarOpen(false); }}
                            title={tr('trendPage.toggle_pens')} aria-label={tr('trendPage.toggle_pens')}>
                            <PanelRight className={`w-4 h-4 ${penPanelOpen ? 'text-primary' : 'text-muted-foreground'}`} />
                        </Button>
                    </div>
                </div>

                {/* Row 2: date range picker */}
                <HistorianDatePicker value={timeRange} onChange={(r) => { setTimeRange(r); setLiveMode(false); setZoomStack([]); }} />
            </div>

            {/* ── CONTENT ────────────────────────────────────────────── */}
            {/* On a phone the page scrolls and has no height to fill: the
                chart gets most of the screen instead (dvh follows the browser bar). */}
            <div className={`relative flex overflow-hidden ${isWide ? 'h-[calc(100dvh-11rem)] min-h-[420px] shrink-0' : 'h-[75dvh] min-h-[380px] shrink-0'}`}>

                {/* Left: tag browser. On a narrow screen it floats over the chart. */}
                {sidebarOpen && (
                    <div className={isWide
                        ? 'w-64 shrink-0 border-r overflow-y-auto bg-card/50'
                        : 'absolute inset-y-0 left-0 z-20 w-[85%] max-w-xs border-r overflow-y-auto bg-card shadow-xl'}>
                        <TagBrowser
                            onAddTagToChart={addPen}
                            selectedTagIds={penIds}
                        />
                    </div>
                )}

                {/* Center: chart */}
                <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
                    {pens.length === 0 ? (
                        <div className="flex-1 flex flex-col items-center justify-center gap-3 text-muted-foreground">
                            <BarChart2 className="w-16 h-16 opacity-20" />
                            <div className="text-center">
                                <p className="font-medium">{tr('trendPage.empty_title')}</p>
                                <p className="text-sm mt-1">{tr('trendPage.empty_desc')}</p>
                            </div>
                        </div>
                    ) : (
                        <div className="flex-1 min-h-0 p-2 overflow-hidden">
                            <UPlotChart
                                timestamps={timestamps}
                                values={values}
                                pens={penSeries}
                                range={{ from: timeRange.from.getTime(), to: timeRange.to.getTime() }}
                                bands={bands}
                                events={chartEvents}
                                yRanges={yRanges}
                                live={liveMode}
                                onZoom={handleZoom}
                                measure={measure}
                                marks={marks}
                                onMeasureClick={onMeasureClick}
                            />
                        </div>
                    )}
                    {measure && pens.length > 0 && (
                        <div className="shrink-0 border-t bg-card/60 px-3 py-2 text-xs">
                            {marks.a === null ? (
                                <p className="text-muted-foreground">{tr('trendPage.measure_hint')}</p>
                            ) : (
                                <div className="overflow-x-auto">
                                    <div className="flex items-center gap-3 mb-1 font-mono text-[11px]">
                                        <span>A {new Date(marks.a).toLocaleString(i18n.language)}</span>
                                        {marks.b !== null && <span>B {new Date(marks.b).toLocaleString(i18n.language)}</span>}
                                        {marks.b !== null && <span className="font-semibold">{tr('trendPage.delta_t')} {fmtDuration(Math.abs(marks.b - marks.a))}</span>}
                                        <Button size="sm" variant="ghost" className="h-6 ml-auto text-xs" onClick={() => setMarks({ a: null, b: null })}>
                                            {tr('trendPage.measure_clear')}
                                        </Button>
                                    </div>
                                    <table className="w-full font-mono text-[11px]">
                                        <tbody>
                                            {measurement?.map(({ pen, a, b, delta }) => (
                                                <tr key={pen.tagId}>
                                                    <td className="pr-3 truncate max-w-[180px]" style={{ color: pen.color }}>{pen.label}</td>
                                                    <td className="pr-3">A {a ? a.value.toFixed(3).replace(/\.?0+$/, '') : '—'}</td>
                                                    {marks.b !== null && <td className="pr-3">B {b ? b.value.toFixed(3).replace(/\.?0+$/, '') : '—'}</td>}
                                                    {marks.b !== null && <td className="font-semibold">Δ {delta === null ? '—' : `${delta >= 0 ? '+' : ''}${delta.toFixed(3).replace(/\.?0+$/, '')}`}{pen.unit ? ` ${pen.unit}` : ''}</td>}
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table>
                                </div>
                            )}
                        </div>
                    )}
                </div>

                {/* Right: pen panel + stats */}
                {penPanelOpen && (
                    <div className={isWide
                        ? 'w-56 shrink-0 border-l flex flex-col bg-card/50'
                        : 'absolute inset-y-0 right-0 z-20 w-[85%] max-w-xs border-l flex flex-col bg-card shadow-xl'}>
                        <div className="px-3 py-2 border-b flex items-center gap-1.5">
                            <Settings2 className="w-3.5 h-3.5 text-muted-foreground" />
                            <span className="text-xs font-medium">{tr('trendPage.pens')}</span>
                            {pens.length > 0 && (
                                <Badge variant="secondary" className="ml-auto text-[10px] h-4 px-1">
                                    {pens.length}
                                </Badge>
                            )}
                        </div>
                        <div className="flex-1 overflow-y-auto">
                            <PenPanel
                                pens={pensWithStats}
                                onToggleVisibility={togglePenVisibility}
                                onRemove={removePen}
                                onColorChange={changePenColor}
                            />
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}

// ── Utility ───────────────────────────────────────────────────────────────────

function presetMs(preset: string): number | undefined {
    const map: Record<string, number> = {
        '1h':  1  * 60 * 60_000,
        '4h':  4  * 60 * 60_000,
        '8h':  8  * 60 * 60_000,
        '12h': 12 * 60 * 60_000,
        '24h': 24 * 60 * 60_000,
        '3d':  3  * 24 * 60 * 60_000,
        '7d':  7  * 24 * 60 * 60_000,
        '30d': 30 * 24 * 60 * 60_000,
    };
    return map[preset];
}
