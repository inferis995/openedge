import React, { useMemo, useRef, useEffect, useCallback } from 'react';
import ReactECharts from 'echarts-for-react';
import type { EChartsOption } from 'echarts';
import { ChartConfig, ChartSeries } from '@/types/trend';
import { Button } from '@/components/ui/button';
import { X } from 'lucide-react';

interface TrendChartProps {
    chart: ChartConfig;
    seriesData: ChartSeries[];
    timeRange: { start: Date; end: Date };
    onChartClick?: (params: any) => void;
    isActive?: boolean;
    onActivate?: () => void;
    syncGroup?: string;
    onRemove?: () => void;
}

const formatTime = (timestamp: number): string =>
    new Date(timestamp).toLocaleTimeString('it-IT', {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });

const formatDate = (timestamp: number): string => {
    const d = new Date(timestamp);
    return (
        d.toLocaleDateString('it-IT', { day: '2-digit', month: '2-digit' }) +
        ' ' +
        d.toLocaleTimeString('it-IT', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    );
};

const formatValue = (
    value: number | null | undefined | string | boolean,
    isBool: boolean
): string => {
    if (value === null || value === undefined) return 'N/A';

    if (isBool) {
        let num: number;
        if (typeof value === 'boolean') {
            num = value ? 1 : 0;
        } else if (typeof value === 'string') {
            const s = value.toLowerCase();
            num = s === 'true' || s === '1' ? 1 : 0;
        } else {
            num = Number(value);
        }
        return num >= 0.5 ? 'TRUE' : 'FALSE';
    }

    const n = Number(value);
    if (isNaN(n)) return 'N/A';
    if (Number.isInteger(n)) return n.toString();
    return n.toFixed(3);
};

// Colour for BOOL value: green = ON, red = OFF
const boolColor = (value: number | null | undefined): string => {
    if (value === null || value === undefined) return '#9ca3af';
    return value >= 0.5 ? '#16a34a' : '#dc2626';
};

export const TrendChart: React.FC<TrendChartProps> = ({
    chart,
    seriesData,
    timeRange,
    onChartClick,
    isActive = false,
    onActivate,
    syncGroup = 'trend-charts',
    onRemove,
}) => {
    const chartRef = useRef<ReactECharts>(null);

    // Ensure timeRange values are Date objects
    const safeStart = timeRange.start instanceof Date ? timeRange.start : new Date(timeRange.start);
    const safeEnd = timeRange.end instanceof Date ? timeRange.end : new Date(timeRange.end);

    const option: EChartsOption = useMemo(() => {
        if (seriesData.length === 0) return {};

        const rangeHours =
            (safeEnd.getTime() - safeStart.getTime()) / (1000 * 60 * 60);

        // ── Y-axes ────────────────────────────────────────────────────────────
        const yAxis = seriesData.map((s, index) => ({
            type: 'value' as const,
            name: s.tagName,
            nameLocation: 'end' as const,
            nameGap: 8,
            nameTextStyle: { fontSize: 10, color: s.color },
            position: (index % 2 === 0 ? 'left' : 'right') as 'left' | 'right',
            offset: Math.floor(index / 2) * 58,
            axisLine: { show: true, lineStyle: { color: s.color } },
            axisLabel: {
                fontSize: 10,
                formatter: (value: number) => {
                    if (s.isBool) {
                        if (value >= 1) return 'ON';
                        if (value <= 0) return 'OFF';
                        return '';
                    }
                    if (Math.abs(value) >= 1000) return value.toExponential(0);
                    return value.toFixed(1);
                },
            },
            splitLine: {
                show: index === 0,
                lineStyle: { type: 'dashed' as const, color: '#e5e7eb' },
            },
            // BOOL: fixed -0.1..1.1 so ON/OFF are clearly separated
            min: s.isBool ? -0.1 : undefined,
            max: s.isBool ? 1.1 : undefined,
            splitNumber: s.isBool ? 1 : undefined,
        }));

        // ── ECharts series ────────────────────────────────────────────────────
        const echartsSeries: any[] = seriesData.map((s, index) => {
            const base = {
                name: s.tagName,
                type: 'line' as const,
                data: s.data,
                yAxisIndex: index,
                symbol: 'none',
                animation: false,
                emphasis: { focus: 'series' as const },
                large: true,
                largeThreshold: 1000,
            };

            if (s.isBool) {
                // ── Digital (BOOL) series ──────────────────────────────────
                // Keep this intentionally simple: step chart with a single colour.
                // visualMap piecewise (dimension:1) + areaStyle both trigger
                // "Cannot read properties of undefined (reading 'coord')" during
                // ECharts coordinate resolution on step series — removed entirely.
                return {
                    ...base,
                    lineStyle: { width: 3, color: s.color },
                    step: 'end' as const,
                    connectNulls: false,  // null = offline marker → visible gap
                };
            }

            // ── Numeric (INT / REAL / DINT) series ────────────────────────
            return {
                ...base,
                lineStyle: { width: 2, color: s.color },
                connectNulls: false,  // null = offline marker → visible gap
            };
        });

        // ── Tooltip ──────────────────────────────────────────────────────────
        const tooltipFormatter = (params: any) => {
            if (!params?.length) return '';
            // params[0].data can be undefined when the axis fires over a gap
            // (null data point) — find the first param that has actual array data
            const firstValid = (params as any[]).find(p => Array.isArray(p.data));
            if (!firstValid) return '';
            const ts: number = firstValid.data[0];
            let html = `<div style="font-weight:600;color:#6b7280;font-size:10px;margin-bottom:4px;">${formatDate(ts)}</div>`;

            (params as any[]).forEach((param: any) => {
                // Skip params whose data is missing (gaps / null windows)
                if (!Array.isArray(param.data)) return;
                const si = seriesData.find(s => s.tagName === param.seriesName);
                const v: number | null = param.data[1];
                const isNull = v === null || v === undefined;
                const displayVal = formatValue(v, si?.isBool ?? false);
                const dotColour = si?.isBool ? boolColor(v) : param.color;

                html += `
                <div style="display:flex;justify-content:space-between;align-items:center;gap:14px;padding:2px 0;">
                    <div style="display:flex;align-items:center;gap:6px;">
                        <span style="display:inline-block;width:9px;height:9px;border-radius:50%;background:${dotColour};flex-shrink:0;"></span>
                        <span style="color:#374151;font-size:11px;">${param.seriesName}</span>
                    </div>
                    <span style="font-family:monospace;font-weight:700;font-size:12px;color:${isNull ? '#ef4444' : (si?.isBool ? dotColour : '#111827')};">
                        ${displayVal}
                    </span>
                </div>`;
            });

            return `<div style="padding:2px 4px;">${html}</div>`;
        };

        // ── Full chart option ─────────────────────────────────────────────────
        return {
            animation: false,
            grid: {
                left: 58 + (Math.ceil(seriesData.length / 2) - 1) * 58,
                right: 20 + Math.floor(seriesData.length / 2) * 58,
                top: 44,
                bottom: 32,
            },
            tooltip: {
                trigger: 'axis',
                confine: true,
                backgroundColor: 'rgba(255,255,255,0.97)',
                borderColor: '#e5e7eb',
                borderWidth: 1,
                textStyle: { fontSize: 11 },
                formatter: tooltipFormatter,
            },
            axisPointer: {
                link: [{ xAxisIndex: 'all' }],
                snap: true,
                lineStyle: { color: '#3b82f6', width: 1, type: 'dashed' },
            },
            xAxis: {
                type: 'time',
                min: safeStart.getTime(),
                max: safeEnd.getTime(),
                axisLabel: {
                    fontSize: 10,
                    color: '#6b7280',
                    formatter: (value: number) => {
                        if (rangeHours <= 1) return formatTime(value);
                        if (rangeHours <= 24)
                            return new Date(value).toLocaleTimeString('it-IT', {
                                hour: '2-digit',
                                minute: '2-digit',
                            });
                        return new Date(value).toLocaleDateString('it-IT', {
                            day: '2-digit',
                            month: '2-digit',
                        });
                    },
                },
                axisLine: { lineStyle: { color: '#e5e7eb' } },
                splitLine: { show: false },
            },
            yAxis,
            series: echartsSeries,
            dataZoom: [
                {
                    type: 'inside',
                    xAxisIndex: 0,
                    filterMode: 'none',
                },
            ],
        };
    }, [seriesData, timeRange]);

    const handleChartClick = useCallback(
        (params: any) => {
            onActivate?.();
            onChartClick?.(params);
        },
        [onActivate, onChartClick]
    );

    // Connect chart to sync group for cross-chart cursor
    useEffect(() => {
        const instance = chartRef.current?.getEchartsInstance();
        if (instance) {
            instance.group = syncGroup;
        }
    }, [syncGroup]);

    const hasData = seriesData.some(s => s.data?.length > 0);

    if (seriesData.length === 0) {
        return (
            <div
                className={`h-full flex flex-col bg-white rounded-lg border ${
                    isActive ? 'border-blue-300 ring-2 ring-blue-100' : 'border-gray-200'
                } min-h-[250px]`}
                onClick={onActivate}
            >
                <div className="chart-header cursor-move flex items-center justify-between px-2 py-1.5 border-b bg-white flex-shrink-0">
                    <span className="text-xs font-medium text-gray-600 truncate">{chart.title}</span>
                    {onRemove && (
                        <Button variant="ghost" size="icon" className="h-5 w-5 flex-shrink-0" onClick={e => { e.stopPropagation(); onRemove(); }}>
                            <X className="w-3 h-3" />
                        </Button>
                    )}
                </div>
                <div className="flex-1 flex flex-col items-center justify-center cursor-pointer">
                    <svg className="w-12 h-12 text-gray-300 mb-2" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1}
                            d="M7 12l3-3 3 3 4-4M8 21l4-4 4 4M3 4h18M4 4h16v12a1 1 0 01-1 1H5a1 1 0 01-1-1V4z" />
                    </svg>
                    <p className="text-sm text-gray-400">Add tags from the browser</p>
                </div>
            </div>
        );
    }

    if (!hasData) {
        return (
            <div
                className={`h-full flex flex-col bg-white rounded-lg border ${
                    isActive ? 'border-blue-300 ring-2 ring-blue-100' : 'border-gray-200'
                } min-h-[250px]`}
                onClick={onActivate}
            >
                <div className="chart-header cursor-move flex items-center justify-between px-2 py-1.5 border-b bg-white flex-shrink-0">
                    <span className="text-xs font-medium text-gray-600 truncate">{chart.title}</span>
                    {onRemove && (
                        <Button variant="ghost" size="icon" className="h-5 w-5 flex-shrink-0" onClick={e => { e.stopPropagation(); onRemove(); }}>
                            <X className="w-3 h-3" />
                        </Button>
                    )}
                </div>
                <div className="flex-1 flex flex-col items-center justify-center cursor-pointer">
                    <svg className="w-12 h-12 text-gray-300 mb-2" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1}
                            d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
                    </svg>
                    <p className="text-sm text-gray-500">No data for selected time range</p>
                    <p className="text-xs text-gray-400 mt-1">Extend the range or verify data collection</p>
                </div>
            </div>
        );
    }

    return (
        <div
            className={`h-full bg-white rounded-lg border overflow-hidden flex flex-col ${
                isActive ? 'border-blue-300 ring-2 ring-blue-100' : 'border-gray-200'
            }`}
            onClick={onActivate}
        >
            {/* Header — drag handle + tag legend + remove button */}
            <div className="chart-header cursor-move flex-shrink-0 flex items-center gap-2 px-2 py-1.5 border-b bg-white">
                <span className="text-xs font-semibold text-gray-600 truncate flex-shrink-0">
                    {chart.title || 'Chart'}
                </span>
                <div className="flex items-center gap-1.5 flex-wrap flex-1 min-w-0">
                    {seriesData.map(s => (
                        <div key={s.tagId} className="flex items-center gap-1 bg-gray-100 px-1.5 py-0.5 rounded">
                            <div
                                className="w-2 h-2 rounded-full flex-shrink-0"
                                style={{ backgroundColor: s.color }}
                            />
                            <span className="text-xs text-gray-800 font-medium truncate max-w-[110px]">
                                {s.tagName}
                            </span>
                            {s.isBool && (
                                <span className="text-[9px] font-bold uppercase tracking-wider text-gray-400">
                                    B
                                </span>
                            )}
                        </div>
                    ))}
                </div>
                {onRemove && (
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5 flex-shrink-0"
                        onClick={e => { e.stopPropagation(); onRemove(); }}
                    >
                        <X className="w-3 h-3" />
                    </Button>
                )}
            </div>

            {/* Chart */}
            <div className="flex-1 min-h-0 w-full">
                <ReactECharts
                    ref={chartRef}
                    option={option}
                    style={{ height: '100%', width: '100%', minHeight: '200px' }}
                    opts={{ renderer: 'canvas' }}
                    onEvents={{ click: handleChartClick }}
                    notMerge={false}
                    lazyUpdate={true}
                />
            </div>
        </div>
    );
};

export default TrendChart;
