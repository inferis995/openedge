import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
    Area,
    AreaChart,
    CartesianGrid,
    ResponsiveContainer,
    Tooltip,
    XAxis,
    YAxis,
} from 'recharts';
import { History, RefreshCw } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { historyApi } from '@/api/history';
import { startLoad } from '@/lib/startLoad';
import i18n from '@/i18n';

const TAG_HISTORY_RANGES = [
    { label: '1h', value: 1 },
    { label: '6h', value: 6 },
    { label: '24h', value: 24 },
    { label: '7d', value: 168 },
];

export interface HistoryPoint {
    t: string;
    v: number | null;
}

/**
 * The one history panel used everywhere — I3X property view, Tags table,
 * Map popup. Same header (name + sample count), same range selector and
 * refresh, same chart: samples are always drawn as dots, gaps stay gaps.
 * Only the data source differs per caller, passed in as fetchData.
 */
export function HistoryPanel({
    title,
    fetchData,
    chartId,
    height = 140,
}: {
    title: string;
    fetchData: (rangeHours: number) => Promise<HistoryPoint[]>;
    chartId: string;
    height?: number;
}) {
    const { t } = useTranslation();
    const [range, setRange] = useState(24);
    const [points, setPoints] = useState<HistoryPoint[]>([]);
    // Starts true: the effect below fetches on mount.
    const [loading, setLoading] = useState(true);

    const load = useCallback(async () => {
        try {
            // Offline markers (null values) are state, not measurements: they
            // must not split the line between two real samples.
            setPoints((await fetchData(range)).filter((pt) => pt.v !== null));
        } catch {
            setPoints([]);
        } finally {
            setLoading(false);
        }
    }, [fetchData, range]);

    // Loading starts true on mount and on every range change (the select
    // handler sets it); the fetch clears it. Nothing is set inside the
    // effect itself.
    useEffect(() => {
        startLoad(load);
    }, [load]);

    const changeRange = (next: number) => {
        if (next === range) return;
        setLoading(true);
        setRange(next);
    };

    const samples = points.filter((p) => p.v !== null).length;

    return (
        <div className="space-y-2">
            <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2 text-sm font-medium min-w-0">
                    <History size={14} className="text-primary shrink-0" />
                    <span className="truncate">{title}</span>
                    <Badge variant="outline" className="text-xs font-mono shrink-0">
                        {t('tagHistory.samples', { count: samples })}
                    </Badge>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                    <Select value={String(range)} onValueChange={(v) => changeRange(Number(v))}>
                        <SelectTrigger className="h-9 sm:h-7 text-xs w-20">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {TAG_HISTORY_RANGES.map((o) => (
                                <SelectItem key={o.value} value={String(o.value)} className="text-xs">
                                    {o.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-9 sm:h-7 w-9 sm:w-7"
                        onClick={() => { setLoading(true); void load(); }}
                        disabled={loading}
                    >
                        <RefreshCw size={12} className={loading ? 'animate-spin' : ''} />
                    </Button>
                </div>
            </div>

            {loading ? (
                <div className="bg-muted animate-pulse rounded" style={{ height }} />
            ) : samples === 0 ? (
                <div
                    className="flex items-center justify-center text-xs text-muted-foreground"
                    style={{ height }}
                >
                    {t('tagHistory.empty')}
                </div>
            ) : (
                <ResponsiveContainer width="100%" height={height}>
                    <AreaChart data={points} margin={{ top: 4, right: 8, left: -20, bottom: 0 }}>
                        <defs>
                            <linearGradient id={chartId} x1="0" y1="0" x2="0" y2="1">
                                <stop offset="5%" stopColor="hsl(var(--primary))" stopOpacity={0.3} />
                                <stop offset="95%" stopColor="hsl(var(--primary))" stopOpacity={0} />
                            </linearGradient>
                        </defs>
                        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                        <XAxis dataKey="t" tick={{ fontSize: 10 }} interval="preserveStartEnd" />
                        <YAxis tick={{ fontSize: 10 }} domain={['auto', 'auto']} />
                        <Tooltip
                            contentStyle={{
                                fontSize: 11,
                                background: 'hsl(var(--card))',
                                border: '1px solid hsl(var(--border))',
                                borderRadius: 6,
                                color: 'hsl(var(--foreground))',
                            }}
                            labelStyle={{ color: 'hsl(var(--muted-foreground))' }}
                            formatter={(v: unknown) => [typeof v === 'number' ? v : String(v ?? ''), title]}
                        />
                        <Area
                            type="monotone"
                            dataKey="v"
                            stroke="hsl(var(--primary))"
                            strokeWidth={1.5}
                            fill={`url(#${chartId})`}
                            connectNulls={false}
                            dot={{ r: 2, fill: 'hsl(var(--primary))', strokeWidth: 0 }}
                            activeDot={{ r: 4 }}
                        />
                    </AreaChart>
                </ResponsiveContainer>
            )}
        </div>
    );
}

/**
 * Tag-based flavor: samples come from the tag historian with the same
 * range selector. Gaps (offline stretches) render as holes, never as zero.
 */
export default function TagHistoryChart({
    tagId,
    tagName,
    height = 140,
}: {
    tagId: number;
    tagName: string;
    height?: number;
}) {
    const fetchData = useCallback(
        async (rangeHours: number): Promise<HistoryPoint[]> => {
            const to = new Date();
            const from = new Date(to.getTime() - rangeHours * 3600_000);
            const rows = await historyApi.query({
                tag_id: tagId,
                start: from.toISOString(),
                end: to.toISOString(),
            });
            return (rows ?? []).map((p) => ({
                t: new Date(p.timestamp).toLocaleString(i18n.language, {
                    hour12: false,
                    day: '2-digit',
                    month: '2-digit',
                    hour: '2-digit',
                    minute: '2-digit',
                }),
                v: p.value,
            }));
        },
        [tagId],
    );
    return (
        <HistoryPanel
            title={tagName}
            fetchData={fetchData}
            chartId={`tagHist-${tagId}`}
            height={height}
        />
    );
}
