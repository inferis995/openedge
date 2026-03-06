import React from 'react';
import { TagStats } from '@/api/history';
import { Loader2, TrendingUp, TrendingDown, Activity, Sigma, Clock } from 'lucide-react';

interface TagStatsPanelProps {
    tagName: string;
    tagColor: string;
    stats: TagStats | null;
    isLoading: boolean;
    isBool?: boolean;
}

const formatValue = (value: number | null | undefined, isBool?: boolean): string => {
    if (value === null || value === undefined) return 'N/A';

    if (isBool) {
        return value >= 0.5 ? 'ON' : 'OFF';
    }

    const n = Number(value);
    if (isNaN(n)) return 'N/A';
    if (Math.abs(n) >= 1000) return n.toFixed(1);
    if (Number.isInteger(n)) return n.toString();
    return n.toFixed(3);
};

const formatDate = (timestamp: number | null | undefined): string => {
    if (!timestamp) return 'N/A';
    const d = new Date(timestamp);
    return d.toLocaleTimeString('it-IT', {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    });
};

export const TagStatsPanel: React.FC<TagStatsPanelProps> = ({
    tagName,
    tagColor,
    stats,
    isLoading,
    isBool = false
}) => {
    if (isLoading) {
        return (
            <div className="bg-card rounded-lg border border-border p-3 animate-pulse">
                <div className="flex items-center gap-2 mb-2">
                    <div className="w-2 h-2 rounded-full bg-muted" />
                    <div className="h-4 w-24 bg-muted rounded" />
                </div>
                <div className="flex justify-center py-4">
                    <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
                </div>
            </div>
        );
    }

    if (!stats) {
        return null;
    }

    const hasData = stats.sample_count > 0;

    return (
        <div className="bg-card rounded-lg border border-border p-3 shadow-sm">
            {/* Header */}
            <div className="flex items-center gap-2 mb-3 pb-2 border-b border-border">
                <div
                    className="w-2.5 h-2.5 rounded-full flex-shrink-0"
                    style={{ backgroundColor: tagColor }}
                />
                <span className="text-sm font-semibold text-foreground truncate">
                    {tagName}
                </span>
                {isBool && (
                    <span className="text-[9px] font-bold uppercase tracking-wider text-blue-500 bg-blue-500/10 px-1.5 py-0.5 rounded">
                        BOOL
                    </span>
                )}
            </div>

            {!hasData ? (
                <div className="text-center py-2 text-sm text-muted-foreground">
                    No data available
                </div>
            ) : (
                <>
                    {/* Stats Grid */}
                    <div className="grid grid-cols-2 gap-2 mb-2">
                        {/* Min */}
                        <div className="flex items-center gap-2 bg-red-500/10 rounded px-2 py-1.5">
                            <TrendingDown className="w-3.5 h-3.5 text-red-500 flex-shrink-0" />
                            <div className="min-w-0">
                                <div className="text-[10px] text-red-500 font-medium uppercase tracking-wide">Min</div>
                                <div className={`text-sm font-bold ${isBool ? (stats.min_value && stats.min_value >= 0.5 ? 'text-green-500' : 'text-red-500') : 'text-foreground'}`}>
                                    {formatValue(stats.min_value, isBool)}
                                </div>
                            </div>
                        </div>

                        {/* Max */}
                        <div className="flex items-center gap-2 bg-green-500/10 rounded px-2 py-1.5">
                            <TrendingUp className="w-3.5 h-3.5 text-green-500 flex-shrink-0" />
                            <div className="min-w-0">
                                <div className="text-[10px] text-green-500 font-medium uppercase tracking-wide">Max</div>
                                <div className={`text-sm font-bold ${isBool ? (stats.max_value && stats.max_value >= 0.5 ? 'text-green-500' : 'text-red-500') : 'text-foreground'}`}>
                                    {formatValue(stats.max_value, isBool)}
                                </div>
                            </div>
                        </div>

                        {/* Average */}
                        <div className="flex items-center gap-2 bg-blue-500/10 rounded px-2 py-1.5">
                            <Activity className="w-3.5 h-3.5 text-blue-500 flex-shrink-0" />
                            <div className="min-w-0">
                                <div className="text-[10px] text-blue-500 font-medium uppercase tracking-wide">Avg</div>
                                <div className="text-sm font-bold text-foreground">
                                    {formatValue(stats.avg_value, isBool)}
                                </div>
                            </div>
                        </div>

                        {/* Std Dev */}
                        <div className="flex items-center gap-2 bg-purple-500/10 rounded px-2 py-1.5">
                            <Sigma className="w-3.5 h-3.5 text-purple-500 flex-shrink-0" />
                            <div className="min-w-0">
                                <div className="text-[10px] text-purple-500 font-medium uppercase tracking-wide">StDev</div>
                                <div className="text-sm font-bold text-foreground">
                                    {formatValue(stats.std_dev)}
                                </div>
                            </div>
                        </div>
                    </div>

                    {/* Sample Count & Time Range */}
                    <div className="flex items-center justify-between text-[10px] text-muted-foreground pt-2 border-t border-border">
                        <div className="flex items-center gap-1">
                            <Clock className="w-3 h-3" />
                            <span>{formatDate(stats.first_timestamp)} - {formatDate(stats.last_timestamp)}</span>
                        </div>
                        <div className="bg-muted px-1.5 py-0.5 rounded font-medium">
                            {stats.sample_count.toLocaleString()} samples
                        </div>
                    </div>
                </>
            )}
        </div>
    );
};

export default TagStatsPanel;
