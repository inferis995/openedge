import React, { useMemo, useCallback, useEffect, useState } from 'react';
import GridLayout from 'react-grid-layout';
import { useTrendStore } from '@/stores/useTrendStore';
import { TrendChart } from './TrendChart';
import { TagStatsPanel } from './TagStatsPanel';
import { ChartSeries, TagWithHierarchy, TrendDataPoint, TAG_COLORS, getAutoInterval } from '@/types/trend';
import { historyApi, TagStats } from '@/api/history';
import { useQueries } from '@tanstack/react-query';
import { Loader2, Plus, BarChart3, ChevronDown } from 'lucide-react';
import { Button } from '@/components/ui/button';
import 'react-grid-layout/css/styles.css';
import 'react-resizable/css/styles.css';

type LayoutItem = {
    i: string;
    x: number;
    y: number;
    w: number;
    h: number;
    minW?: number;
    minH?: number;
};

// Cast GridLayout to any to work around type issues
const Grid = GridLayout as any;

interface TrendDashboardProps {
    tags: TagWithHierarchy[];
    timeRange: { start: Date; end: Date };
    aggregation: string;
    realtimeValues?: Map<number, { value: number; timestamp: number; quality: number }>;
    refreshTrigger?: number;
}

const COLS = 12;
const ROW_HEIGHT = 80;

