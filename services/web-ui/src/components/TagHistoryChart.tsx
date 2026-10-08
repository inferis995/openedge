import { useCallback, useEffect, useMemo, useState } from 'react';
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
import { History } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
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

export const TAG_HISTORY_RANGES = [
    { label: '1h', value: 1 },
    { label: '6h', value: 6 },
    { label: '24h', value: 24 },
    { label: '7d', value: 168 },
];

/**
 * Shared tag history chart: the same samples-and-range view everywhere
 * (I3X property panel, Tags table, Map popup). One tag, one range selector,
 * gaps in the data stay gaps — an offline stretch reads as a hole, never
 * as a zero line.
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
    const { t } = useTranslation();
    const [range, setRange] = useState(24);
    const [points, setPoints] = useState<{ t: string; v: number | null }[]>([]);
    const [loading, setLoading] = useState(true);

    const fetchHistory = useCallback(async () => {
        try {
            const to = new Date();
            const from = new Date(to.getTime() - range * 3600_000);
            const rows = await historyApi.query({
                tag_id: tagId,
                start: from.toISOString(),
                end: to.toISOString(),
            });
            setPoints(
                (rows ?? []).map((p) => ({
                    t: new Date(p.timestamp).toLocaleString(i18n.language, {
                        hour12: false,
                        day: '2-digit',
                        month: '2-digit',
                        hour: '2-digit',
                        minute: '2-digit',
                    }),
                    v: p.value,
                })),
            );
        } catch {
            setPoints([]);
        } finally {
            setLoading(false);
        }
    }, [tagId, range]);

    useEffect(() => {
        setLoading(true);
        startLoad(fetchHistory);
    }, [fetchHistory]);

    const samples = useMemo(() => points.filter((p) => p.v !== null).length, [points]);

    return (
        <div className="space-y-2">
            <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2 text-sm font-medium min-w-0">
                    <History size={14} className="text-primary shrink-0" />
                    <span className="truncate">{tagName}</span>
                    <Badge variant="outline" className="text-xs font-mono shrink-0">
                        {t('tagHistory.samples', { count: samples })}
                    </Badge>
                </div>
                <Select
                    value={String(range)}
                    onValueChange={(v) => { setRange(Number(v)); setLoading(true); }}
                >
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
            </div>

            {loading ? (
                <div className="h-40 bg-muted animate-pulse rounded" style={{ height }} />
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
                            <linearGradient id={`tagHist-${tagId}`} x1="0" y1="0" x2="0" y2="1">
                                <stop offset="5%" stopColor="hsl(var(--primary))" stopOpacity={0.3} />
                                <stop offset="95%" stopColor="hsl(var(--primary))" stopOpacity={0} />
                            </linearGradient>
                        </defs>
                        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                        <XAxis dataKey="t" tick={{ fontSize: 10 }} interval="preserveStartEnd" />
                        <YAxis tick={{ fontSize: 10 }} domain={['auto', 'auto']} />
                        <Tooltip contentStyle={{ fontSize: 11 }} />
                        <Area
                            type="monotone"
                            dataKey="v"
                            stroke="hsl(var(--primary))"
                            strokeWidth={1.5}
                            fill={`url(#tagHist-${tagId})`}
                            connectNulls={false}
                            dot={false}
                        />
                    </AreaChart>
                </ResponsiveContainer>
            )}
        </div>
    );
}
