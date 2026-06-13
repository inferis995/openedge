import React, { useMemo } from 'react';
import ReactECharts from 'echarts-for-react';
import { TagStats } from '@/api/history';
import { TrendDataPoint } from '@/types/trend';
import { Loader2 } from 'lucide-react';

interface TagStatsPanelProps {
    tagName: string;
    tagColor: string;
    stats: TagStats | null;
    isLoading: boolean;
    isBool?: boolean;
    data?: TrendDataPoint[];
    timeRange?: { start: Date; end: Date };
}

const fmt = (value: number | null | undefined, isBool?: boolean): string => {
    if (value === null || value === undefined) return '—';
    if (isBool) return value >= 0.5 ? 'ON' : 'OFF';
    const n = Number(value);
    if (isNaN(n)) return '—';
    if (Math.abs(n) >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
    if (Math.abs(n) >= 1_000) return (n / 1_000).toFixed(1) + 'k';
    if (Number.isInteger(n)) return n.toString();
    return n.toFixed(3);
};

export const TagStatsPanel: React.FC<TagStatsPanelProps> = ({
    tagName,
    tagColor,
    stats,
    isLoading,
    isBool = false,
    data,
    timeRange,
}) => {
    const sparkData = useMemo(() => {
        if (!data?.length) return [];
        return data
            .filter(p => p.quality === 0 && p.value !== null)
            .map(p => [p.timestamp, typeof p.value === 'boolean' ? (p.value ? 1 : 0) : Number(p.value)] as [number, number]);
    }, [data]);

    const boolStats = useMemo(() => {
        if (!isBool || !data?.length) return null;
        const good = data.filter(p => p.quality === 0);
        if (good.length === 0) return null;
        const onCount = good.filter(p => {
            const v = p.value;
            if (v === null) return false;
            return v >= 0.5;
        }).length;
        const onPct = (onCount / good.length) * 100;
        return { onPct, offPct: 100 - onPct };
    }, [isBool, data]);

    const sparkOption = useMemo(() => {
        if (sparkData.length === 0) return null;
        return {
            animation: false,
            backgroundColor: 'transparent',
            grid: { left: 0, right: 0, top: 2, bottom: 2, containLabel: false },
            xAxis: {
                type: 'time',
                show: false,
                min: timeRange?.start?.getTime(),
                max: timeRange?.end?.getTime(),
            },
            yAxis: { type: 'value', show: false, scale: true },
            series: [{
                type: 'line',
                step: isBool ? ('end' as const) : undefined,
                data: sparkData,
                lineStyle: { color: tagColor, width: 1.5, opacity: 0.85 },
                symbol: 'none',
                areaStyle: { color: tagColor, opacity: 0.12 },
                animation: false,
                silent: true,
            }],
        };
    }, [sparkData, tagColor, timeRange, isBool]);

    if (isLoading) {
        return (
            <div className="bg-card rounded-lg border border-border h-[160px] flex items-center justify-center">
                <Loader2 className="w-4 h-4 text-muted-foreground animate-spin" />
            </div>
        );
    }

    if (!stats || stats.sample_count === 0) {
        return (
            <div className="bg-card rounded-lg border border-border p-3 h-[160px] flex flex-col">
                <div className="flex items-center gap-2 mb-2">
                    <div className="w-2 h-2 rounded-full flex-shrink-0" style={{ backgroundColor: tagColor }} />
                    <span className="text-xs font-semibold text-foreground truncate">{tagName}</span>
                </div>
                <div className="flex-1 flex items-center justify-center text-xs text-muted-foreground">No data</div>
            </div>
        );
    }

    const rangeWidth = (stats.max_value ?? 0) - (stats.min_value ?? 0);
    const avgPos = rangeWidth > 0
        ? Math.max(2, Math.min(98, (((stats.avg_value ?? 0) - (stats.min_value ?? 0)) / rangeWidth) * 100))
        : 50;

    return (
        <div className="bg-card rounded-lg border border-border overflow-hidden shadow-sm hover:border-primary/40 transition-colors">
            {/* Header */}
            <div className="flex items-center gap-2 px-2.5 pt-2 pb-0.5">
                <div
                    className="w-2 h-2 rounded-full flex-shrink-0"
                    style={{ backgroundColor: tagColor }}
                />
                <span className="text-xs font-semibold text-foreground truncate flex-1" title={tagName}>
                    {tagName}
                </span>
                {isBool && (
                    <span className="text-[9px] font-bold uppercase tracking-wider text-blue-400 bg-blue-500/10 px-1 rounded">
                        BOOL
                    </span>
                )}
                <span className="text-[10px] text-muted-foreground tabular-nums">
                    {stats.sample_count >= 1000
                        ? `${(stats.sample_count / 1000).toFixed(1)}k`
                        : stats.sample_count} pts
                </span>
            </div>

            {/* Sparkline */}
            {sparkOption ? (
                <div style={{ height: 58, marginLeft: 1, marginRight: 1 }}>
                    <ReactECharts
                        option={sparkOption}
                        style={{ height: '100%', width: '100%' }}
                        opts={{ renderer: 'canvas' }}
                    />
                </div>
            ) : (
                <div style={{ height: 58 }} className="flex items-center justify-center">
                    <span className="text-[10px] text-muted-foreground italic">no chart data</span>
                </div>
            )}

            <div className="px-2.5 pb-2.5">
                {!isBool ? (
                    <>
                        {/* Min / Avg / Max */}
                        <div className="grid grid-cols-3 gap-0 mb-1.5">
                            {([
                                { label: 'Min', value: stats.min_value, color: '#ef4444' },
                                { label: 'Avg', value: stats.avg_value, color: tagColor },
                                { label: 'Max', value: stats.max_value, color: '#22c55e' },
                            ] as const).map(({ label, value, color }) => (
                                <div key={label} className="text-center">
                                    <div
                                        className="text-[9px] uppercase tracking-wide font-semibold leading-tight"
                                        style={{ color }}
                                    >
                                        {label}
                                    </div>
                                    <div className="text-xs font-bold font-mono text-foreground leading-tight">
                                        {fmt(value)}
                                    </div>
                                </div>
                            ))}
                        </div>

                        {/* Range bar with avg marker */}
                        <div className="relative h-1.5 rounded-full overflow-hidden" style={{ background: 'rgba(100,116,139,0.15)' }}>
                            <div
                                className="absolute inset-0 rounded-full"
                                style={{ background: `linear-gradient(to right, #ef4444, ${tagColor}, #22c55e)`, opacity: 0.5 }}
                            />
                            <div
                                className="absolute top-0 h-full w-px bg-white/90 rounded-full"
                                style={{ left: `${avgPos}%` }}
                            />
                        </div>

                        {stats.std_dev !== null && stats.std_dev !== undefined && (
                            <div className="mt-1 text-center">
                                <span className="text-[9px] text-muted-foreground tabular-nums">
                                    σ {fmt(stats.std_dev)}
                                </span>
                            </div>
                        )}
                    </>
                ) : (
                    <>
                        {/* BOOL: ON/OFF bar */}
                        {boolStats ? (
                            <>
                                <div className="flex rounded overflow-hidden h-4 mb-1">
                                    {boolStats.onPct > 0 && (
                                        <div
                                            className="flex items-center justify-center text-[9px] font-bold text-white"
                                            style={{ width: `${boolStats.onPct}%`, background: '#22c55e' }}
                                        >
                                            {boolStats.onPct >= 18 ? `${boolStats.onPct.toFixed(0)}%` : ''}
                                        </div>
                                    )}
                                    {boolStats.offPct > 0 && (
                                        <div
                                            className="flex items-center justify-center text-[9px] font-bold text-white"
                                            style={{ width: `${boolStats.offPct}%`, background: '#ef4444' }}
                                        >
                                            {boolStats.offPct >= 18 ? `${boolStats.offPct.toFixed(0)}%` : ''}
                                        </div>
                                    )}
                                </div>
                                <div className="flex justify-between">
                                    <span className="text-[10px] font-medium text-green-500 tabular-nums">
                                        ON {boolStats.onPct.toFixed(1)}%
                                    </span>
                                    <span className="text-[10px] font-medium text-red-500 tabular-nums">
                                        OFF {boolStats.offPct.toFixed(1)}%
                                    </span>
                                </div>
                            </>
                        ) : (
                            <div className="flex justify-between">
                                <span className="text-[10px] text-muted-foreground">
                                    {fmt(stats.last_value, true)}
                                </span>
                            </div>
                        )}
                    </>
                )}
            </div>
        </div>
    );
};

export default TagStatsPanel;