export const TrendDashboard: React.FC<TrendDashboardProps> = ({
    tags,
    timeRange,
    aggregation,
    realtimeValues,
    refreshTrigger = 0,
}) => {
    const {
        charts,
        activeChartId,
        setActiveChart,
        removeChart,
        updateChart,
        addChart,
        liveMode,
        removeTagFromChart,
        updateYAxisConfig,
    } = useTrendStore();

    const [containerWidth, setContainerWidth] = useState(1200);
    const containerRef = React.useRef<HTMLDivElement>(null);

    // Measure container width
    useEffect(() => {
        const updateWidth = () => {
            if (containerRef.current) {
                setContainerWidth(containerRef.current.offsetWidth);
            }
        };

        updateWidth();
        window.addEventListener('resize', updateWidth);
        return () => window.removeEventListener('resize', updateWidth);
    }, []);

    // Ensure timeRange values are Date objects (defensive programming)
    const safeTimeRange = useMemo(() => {
        const start = timeRange.start instanceof Date ? timeRange.start : new Date(timeRange.start);
        const end = timeRange.end instanceof Date ? timeRange.end : new Date(timeRange.end);
        return { start, end };
    }, [timeRange.start, timeRange.end]);

    // Live clock for smooth X-axis scrolling in Live Mode
    const [now, setNow] = useState<Date>(new Date());
    useEffect(() => {
        if (!liveMode) return;
        setNow(new Date()); // sync immediately
        const intervalId = window.setInterval(() => setNow(new Date()), 1000);
        return () => window.clearInterval(intervalId);
    }, [liveMode]);

    // Calculate the actual display range for the charts
    const displayTimeRange = useMemo(() => {
        if (!liveMode) return safeTimeRange;
        // In live mode, we want the visible window to end AT THIS EXACT SECOND.
        // The original safeTimeRange tells us the width of the window (end - start).
        const windowWidth = safeTimeRange.end.getTime() - safeTimeRange.start.getTime();
        return {
            start: new Date(now.getTime() - windowWidth),
            end: now,
        };
    }, [safeTimeRange, now, liveMode]);

    // Calculate interval based on time range
    const interval = useMemo(() => {
        return getAutoInterval(safeTimeRange.start, safeTimeRange.end);
    }, [safeTimeRange]);

    // Fetch historical data for all tags in all charts
    const allTagIds = useMemo(() => {
        const ids = new Set<number>();
        charts.forEach(chart => {
            chart.tagIds.forEach(id => ids.add(id));
        });
        return Array.from(ids);
    }, [charts]);

    // Query historical data
    const historyQueries = useQueries({
        queries: allTagIds.map(tagId => ({
            queryKey: ['history', tagId, safeTimeRange.start.toISOString(), safeTimeRange.end.toISOString(), aggregation, interval, refreshTrigger],
            queryFn: async () => {
                const response = await historyApi.query({
                    tag_id: tagId,
                    start: safeTimeRange.start.toISOString(),
                    end: safeTimeRange.end.toISOString(),
                    agg: aggregation as any,
                    interval,
                });
                return { tagId, data: response };
            },
            staleTime: 5000,
            enabled: allTagIds.includes(tagId),
        })),
    });

    // Build data map
    const historyDataMap = useMemo(() => {
        const map = new Map<number, TrendDataPoint[]>();
        historyQueries.forEach(query => {
            if (query.data) {
                map.set(query.data.tagId, query.data.data);
            }
        });
        return map;
    }, [historyQueries]);

    // Check if any query is loading
    const isLoading = historyQueries.some(q => q.isLoading);

    // Fetch statistics for all tags
    const [tagStats, setTagStats] = useState<Map<number, TagStats>>(new Map());
    const [statsLoading, setStatsLoading] = useState(false);

    useEffect(() => {
        if (allTagIds.length === 0) {
            setTagStats(new Map());
            return;
        }

        setStatsLoading(true);
        Promise.all(
            allTagIds.map(tagId =>
                historyApi.getStats(tagId, safeTimeRange.start.toISOString(), safeTimeRange.end.toISOString())
                    .then(stats => ({ tagId, stats }))
                    .catch(() => ({ tagId, stats: null as TagStats | null }))
            )
        ).then(results => {
            const newStats = new Map<number, TagStats>();
            results.forEach(r => {
                if (r.stats) {
                    newStats.set(r.tagId, r.stats);
                }
            });
            setTagStats(newStats);
            setStatsLoading(false);
        });
    }, [allTagIds, safeTimeRange.start, safeTimeRange.end]);

    // Build series data for each chart
    const getChartSeries = useCallback((chartId: string): ChartSeries[] => {
        const chart = charts.find(c => c.id === chartId);
        if (!chart) return [];

        return chart.tagIds.map((tagId, index) => {
            const tag = tags.find(t => t.id === tagId);
            const historyData = historyDataMap.get(tagId) || [];
            const yAxisConfig = chart.yAxisConfigs.find(y => y.tagId === tagId);
            const isBool = tag?.data_type === 'BOOL';

            // Helper to convert value to number
            const toNumber = (val: any): number | null => {
                if (val === null || val === undefined) return null;
                if (typeof val === 'boolean') return val ? 1 : 0;
                if (typeof val === 'string') {
                    const lower = val.toLowerCase();
                    if (lower === 'true') return 1;
                    if (lower === 'false') return 0;
                    const num = parseFloat(val);
                    return isNaN(num) ? null : num;
                }
                const num = Number(val);
                return isNaN(num) ? null : num;
            };

            // Convert history data to chart format [timestamp, value]
            const data: [number, number | null][] = historyData.map(point => {
                // Timestamp from API is already in milliseconds - use directly
                const ts = point.timestamp;
                // Quality 0 = GOOD, >0 = BAD (show as null/gap)
                const value = point.quality === 0 ? toNumber(point.value) : null;
                return [ts, value];
            });

            // Append current realtime value to the series so the chart always reflects
            // the live state, even when the realtime update arrived after the chart's
            // query window was calculated. Clamp the timestamp to displayTimeRange.end
            if (realtimeValues) {
                const rv = realtimeValues.get(tagId);
                if (rv && rv.timestamp >= displayTimeRange.start.getTime()) {
                    const ts = Math.min(rv.timestamp, displayTimeRange.end.getTime());
                    const exists = data.some(d => d[0] === ts);
                    if (!exists) {
                        const value = rv.quality === 0 ? toNumber(rv.value) : null;
                        data.push([ts, value]);
                    }
                }
            }

            // Sort by timestamp
            data.sort((a, b) => a[0] - b[0]);

            // ── FILL-TO-END ─────────────────────────────────────────────
            // Extend the last known value to the end of the display time range so
            // the chart line reaches the right edge.
            // Determine online status from the ACTUAL realtime value if available,
            // otherwise from the last data point.
            const rv = realtimeValues?.get(tagId);
            // Tag is online only if we have a realtime value WITH good quality
            const isCurrentlyOnline = rv !== undefined && rv.quality === 0;

            if (data.length > 0) {
                const lastPoint = data[data.length - 1];
                const endMs = displayTimeRange.end.getTime();

                if (lastPoint[0] < endMs - 1000) {
                    if (isCurrentlyOnline && lastPoint[1] !== null) {
                        // Tag is confirmed online with good quality -> draw line to end
                        data.push([endMs, lastPoint[1]]);
                    } else {
                        // Tag is offline (no realtime value, or bad quality, or last value was null)
                        // Push null to end so the X-axis scrolls and gap/N/A is visible
                        data.push([endMs, null]);
                    }
                }
            } else {
                // No data at all — push a single null point at end so chart axis still scrolls
                const endMs = displayTimeRange.end.getTime();
                data.push([endMs, null]);
            }

            // Remove only exact-same-timestamp duplicates, keep everything else
            // For booleans nulls become -0.5 in TrendChart, so we need them to persist
            const dedupedData = data.filter((pt, i, arr) => {
                if (i === 0) return true;
                const prev = arr[i - 1];
                // Drop exact same timestamp duplicates
                if (pt[0] === prev[0]) return false;
                return true;
            });

            return {
                tagId,
                tagName: tag?.alias || tag?.code || `Tag ${tagId}`,
                data: dedupedData,
                color: yAxisConfig?.color || TAG_COLORS[index % TAG_COLORS.length],
                yAxisIndex: index,
                isBool,
                quality: historyData.map(p => p.quality),
                // Style from YAxisConfig
                lineType: yAxisConfig?.lineType ?? 'solid',
                lineWidth: yAxisConfig?.lineWidth ?? 2,
                chartType: yAxisConfig?.chartType ?? 'line',
                showMarkers: yAxisConfig?.showMarkers ?? false,
                visible: yAxisConfig?.visible ?? true,
                yMin: yAxisConfig?.autoScale === false ? yAxisConfig?.min : undefined,
                yMax: yAxisConfig?.autoScale === false ? yAxisConfig?.max : undefined,
                autoScale: yAxisConfig?.autoScale ?? true,
                yPosition: yAxisConfig?.position ?? (index % 2 === 0 ? 'left' : 'right'),
                areaOpacity: yAxisConfig?.areaOpacity ?? 0.15,
            };
        });
    }, [charts, tags, historyDataMap, realtimeValues, displayTimeRange]);

    // Grid layout
    const layout: LayoutItem[] = useMemo(() => {
        return charts.map((chart, index) => ({
            i: chart.id,
            x: (index % 2) * 6,
            y: Math.floor(index / 2) * 5,
            w: 6,
            h: Math.max(chart.height, 4), // Minimum height of 4 grid units
            minW: 4,
            minH: 4,
        }));
    }, [charts]);

    const handleLayoutChange = useCallback((newLayout: LayoutItem[]) => {
        newLayout.forEach(item => {
            const chart = charts.find(c => c.id === item.i);
            if (chart && chart.height !== item.h) {
                updateChart(item.i, { height: item.h });
            }
        });
    }, [charts, updateChart]);

    const handleAddChart = useCallback(() => {
        addChart();
    }, [addChart]);

    // Calculate grid height based on number of charts
    const gridHeight = useMemo(() => {
        if (charts.length === 0) return 400;
        // Calculate max Y position + height
        const maxY = Math.max(...layout.map(item => item.y + item.h));
        return (maxY + 1) * (ROW_HEIGHT + 10); // +10 for margin
    }, [layout, charts.length]);

    // Stats panel toggle
    const [showStats, setShowStats] = useState(true);

    // Get all unique tags with their colors from all charts
    const allTagsWithColors = useMemo(() => {
        const result: { tagId: number; tagName: string; color: string; isBool: boolean }[] = [];
        const seen = new Set<number>();

        charts.forEach(chart => {
            chart.tagIds.forEach((tagId, index) => {
                if (!seen.has(tagId)) {
                    seen.add(tagId);
                    const tag = tags.find(t => t.id === tagId);
                    const yAxisConfig = chart.yAxisConfigs.find(y => y.tagId === tagId);
                    result.push({
                        tagId,
                        tagName: tag?.alias || tag?.code || `Tag ${tagId}`,
                        color: yAxisConfig?.color || TAG_COLORS[index % TAG_COLORS.length],
                        isBool: tag?.data_type === 'BOOL',
                    });
                }
            });
        });

        return result;
    }, [charts, tags]);

    if (charts.length === 0) {
        return (
            <div className="h-full flex flex-col items-center justify-center bg-muted/30 rounded-lg border-2 border-dashed border-border">
                <div className="text-muted-foreground/50 mb-3">
                    <svg className="w-16 h-16" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
                    </svg>
                </div>
                <p className="text-sm text-muted-foreground mb-3">No charts created</p>
                <Button onClick={handleAddChart} size="sm" className="gap-1">
                    <Plus className="w-4 h-4" />
                    Add Chart
                </Button>
            </div>
        );
    }

    return (
        <div ref={containerRef} className="relative" style={{ minHeight: gridHeight, width: '100%' }}>
            {isLoading && (
                <div className="absolute top-2 right-2 z-10 bg-card/90 px-2 py-1 rounded shadow text-xs text-muted-foreground flex items-center gap-1">
                    <Loader2 className="w-3 h-3 animate-spin" />
                    Loading...
                </div>
            )}

            <Grid
                className="layout"
                layout={layout}
                cols={COLS}
                rowHeight={ROW_HEIGHT}
                width={containerWidth - 20}
                onLayoutChange={handleLayoutChange}
                draggableHandle=".chart-header"
                margin={[10, 10]}
            >
                {charts.map(chart => (
                    <div key={chart.id} className="relative">
                        <TrendChart
                            chart={chart}
                            seriesData={getChartSeries(chart.id)}
                            timeRange={safeTimeRange}
                            isActive={activeChartId === chart.id}
                            onActivate={() => setActiveChart(chart.id)}
                            syncGroup="trend-charts"
                            onRemove={() => removeChart(chart.id)}
                            onRemoveTag={(tagId) => removeTagFromChart(chart.id, tagId)}
                            onUpdateYAxisConfig={(tagId, updates) => updateYAxisConfig(chart.id, tagId, updates)}
                            onUpdateTitle={(title) => updateChart(chart.id, { title })}
                        />
                    </div>
                ))}
            </Grid>

            {/* Statistics Panel */}
            {allTagsWithColors.length > 0 && (
                <div className="mt-4">
                    {/* Section header */}
                    <button
                        onClick={() => setShowStats(!showStats)}
                        className="w-full flex items-center gap-2 px-3 py-2 rounded-lg bg-muted/40 hover:bg-muted/70 border border-border transition-colors mb-2 group"
                    >
                        <BarChart3 className="w-3.5 h-3.5 text-muted-foreground group-hover:text-primary transition-colors" />
                        <span className="text-xs font-semibold text-foreground group-hover:text-primary transition-colors">
                            Tag Statistics
                        </span>
                        <span className="text-[10px] text-muted-foreground font-normal bg-muted px-1.5 py-0.5 rounded">
                            {allTagsWithColors.length}
                        </span>
                        {statsLoading && (
                            <Loader2 className="w-3 h-3 text-muted-foreground animate-spin ml-1" />
                        )}
                        <ChevronDown
                            className={`w-3.5 h-3.5 text-muted-foreground ml-auto transition-transform duration-200 ${showStats ? '' : '-rotate-90'}`}
                        />
                    </button>

                    {showStats && (
                        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-2">
                            {allTagsWithColors.map(({ tagId, tagName, color, isBool }) => (
                                <TagStatsPanel
                                    key={tagId}
                                    tagName={tagName}
                                    tagColor={color}
                                    stats={tagStats.get(tagId) || null}
                                    isLoading={statsLoading}
                                    isBool={isBool}
                                    data={historyDataMap.get(tagId)}
                                    timeRange={safeTimeRange}
                                />
                            ))}
                        </div>
                    )}
                </div>
            )}

            {/* Add Chart Button - fixed position */}
            <Button
                onClick={handleAddChart}
                variant="outline"
                size="sm"
                className="fixed bottom-20 right-6 gap-1 z-20 shadow-md"
            >
                <Plus className="w-4 h-4" />
                Add Chart
            </Button>
        </div>
    );
};

export default TrendDashboard;
