import { useState, useMemo, useCallback, useEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useSearchParams } from 'react-router-dom';
import {
    Activity, Play, Pause, RefreshCw, Download, PanelLeft, PanelRight,
    BarChart2, Settings2, ZoomOut,
} from 'lucide-react';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { toast } from 'sonner';
import { useTranslation } from 'react-i18next';

import { TagBrowser } from '@/components/trend/TagBrowser';
import { UPlotChart, type PenSeries } from '@/components/historian/UPlotChart';
import { HistorianDatePicker, type HistorianRange } from '@/components/historian/HistorianDatePicker';
import { PenPanel, type Pen, type PenStats } from '@/components/historian/PenPanel';

import { historyApi } from '@/api/history';
import { tagsApi } from '@/api/tags';
import { alignPens, mergeSeries, realtimePoint } from '@/lib/trendAlign';
import { decodeView, encodeView, resolveRange, TREND_VIEW_KEY, type TrendView } from '@/lib/trendView';
import { useRealtime } from '@/hooks/useRealtime';
import { useAuthStore } from '@/stores/useAuthStore';
import { getAutoInterval, type TrendDataPoint, type AggregationType, type TagWithHierarchy } from '@/types/trend';

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
    { value: 'first', labelKey: 'trendPage.agg_first' },
    { value: 'last',  labelKey: 'trendPage.agg_last'  },
];

// ── Helpers ───────────────────────────────────────────────────────────────────

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
        if (v < min) min = v;
        if (v > max) max = v;
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
    }).toString(), [rawPens, timeRange, aggregation, liveMode]);
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
            interval: getAutoInterval(queryRange.from, queryRange.to),
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
                agg: aggregation, interval: getAutoInterval(from, to),
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
    const { timestamps, values } = useMemo<{ timestamps: number[]; values: (number | null | undefined)[][] }>(() => {
        if (!series || pens.length === 0) return { timestamps: [], values: [] };
        return alignPens(pens, series);
    }, [series, pens]);

    // Compute per-pen stats
    const pensWithStats = useMemo<Pen[]>(() => {
        if (!series) return pens;
        return pens.map(pen => ({ ...pen, stats: computeStats(series[pen.tagId] ?? []) }));
    }, [series, pens]);

    // Build PenSeries for uPlot
    const penSeries = useMemo<PenSeries[]>(() =>
        pens.map(p => ({
            tagId: p.tagId,
            label: p.label,
            unit: p.unit,
            decimals: tagMap.get(p.tagId)?.eu_decimals ?? undefined,
            step: tagMap.get(p.tagId)?.data_type === 'BOOL',
            color: p.color,
            visible: p.visible,
        })), [pens, tagMap]);

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
            <div className={`relative flex overflow-hidden ${isWide ? 'flex-1 min-h-0' : 'h-[75dvh] min-h-[380px] shrink-0'}`}>

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
                                live={liveMode}
                                onZoom={handleZoom}
                            />
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
