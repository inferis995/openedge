import React, { useMemo, useRef, useEffect, useCallback, useState } from 'react';
import ReactECharts from 'echarts-for-react';
import type { EChartsOption } from 'echarts';
import { ChartConfig, ChartSeries, YAxisConfig, LineType, ChartType } from '@/types/trend';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { X, Settings2, Eye, EyeOff, Minus, Check } from 'lucide-react';

interface TrendChartProps {
    chart: ChartConfig;
    seriesData: ChartSeries[];
    timeRange: { start: Date; end: Date };
    onChartClick?: (params: unknown) => void;
    isActive?: boolean;
    onActivate?: () => void;
    syncGroup?: string;
    onRemove?: () => void;
    onRemoveTag?: (tagId: number) => void;
    onUpdateYAxisConfig?: (tagId: number, updates: Partial<YAxisConfig>) => void;
    onUpdateTitle?: (title: string) => void;
}

const fmt = {
    time: (ts: number) =>
        new Date(ts).toLocaleTimeString('it-IT', { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
    datetime: (ts: number) => {
        const d = new Date(ts);
        return (
            d.toLocaleDateString('it-IT', { day: '2-digit', month: '2-digit' }) +
            ' ' +
            d.toLocaleTimeString('it-IT', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
        );
    },
    value: (v: number | null | undefined, isBool: boolean): string => {
        if (v === null || v === undefined) return 'N/A';
        if (isBool) {
            if (typeof v === 'number' && v < 0) return 'N/A';
            return v >= 0.5 ? 'TRUE' : 'FALSE';
        }
        const n = Number(v);
        if (isNaN(n)) return 'N/A';
        if (Number.isInteger(n)) return n.toString();
        return n.toFixed(3);
    },
};

const boolColor = (v: number | null | undefined) => {
    if (v === null || v === undefined || (typeof v === 'number' && v < 0)) return '#9ca3af';
    return v >= 0.5 ? '#16a34a' : '#dc2626';
};

const LINE_TYPE_OPTIONS: { value: LineType; label: string; dash: string }[] = [
    { value: 'solid', label: 'Solid', dash: '────' },
    { value: 'dashed', label: 'Dashed', dash: '– – –' },
    { value: 'dotted', label: 'Dotted', dash: '· · ·' },
];

const CHART_TYPE_OPTIONS: { value: ChartType; label: string }[] = [
    { value: 'line', label: 'Line' },
    { value: 'area', label: 'Area' },
    { value: 'step', label: 'Step' },
    { value: 'bar', label: 'Bar' },
];

// ── Series Settings Panel ────────────────────────────────────────────────────

interface SeriesSettingsPanelProps {
    seriesData: ChartSeries[];
    chart: ChartConfig;
    onUpdate: (tagId: number, updates: Partial<YAxisConfig>) => void;
    onRemoveTag: (tagId: number) => void;
}

const TAG_COLORS_PALETTE = [
    '#3b82f6', '#ef4444', '#22c55e', '#f59e0b', '#8b5cf6',
    '#06b6d4', '#ec4899', '#f97316', '#6366f1', '#14b8a6',
    '#a855f7', '#eab308', '#84cc16', '#f43f5e', '#22d3ee',
];

const SeriesSettingsPanel: React.FC<SeriesSettingsPanelProps> = ({
    seriesData,
    chart,
    onUpdate,
    onRemoveTag,
}) => {
    const [scaleInputs, setScaleInputs] = useState<Record<number, { min: string; max: string }>>({});

    const getConfig = (tagId: number) =>
        chart.yAxisConfigs.find((y) => y.tagId === tagId);

    const handleScaleApply = (tagId: number) => {
        const inputs = scaleInputs[tagId];
        if (!inputs) return;
        const min = inputs.min !== '' ? parseFloat(inputs.min) : undefined;
        const max = inputs.max !== '' ? parseFloat(inputs.max) : undefined;
        onUpdate(tagId, {
            autoScale: false,
            min: isNaN(min as number) ? undefined : min,
            max: isNaN(max as number) ? undefined : max,
        });
    };

    if (seriesData.length === 0) {
        return <p className="text-xs text-muted-foreground p-2">No series in this chart.</p>;
    }

    return (
        <div className="space-y-4 max-h-[480px] overflow-y-auto pr-1">
            {seriesData.map((s) => {
                const cfg = getConfig(s.tagId);
                const isVisible = s.visible ?? true;
                const si = scaleInputs[s.tagId] || {
                    min: cfg?.min != null ? String(cfg.min) : '',
                    max: cfg?.max != null ? String(cfg.max) : '',
                };

                return (
                    <div key={s.tagId} className="border border-border rounded-md p-3 space-y-3">
                        {/* Tag header */}
                        <div className="flex items-center justify-between gap-2">
                            <div className="flex items-center gap-2 min-w-0">
                                <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: s.color }} />
                                <span className="text-xs font-semibold truncate">{s.tagName}</span>
                                {s.isBool && (
                                    <span className="text-[9px] bg-muted px-1 rounded font-bold text-muted-foreground">BOOL</span>
                                )}
                            </div>
                            <div className="flex items-center gap-1">
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-6 w-6"
                                    title={isVisible ? 'Hide series' : 'Show series'}
                                    onClick={() => onUpdate(s.tagId, { visible: !isVisible })}
                                >
                                    {isVisible ? <Eye className="w-3 h-3" /> : <EyeOff className="w-3 h-3 text-muted-foreground" />}
                                </Button>
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-6 w-6 text-red-500 hover:text-red-600 hover:bg-red-50"
                                    title="Remove from chart"
                                    onClick={() => onRemoveTag(s.tagId)}
                                >
                                    <X className="w-3 h-3" />
                                </Button>
                            </div>
                        </div>

                        {!s.isBool && (
                            <>
                                {/* Color palette */}
                                <div>
                                    <p className="text-[10px] text-muted-foreground mb-1">Color</p>
                                    <div className="flex flex-wrap gap-1">
                                        {TAG_COLORS_PALETTE.map((c) => (
                                            <button
                                                key={c}
                                                className="w-5 h-5 rounded-full border-2 transition-transform hover:scale-110"
                                                style={{
                                                    backgroundColor: c,
                                                    borderColor: s.color === c ? '#111' : 'transparent',
                                                }}
                                                onClick={() => onUpdate(s.tagId, { color: c })}
                                            />
                                        ))}
                                    </div>
                                </div>

                                {/* Chart type */}
                                <div>
                                    <p className="text-[10px] text-muted-foreground mb-1">Type</p>
                                    <div className="flex gap-1">
                                        {CHART_TYPE_OPTIONS.map((opt) => (
                                            <button
                                                key={opt.value}
                                                onClick={() => onUpdate(s.tagId, { chartType: opt.value })}
                                                className={`px-2 py-1 text-[10px] rounded border transition-colors ${s.chartType === opt.value
                                                    ? 'bg-primary text-primary-foreground border-primary'
                                                    : 'border-border hover:border-primary/50'
                                                    }`}
                                            >
                                                {opt.label}
                                            </button>
                                        ))}
                                    </div>
                                </div>

                                {/* Line style */}
                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                                    <div>
                                        <p className="text-[10px] text-muted-foreground mb-1">Line style</p>
                                        <div className="flex gap-1">
                                            {LINE_TYPE_OPTIONS.map((opt) => (
                                                <button
                                                    key={opt.value}
                                                    title={opt.label}
                                                    onClick={() => onUpdate(s.tagId, { lineType: opt.value })}
                                                    className={`px-2 py-1 text-[10px] font-mono rounded border transition-colors ${(s.lineType ?? 'solid') === opt.value
                                                        ? 'bg-primary text-primary-foreground border-primary'
                                                        : 'border-border hover:border-primary/50'
                                                        }`}
                                                >
                                                    {opt.dash}
                                                </button>
                                            ))}
                                        </div>
                                    </div>
                                    <div>
                                        <p className="text-[10px] text-muted-foreground mb-1">Width</p>
                                        <div className="flex gap-1">
                                            {[1, 2, 3, 4].map((w) => (
                                                <button
                                                    key={w}
                                                    onClick={() => onUpdate(s.tagId, { lineWidth: w })}
                                                    className={`w-7 h-7 rounded border text-[10px] font-mono transition-colors ${(s.lineWidth ?? 2) === w
                                                        ? 'bg-primary text-primary-foreground border-primary'
                                                        : 'border-border hover:border-primary/50'
                                                        }`}
                                                >
                                                    {w}
                                                </button>
                                            ))}
                                        </div>
                                    </div>
                                </div>

                                {/* Markers + Y side */}
                                <div className="flex items-center gap-4">
                                    <label className="flex items-center gap-1.5 cursor-pointer">
                                        <input
                                            type="checkbox"
                                            className="h-3 w-3 accent-primary"
                                            checked={s.showMarkers ?? false}
                                            onChange={(e) => onUpdate(s.tagId, { showMarkers: e.target.checked })}
                                        />
                                        <span className="text-[10px] text-muted-foreground">Markers</span>
                                    </label>
                                    <div className="flex items-center gap-1">
                                        <span className="text-[10px] text-muted-foreground">Y-axis:</span>
                                        {(['left', 'right'] as const).map((pos) => (
                                            <button
                                                key={pos}
                                                onClick={() => onUpdate(s.tagId, { position: pos })}
                                                className={`px-2 py-0.5 text-[10px] rounded border capitalize ${(s.yPosition ?? 'left') === pos
                                                    ? 'bg-primary text-primary-foreground border-primary'
                                                    : 'border-border hover:border-primary/50'
                                                    }`}
                                            >
                                                {pos}
                                            </button>
                                        ))}
                                    </div>
                                </div>

                                {/* Scale */}
                                <div>
                                    <div className="flex items-center justify-between mb-1">
                                        <p className="text-[10px] text-muted-foreground">Y Scale</p>
                                        <button
                                            onClick={() => onUpdate(s.tagId, { autoScale: true, min: undefined, max: undefined })}
                                            className={`text-[10px] px-1.5 py-0.5 rounded border ${(s.autoScale ?? true) ? 'bg-primary/10 text-primary border-primary/30' : 'border-border hover:border-primary/50'}`}
                                        >
                                            Auto
                                        </button>
                                    </div>
                                    <div className="flex items-center gap-1">
                                        <Input
                                            type="number"
                                            placeholder="Min"
                                            className="h-6 text-[10px] w-16"
                                            value={si.min}
                                            onChange={(e) =>
                                                setScaleInputs((prev) => ({
                                                    ...prev,
                                                    [s.tagId]: { ...si, min: e.target.value },
                                                }))
                                            }
                                        />
                                        <Minus className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                                        <Input
                                            type="number"
                                            placeholder="Max"
                                            className="h-6 text-[10px] w-16"
                                            value={si.max}
                                            onChange={(e) =>
                                                setScaleInputs((prev) => ({
                                                    ...prev,
                                                    [s.tagId]: { ...si, max: e.target.value },
                                                }))
                                            }
                                        />
                                        <Button
                                            variant="outline"
                                            size="icon"
                                            className="h-6 w-6"
                                            onClick={() => handleScaleApply(s.tagId)}
                                        >
                                            <Check className="w-3 h-3" />
                                        </Button>
                                    </div>
                                </div>
                            </>
                        )}
                    </div>
                );
            })}
        </div>
    );
};

// ── Main TrendChart component ────────────────────────────────────────────────

export const TrendChart: React.FC<TrendChartProps> = ({
    chart,
    seriesData,
    timeRange,
    onChartClick,
    isActive = false,
    onActivate,
    syncGroup = 'trend-charts',
    onRemove,
    onRemoveTag,
    onUpdateYAxisConfig,
    onUpdateTitle,
}) => {
    const chartRef = useRef<ReactECharts>(null);
    const [isEditingTitle, setIsEditingTitle] = useState(false);
    const [titleDraft, setTitleDraft] = useState(chart.title || 'Chart');

    const safeStart = timeRange.start instanceof Date ? timeRange.start : new Date(timeRange.start);
    const safeEnd = timeRange.end instanceof Date ? timeRange.end : new Date(timeRange.end);

    // Only visible series participate in chart rendering
    const visibleSeries = useMemo(() => seriesData.filter((s) => s.visible !== false), [seriesData]);

    const option: EChartsOption = useMemo(() => {
        if (visibleSeries.length === 0) return {};

        const rangeHours = (safeEnd.getTime() - safeStart.getTime()) / (1000 * 60 * 60);

        // ── Y-axes ─────────────────────────────────────────────────────────
        const yAxis = visibleSeries.map((s, index) => ({
            type: 'value' as const,
            name: s.tagName,
            nameLocation: 'end' as const,
            nameGap: 8,
            nameTextStyle: { fontSize: 10, color: s.color, opacity: 0.8 },
            position: (s.yPosition ?? (index % 2 === 0 ? 'left' : 'right')) as 'left' | 'right',
            offset: Math.floor(index / 2) * 58,
            axisLine: { show: true, lineStyle: { color: s.color, opacity: 0.7 } },
            axisTick: { lineStyle: { color: s.color, opacity: 0.5 } },
            axisLabel: {
                fontSize: 10,
                color: s.color,
                formatter: (value: number) => {
                    if (s.isBool) {
                        if (value >= 1) return 'ON';
                        if (value === 0) return 'OFF';
                        if (value <= -0.5) return 'N/A';
                        return '';
                    }
                    if (Math.abs(value) >= 10000) return value.toExponential(1);
                    if (Math.abs(value) >= 1000) return (value / 1000).toFixed(1) + 'k';
                    return value.toFixed(1);
                },
            },
            splitLine: {
                show: index === 0,
                lineStyle: { type: 'dashed' as const, color: 'rgba(100,116,139,0.12)', width: 1 },
            },
            min: s.isBool ? -0.6 : (s.autoScale === false && s.yMin !== undefined ? s.yMin : undefined),
            max: s.isBool ? 1.1 : (s.autoScale === false && s.yMax !== undefined ? s.yMax : undefined),
            splitNumber: s.isBool ? 2 : undefined,
        }));

        // ── Series ──────────────────────────────────────────────────────────
        const echartsSeries: object[] = visibleSeries.map((s, index) => {
            const lineTypeToDash = (lt: LineType | undefined): string | number[] => {
                if (lt === 'dashed') return 'dashed';
                if (lt === 'dotted') return [3, 5];
                return 'solid';
            };

            const lineWidth = s.lineWidth ?? 2;
            const lineType = lineTypeToDash(s.lineType);
            const showMarkers = s.showMarkers ?? false;
            const chartType = s.isBool ? 'step' : (s.chartType ?? 'line');

            const base = {
                name: s.tagName,
                type: chartType === 'bar' ? 'bar' : 'line',
                data: s.data,
                yAxisIndex: index,
                animation: false,
                emphasis: { focus: 'series' as const },
                large: true,
                largeThreshold: 1000,
                symbol: showMarkers ? 'circle' : 'none',
                symbolSize: 5,
            };

            if (s.isBool) {
                const threeStateData = s.data.map((pt) => [pt[0], pt[1] === null ? -0.5 : pt[1]]);
                return {
                    ...base,
                    data: threeStateData,
                    lineStyle: { width: 3, color: s.color },
                    step: 'end' as const,
                    connectNulls: false,
                };
            }

            if (chartType === 'bar') {
                return {
                    ...base,
                    barMaxWidth: 8,
                    itemStyle: { color: s.color, opacity: 0.8 },
                    connectNulls: false,
                };
            }

            const lineStyle = { width: lineWidth, color: s.color, type: lineType as any };

            if (chartType === 'area') {
                return {
                    ...base,
                    lineStyle,
                    areaStyle: { color: s.color, opacity: s.areaOpacity ?? 0.15 },
                    connectNulls: false,
                };
            }

            if (chartType === 'step') {
                return {
                    ...base,
                    lineStyle,
                    step: 'end' as const,
                    connectNulls: false,
                };
            }

            // Default: line
            return {
                ...base,
                lineStyle,
                connectNulls: false,
            };
        });

        // ── Tooltip ─────────────────────────────────────────────────────────
        const tooltipFormatter = (params: unknown) => {
            const p = params as any[];
            if (!p?.length) return '';
            const firstValid = p.find((x: any) => Array.isArray(x.data));
            if (!firstValid) return '';
            const ts: number = firstValid.data[0];

            let html = `<div style="font-size:10px;color:#94a3b8;font-weight:600;margin-bottom:5px;border-bottom:1px solid rgba(148,163,184,0.2);padding-bottom:4px;">${fmt.datetime(ts)}</div>`;

            p.forEach((param: any) => {
                if (!Array.isArray(param.data)) return;
                const si = visibleSeries.find((s) => s.tagName === param.seriesName);
                const v: number | null = param.data[1];
                const isNull = v === null || v === undefined || (si?.isBool && typeof v === 'number' && v < 0);
                const displayVal = fmt.value(v, si?.isBool ?? false);
                const dotColour = si?.isBool ? boolColor(v) : param.color;

                html += `
                <div style="display:flex;justify-content:space-between;align-items:center;gap:16px;padding:2px 0;">
                    <div style="display:flex;align-items:center;gap:6px;min-width:0;">
                        <span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${dotColour};flex-shrink:0;"></span>
                        <span style="color:#cbd5e1;font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:120px;">${param.seriesName}</span>
                    </div>
                    <span style="font-family:'JetBrains Mono',monospace,sans-serif;font-weight:700;font-size:12px;color:${isNull ? '#f87171' : (si?.isBool ? dotColour : '#f1f5f9')};flex-shrink:0;">
                        ${displayVal}
                    </span>
                </div>`;
            });

            return `<div style="background:rgba(15,23,42,0.95);padding:8px 10px;border-radius:6px;min-width:160px;">${html}</div>`;
        };

        const leftAxes = Math.ceil(visibleSeries.length / 2);
        const rightAxes = Math.floor(visibleSeries.length / 2);

        return {
            animation: false,
            backgroundColor: 'transparent',
            grid: {
                left: 20 + Math.max(0, leftAxes - 1) * 58,
                right: 20 + Math.max(0, rightAxes - 1) * 58,
                top: 44,
                bottom: 52, // room for data zoom slider
            },
            tooltip: {
                trigger: 'axis',
                confine: true,
                backgroundColor: 'transparent',
                borderColor: 'transparent',
                borderWidth: 0,
                padding: 0,
                textStyle: { fontSize: 11 },
                formatter: tooltipFormatter,
                extraCssText: 'box-shadow: 0 4px 24px rgba(0,0,0,0.3)',
            },
            axisPointer: {
                link: [{ xAxisIndex: 'all' }],
                snap: true,
                lineStyle: { color: '#60a5fa', width: 1, type: 'solid' },
                label: {
                    backgroundColor: '#1e40af',
                    formatter: (params: any) => {
                        if (typeof params.value === 'number') {
                            return rangeHours <= 1 ? fmt.time(params.value) :
                                rangeHours <= 24
                                    ? new Date(params.value).toLocaleTimeString('it-IT', { hour: '2-digit', minute: '2-digit' })
                                    : new Date(params.value).toLocaleDateString('it-IT', { day: '2-digit', month: '2-digit' });
                        }
                        return '';
                    }
                }
            },
            toolbox: {
                show: true,
                orient: 'horizontal' as const,
                right: 8,
                top: 6,
                itemSize: 13,
                itemGap: 6,
                feature: {
                    dataZoom: {
                        yAxisIndex: 'none' as any,
                        title: { zoom: 'Zoom area', back: 'Undo zoom' },
                        icon: {
                            zoom: 'path://M11 17.25a6.25 6.25 0 110-12.5 6.25 6.25 0 010 12.5zm4.773.042L21.021 21.5',
                            back: 'path://M13 9H5.07M5.07 9l4 4m-4-4 4-4',
                        },
                    },
                    restore: { title: 'Reset zoom' },
                    saveAsImage: {
                        title: 'Save PNG',
                        type: 'png',
                        pixelRatio: 2,
                    },
                },
                iconStyle: {
                    borderColor: '#64748b',
                    borderWidth: 1,
                },
                emphasis: {
                    iconStyle: {
                        borderColor: '#3b82f6',
                        color: '#eff6ff',
                    },
                },
            },
            xAxis: {
                type: 'time',
                min: safeStart.getTime(),
                max: safeEnd.getTime(),
                axisLabel: {
                    fontSize: 10,
                    color: '#94a3b8',
                    formatter: (value: number) => {
                        if (rangeHours <= 1) return fmt.time(value);
                        if (rangeHours <= 24)
                            return new Date(value).toLocaleTimeString('it-IT', { hour: '2-digit', minute: '2-digit' });
                        return new Date(value).toLocaleDateString('it-IT', { day: '2-digit', month: '2-digit' });
                    },
                },
                axisLine: { lineStyle: { color: 'rgba(100,116,139,0.3)' } },
                axisTick: { lineStyle: { color: 'rgba(100,116,139,0.2)' } },
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
                {
                    type: 'slider',
                    xAxisIndex: 0,
                    height: 18,
                    bottom: 8,
                    filterMode: 'none',
                    borderColor: 'rgba(100,116,139,0.2)',
                    backgroundColor: 'rgba(100,116,139,0.05)',
                    fillerColor: 'rgba(59,130,246,0.1)',
                    handleStyle: { color: '#3b82f6', borderColor: '#3b82f6' },
                    moveHandleStyle: { color: '#3b82f6' },
                    emphasis: { handleStyle: { color: '#2563eb' }, handleLabel: { show: false } },
                    dataBackground: {
                        lineStyle: { color: 'rgba(59,130,246,0.3)', width: 1 },
                        areaStyle: { color: 'rgba(59,130,246,0.05)' },
                    },
                    labelFormatter: (value: number) => fmt.time(value),
                    textStyle: { fontSize: 9, color: '#94a3b8' },
                },
            ],
        };
    }, [visibleSeries, timeRange]);

    const handleChartClick = useCallback(
        (params: unknown) => {
            onActivate?.();
            onChartClick?.(params);
        },
        [onActivate, onChartClick]
    );

    useEffect(() => {
        const instance = chartRef.current?.getEchartsInstance();
        if (instance) {
            instance.group = syncGroup;
        }
    }, [syncGroup]);

    const hasData = visibleSeries.some((s) => s.data?.length > 0);

    const emptyState = (msg: string, sub?: string) => (
        <div
            className={`h-full flex flex-col bg-card border ${isActive ? 'border-primary ring-2 ring-primary/20' : 'border-border'} min-h-[250px]`}
            onClick={onActivate}
        >
            <ChartHeader
                chart={chart}
                seriesData={seriesData}
                isEditingTitle={isEditingTitle}
                titleDraft={titleDraft}
                setIsEditingTitle={setIsEditingTitle}
                setTitleDraft={setTitleDraft}
                onUpdateTitle={onUpdateTitle}
                onRemove={onRemove}
                onRemoveTag={onRemoveTag}
                onUpdateYAxisConfig={onUpdateYAxisConfig}
            />
            <div className="flex-1 flex flex-col items-center justify-center gap-2 text-muted-foreground/50">
                <svg className="w-10 h-10" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M7 12l3-3 3 3 4-4M8 21l4-4 4 4M3 4h18M4 4h16v12a1 1 0 01-1 1H5a1 1 0 01-1-1V4z" />
                </svg>
                <p className="text-sm">{msg}</p>
                {sub && <p className="text-xs text-muted-foreground/40">{sub}</p>}
            </div>
        </div>
    );

    if (seriesData.length === 0) return emptyState('Add tags from the browser');
    if (!hasData) return emptyState('No data for selected range', 'Extend the range or verify data collection');

    return (
        <div
            className={`h-full bg-card border overflow-hidden flex flex-col ${isActive ? 'border-primary ring-2 ring-primary/20' : 'border-border'}`}
            onClick={onActivate}
        >
            <ChartHeader
                chart={chart}
                seriesData={seriesData}
                isEditingTitle={isEditingTitle}
                titleDraft={titleDraft}
                setIsEditingTitle={setIsEditingTitle}
                setTitleDraft={setTitleDraft}
                onUpdateTitle={onUpdateTitle}
                onRemove={onRemove}
                onRemoveTag={onRemoveTag}
                onUpdateYAxisConfig={onUpdateYAxisConfig}
            />
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

// ── Chart Header ─────────────────────────────────────────────────────────────

interface ChartHeaderProps {
    chart: ChartConfig;
    seriesData: ChartSeries[];
    isEditingTitle: boolean;
    titleDraft: string;
    setIsEditingTitle: (v: boolean) => void;
    setTitleDraft: (v: string) => void;
    onUpdateTitle?: (title: string) => void;
    onRemove?: () => void;
    onRemoveTag?: (tagId: number) => void;
    onUpdateYAxisConfig?: (tagId: number, updates: Partial<YAxisConfig>) => void;
}

const ChartHeader: React.FC<ChartHeaderProps> = ({
    chart,
    seriesData,
    isEditingTitle,
    titleDraft,
    setIsEditingTitle,
    setTitleDraft,
    onUpdateTitle,
    onRemove,
    onRemoveTag,
    onUpdateYAxisConfig,
}) => {
    const commitTitle = () => {
        const t = titleDraft.trim() || 'Chart';
        onUpdateTitle?.(t);
        setTitleDraft(t);
        setIsEditingTitle(false);
    };

    return (
        <div className="chart-header cursor-move flex-shrink-0 flex items-center gap-2 px-2 py-1.5 border-b bg-muted/30 select-none">
            {/* Title */}
            {isEditingTitle ? (
                <Input
                    autoFocus
                    value={titleDraft}
                    onChange={(e) => setTitleDraft(e.target.value)}
                    onBlur={commitTitle}
                    onKeyDown={(e) => { if (e.key === 'Enter') commitTitle(); if (e.key === 'Escape') setIsEditingTitle(false); }}
                    className="h-5 text-xs px-1 py-0 w-28 flex-shrink-0"
                    onClick={(e) => e.stopPropagation()}
                />
            ) : (
                <span
                    className="text-xs font-semibold text-muted-foreground truncate flex-shrink-0 cursor-text hover:text-foreground transition-colors"
                    title="Double-click to rename"
                    onDoubleClick={(e) => { e.stopPropagation(); setIsEditingTitle(true); }}
                >
                    {chart.title || 'Chart'}
                </span>
            )}

            {/* Series pills */}
            <div className="flex items-center gap-1 flex-wrap flex-1 min-w-0 overflow-hidden">
                {seriesData.map((s) => (
                    <div
                        key={s.tagId}
                        className={`flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium transition-opacity ${s.visible === false ? 'opacity-40' : ''}`}
                        style={{ backgroundColor: `${s.color}22`, borderLeft: `2px solid ${s.color}` }}
                    >
                        <span className="truncate max-w-[90px]">{s.tagName}</span>
                        {s.isBool && <span className="text-[8px] font-bold text-muted-foreground">BOOL</span>}
                    </div>
                ))}
            </div>

            {/* Settings */}
            {seriesData.length > 0 && onUpdateYAxisConfig && onRemoveTag && (
                <Popover>
                    <PopoverTrigger asChild>
                        <Button
                            variant="ghost"
                            size="icon"
                            className="h-5 w-5 flex-shrink-0 text-muted-foreground hover:text-foreground"
                            title="Series settings"
                            onClick={(e) => e.stopPropagation()}
                        >
                            <Settings2 className="w-3 h-3" />
                        </Button>
                    </PopoverTrigger>
                    <PopoverContent
                        className="w-80"
                        align="end"
                        onClick={(e) => e.stopPropagation()}
                    >
                        <div className="mb-3">
                            <p className="text-sm font-semibold">Series Settings</p>
                            <p className="text-[10px] text-muted-foreground">Configure each tag's display style</p>
                        </div>
                        <SeriesSettingsPanel
                            seriesData={seriesData}
                            chart={chart}
                            onUpdate={onUpdateYAxisConfig}
                            onRemoveTag={onRemoveTag}
                        />
                    </PopoverContent>
                </Popover>
            )}

            {/* Close */}
            {onRemove && (
                <Button
                    variant="ghost"
                    size="icon"
                    className="h-5 w-5 flex-shrink-0 text-muted-foreground hover:text-destructive"
                    title="Remove chart"
                    onClick={(e) => { e.stopPropagation(); onRemove(); }}
                >
                    <X className="w-3 h-3" />
                </Button>
            )}
        </div>
    );
};

export default TrendChart;
