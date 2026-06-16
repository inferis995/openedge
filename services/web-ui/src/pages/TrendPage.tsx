import { useState, useMemo, useCallback, useEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
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

import { TagBrowser } from '@/components/trend/TagBrowser';
import { UPlotChart, type PenSeries } from '@/components/historian/UPlotChart';
import { HistorianDatePicker, type HistorianRange } from '@/components/historian/HistorianDatePicker';
import { PenPanel, type Pen, type PenStats } from '@/components/historian/PenPanel';

import { historyApi } from '@/api/history';
import { tagsApi } from '@/api/tags';
import { getAutoInterval, type TrendDataPoint, type AggregationType, type TagWithHierarchy } from '@/types/trend';

// ── Constants ────────────────────────────────────────────────────────────────

const PEN_COLORS = [
    '#3b82f6', '#ef4444', '#22c55e', '#f59e0b', '#8b5cf6',
    '#06b6d4', '#ec4899', '#f97316', '#6366f1', '#14b8a6',
    '#a855f7', '#eab308', '#84cc16', '#f43f5e', '#22d3ee',
];

const AGG_OPTIONS: { value: AggregationType; label: string }[] = [
    { value: 'mean',  label: 'Mean'  },
    { value: 'max',   label: 'Max'   },
    { value: 'min',   label: 'Min'   },
    { value: 'first', label: 'First' },
    { value: 'last',  label: 'Last'  },
];

// ── Helpers ───────────────────────────────────────────────────────────────────

function defaultRange(): HistorianRange {
    const to = new Date();
    const from = new Date(to.getTime() - 60 * 60_000); // last 1h
    return { from, to, preset: '1h' };
}

function computeStats(pts: TrendDataPoint[]): PenStats {
    const vals = pts
        .filter(p => p.quality === 0 && p.value !== null)
        .map(p => p.value as number);
    if (vals.length === 0) return { min: null, max: null, avg: null, last: null, count: 0 };
    const min = Math.min(...vals);
    const max = Math.max(...vals);
    const avg = vals.reduce((s, v) => s + v, 0) / vals.length;
    const last = vals[vals.length - 1] ?? null;
    return { min, max, avg, last, count: vals.length };
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
    const [timeRange, setTimeRange] = useState<HistorianRange>(defaultRange);
    const [liveMode, setLiveMode] = useState(false);
    const [aggregation, setAggregation] = useState<AggregationType>('mean');
    const [pens, setPens] = useState<Pen[]>([]);
    const [tagMap, setTagMap] = useState<Map<number, TagWithHierarchy>>(new Map());
    const [sidebarOpen, setSidebarOpen] = useState(true);
    const [penPanelOpen, setPenPanelOpen] = useState(true);
    const [zoomStack, setZoomStack] = useState<HistorianRange[]>([]);
    const liveRef = useRef<ReturnType<typeof setInterval> | null>(null);

    // Load all tags for label lookup
    const { data: allTags } = useQuery({
        queryKey: ['tags-with-hierarchy'],
        queryFn: () => tagsApi.getAllWithHierarchy(),
        staleTime: 60_000,
    });

    useEffect(() => {
        if (!allTags) return;
        const m = new Map<number, TagWithHierarchy>();
        for (const t of allTags) m.set(t.id, t);
        setTagMap(m);
    }, [allTags]);

    // Live mode: shift the time window every 10 s
    useEffect(() => {
        if (liveRef.current) clearInterval(liveRef.current);
        if (!liveMode || timeRange.preset === 'custom') return;
        liveRef.current = setInterval(() => {
            const ms = presetMs(timeRange.preset ?? '1h');
            const to = new Date();
            const from = new Date(to.getTime() - ms);
            setTimeRange(r => ({ ...r, from, to }));
        }, 10_000);
        return () => { if (liveRef.current) clearInterval(liveRef.current); };
    }, [liveMode, timeRange.preset]);

    const penIds = useMemo(() => pens.map(p => p.tagId), [pens]);

    // Fetch historical data for all selected pens
    const { data: batchData, isFetching, refetch } = useQuery({
        queryKey: ['historian-batch', penIds, timeRange.from.toISOString(), timeRange.to.toISOString(), aggregation],
        queryFn: () => historyApi.batchQuery({
            tag_ids: penIds,
            start: timeRange.from.toISOString(),
            end: timeRange.to.toISOString(),
            agg: aggregation,
            interval: getAutoInterval(timeRange.from, timeRange.to),
        }),
        enabled: penIds.length > 0,
        refetchInterval: liveMode ? 10_000 : false,
        staleTime: 5_000,
    });

    // Build uPlot-aligned data (seconds timestamps)
    const { timestamps, values } = useMemo(() => {
        if (!batchData || pens.length === 0) return { timestamps: [], values: [] };
        const tsSet = new Set<number>();
        const maps = pens.map(pen => {
            const pts: TrendDataPoint[] = (batchData as Record<number, TrendDataPoint[]>)[pen.tagId] ?? [];
            const m = new Map<number, number | null>();
            for (const p of pts) {
                const tsSec = Math.round(p.timestamp / 1000);
                tsSet.add(tsSec);
                m.set(tsSec, p.quality === 0 ? p.value : null);
            }
            return m;
        });
        const sortedTs = Array.from(tsSet).sort((a, b) => a - b);
        const vals = maps.map(m => sortedTs.map(t => m.get(t) ?? null));
        return { timestamps: sortedTs, values: vals };
    }, [batchData, pens]);

    // Compute per-pen stats
    const pensWithStats = useMemo<Pen[]>(() => {
        if (!batchData) return pens;
        return pens.map(pen => {
            const pts: TrendDataPoint[] = (batchData as Record<number, TrendDataPoint[]>)[pen.tagId] ?? [];
            return { ...pen, stats: computeStats(pts) };
        });
    }, [batchData, pens]);

    // Build PenSeries for uPlot
    const penSeries = useMemo<PenSeries[]>(() =>
        pens.map(p => ({
            tagId: p.tagId,
            label: p.label,
            unit: p.unit,
            color: p.color,
            visible: p.visible,
        })), [pens]);

    // Add tag as pen
    const addPen = useCallback((tagId: number) => {
        if (pens.some(p => p.tagId === tagId)) return;
        const tag = tagMap.get(tagId);
        if (!tag) return;
        const colorIdx = pens.length % PEN_COLORS.length;
        setPens(prev => [...prev, {
            tagId,
            label: tag.alias || tag.code,
            unit: tag.eu_unit || undefined,
            color: PEN_COLORS[colorIdx],
            visible: true,
        }]);
    }, [pens, tagMap]);

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
            <div className="flex-shrink-0 bg-card border-b px-3 py-2 space-y-2">
                {/* Row 1: title + actions */}
                <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                        <Button variant="ghost" size="icon" className="h-7 w-7"
                            onClick={() => setSidebarOpen(o => !o)}>
                            <PanelLeft className={`w-4 h-4 ${sidebarOpen ? 'text-primary' : 'text-muted-foreground'}`} />
                        </Button>
                        <div className="p-1 bg-primary/10 rounded">
                            <Activity className="w-4 h-4 text-primary" />
                        </div>
                        <div>
                            <h1 className="text-sm font-semibold">Historian</h1>
                            <p className="text-[10px] text-muted-foreground leading-none">
                                {pens.length} pen{pens.length !== 1 ? 's' : ''} · {totalPoints.toLocaleString()} pts
                                {isFetching && <span className="ml-1 text-primary">loading…</span>}
                                {liveMode && (
                                    <span className="ml-2 inline-flex items-center gap-1 text-green-500">
                                        <span className="w-1.5 h-1.5 rounded-full animate-pulse bg-green-500 inline-block" />
                                        LIVE
                                    </span>
                                )}
                            </p>
                        </div>
                    </div>

                    <div className="flex items-center gap-1.5">
                        {/* Aggregation */}
                        <Select value={aggregation} onValueChange={v => setAggregation(v as AggregationType)}>
                            <SelectTrigger className="h-7 w-24 text-xs">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {AGG_OPTIONS.map(o => (
                                    <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>

                        {/* Live */}
                        <Button
                            size="sm"
                            variant={liveMode ? 'default' : 'outline'}
                            className="h-7 text-xs gap-1"
                            onClick={() => setLiveMode(m => !m)}
                            disabled={timeRange.preset === 'custom'}
                        >
                            {liveMode ? <><Pause className="w-3 h-3" /> Stop</> : <><Play className="w-3 h-3" /> Live</>}
                        </Button>

                        {/* Reset zoom */}
                        {zoomStack.length > 0 && (
                            <Button size="sm" variant="outline" className="h-7 text-xs gap-1"
                                onClick={handleZoomOut} title="Zoom out">
                                <ZoomOut className="w-3 h-3" />
                                {zoomStack.length > 1 ? `Undo (${zoomStack.length})` : 'Reset zoom'}
                            </Button>
                        )}

                        {/* Refresh */}
                        <Button size="icon" variant="outline" className="h-7 w-7"
                            onClick={() => refetch()} disabled={isFetching || penIds.length === 0}>
                            <RefreshCw className={`w-3 h-3 ${isFetching ? 'animate-spin' : ''}`} />
                        </Button>

                        {/* Export CSV */}
                        <Button size="sm" variant="outline" className="h-7 text-xs gap-1"
                            onClick={() => {
                                if (timestamps.length === 0) { toast.info('No data to export'); return; }
                                exportCSV(pens, timestamps, values);
                                toast.success('CSV exported');
                            }}
                            disabled={timestamps.length === 0}>
                            <Download className="w-3 h-3" />
                            CSV
                        </Button>

                        {/* Pen panel toggle */}
                        <Button variant="ghost" size="icon" className="h-7 w-7"
                            onClick={() => setPenPanelOpen(o => !o)}>
                            <PanelRight className={`w-4 h-4 ${penPanelOpen ? 'text-primary' : 'text-muted-foreground'}`} />
                        </Button>
                    </div>
                </div>

                {/* Row 2: date range picker */}
                <HistorianDatePicker value={timeRange} onChange={(r) => { setTimeRange(r); setLiveMode(false); setZoomStack([]); }} />
            </div>

            {/* ── CONTENT ────────────────────────────────────────────── */}
            <div className="flex flex-1 overflow-hidden">

                {/* Left: tag browser */}
                {sidebarOpen && (
                    <div className="w-64 flex-shrink-0 border-r overflow-y-auto bg-card/50">
                        <TagBrowser
                            onAddTagToChart={addPen}
                            selectedTagIds={penIds}
                        />
                    </div>
                )}

                {/* Center: chart */}
                <div className="flex-1 flex flex-col overflow-hidden">
                    {pens.length === 0 ? (
                        <div className="flex-1 flex flex-col items-center justify-center gap-3 text-muted-foreground">
                            <BarChart2 className="w-16 h-16 opacity-20" />
                            <div className="text-center">
                                <p className="font-medium">No pens selected</p>
                                <p className="text-sm mt-1">Select tags from the browser on the left to start</p>
                            </div>
                        </div>
                    ) : (
                        <div className="flex-1 p-2 overflow-hidden">
                            <UPlotChart
                                timestamps={timestamps}
                                values={values}
                                pens={penSeries}
                                height={Math.max(280, window.innerHeight - 220)}
                                onZoom={handleZoom}
                            />
                        </div>
                    )}
                </div>

                {/* Right: pen panel + stats */}
                {penPanelOpen && (
                    <div className="w-56 flex-shrink-0 border-l flex flex-col bg-card/50">
                        <div className="px-3 py-2 border-b flex items-center gap-1.5">
                            <Settings2 className="w-3.5 h-3.5 text-muted-foreground" />
                            <span className="text-xs font-medium">Pens</span>
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

function presetMs(preset: string): number {
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
    return map[preset] ?? 60 * 60_000;
}
