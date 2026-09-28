import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
    LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
    Legend, ReferenceLine,
} from 'recharts';
import { Loader2 } from 'lucide-react';

import { oeeApi, OEEHistoryRow } from '@/api/dashboard';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';

// OEEHistoryChart — grafico storico per un profilo o per il rollup fabbrica.
//
// Legge da /api/oee/history-v2 (snapshot persistiti dal cron) — quindi
// può mostrare finestre arbitrariamente lunghe senza pesare su tag_history.
// Selector range: ultimi 7g (hour) / 30g (day) / 3 mesi (day).
//
// Target prop = soglia OEE del profilo (mostra ReferenceLine).

interface Props {
    profileId: number | null; // null = rollup fabbrica
    target?: number;
}

type Range = '7d' | '30d' | '90d';

// label = i18n key, resolved with t() at render.
const RANGE_CONFIG: Record<Range, { days: number; bucket: 'hour' | 'day'; label: string }> = {
    '7d':  { days: 7,  bucket: 'hour', label: 'oee.history_range_7d' },
    '30d': { days: 30, bucket: 'day',  label: 'oee.history_range_30d' },
    '90d': { days: 90, bucket: 'day',  label: 'oee.history_range_90d' },
};

const formatBucketLabel = (iso: string, bucket: 'hour' | 'day', lang: string): string => {
    const d = new Date(iso);
    if (bucket === 'hour') {
        return d.toLocaleString(lang, { day: '2-digit', month: '2-digit', hour: '2-digit' });
    }
    return d.toLocaleDateString(lang, { day: '2-digit', month: '2-digit' });
};

export const OEEHistoryChart = ({ profileId, target }: Props) => {
    const { t, i18n } = useTranslation();
    const [range, setRange] = useState<Range>('7d');
    const cfg = RANGE_CONFIG[range];

    const { data, isLoading, isError } = useQuery({
        queryKey: ['oee-history-v2', profileId, range],
        // Window computed at fetch time: reading the clock during render is impure.
        queryFn: () => {
            const now = Date.now();
            return oeeApi.historyV2({
                profile_id: profileId,
                from: new Date(now - cfg.days * 86400_000).toISOString(),
                to: new Date(now).toISOString(),
                bucket: cfg.bucket,
            });
        },
    });

    const chartData = (data ?? []).map((r: OEEHistoryRow) => ({
        label: formatBucketLabel(r.bucket_start, cfg.bucket, i18n.language),
        OEE: parseFloat(r.oee.toFixed(1)),
        Availability: parseFloat(r.availability.toFixed(1)),
        Performance: parseFloat(r.performance.toFixed(1)),
        Quality: parseFloat(r.quality.toFixed(1)),
    }));

    return (
        <Card>
            <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
                <CardTitle className="text-sm font-semibold text-muted-foreground">
                    {t('oee.history_title')}
                </CardTitle>
                <div className="flex items-center gap-1">
                    {(Object.keys(RANGE_CONFIG) as Range[]).map((r) => (
                        <Button
                            key={r}
                            size="sm"
                            variant={r === range ? 'default' : 'outline'}
                            onClick={() => setRange(r)}
                            className="h-9 sm:h-7 px-2 text-xs"
                        >
                            {t(RANGE_CONFIG[r].label)}
                        </Button>
                    ))}
                </div>
            </CardHeader>
            <CardContent>
                {isLoading ? (
                    <div className="flex items-center justify-center h-64 text-muted-foreground">
                        <Loader2 className="animate-spin" /> &nbsp; {t('common.loading')}
                    </div>
                ) : isError ? (
                    <div className="h-64 flex items-center justify-center text-sm text-red-500">
                        {t('oee.history_error')}
                    </div>
                ) : chartData.length === 0 ? (
                    <div className="h-64 flex items-center justify-center text-center text-sm text-muted-foreground p-4">
                        {t('oee.history_empty')}<br />
                        <span className="text-xs">
                            {t('oee.history_empty_hint')}
                        </span>
                    </div>
                ) : (
                    <ResponsiveContainer width="100%" height={320}>
                        <LineChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                            <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" opacity={0.4} />
                            <XAxis
                                dataKey="label"
                                stroke="hsl(var(--muted-foreground))"
                                tick={{ fontSize: 10 }}
                                interval="preserveStartEnd"
                            />
                            <YAxis
                                domain={[0, 100]}
                                stroke="hsl(var(--muted-foreground))"
                                tick={{ fontSize: 10 }}
                                unit="%"
                            />
                            <Tooltip
                                contentStyle={{
                                    background: 'hsl(var(--card))',
                                    border: '1px solid hsl(var(--border))',
                                    fontSize: 12,
                                }}
                            />
                            <Legend wrapperStyle={{ fontSize: 12 }} />
                            {target !== undefined && target > 0 && (
                                <ReferenceLine
                                    y={target}
                                    stroke="hsl(var(--primary))"
                                    strokeDasharray="3 3"
                                    label={{ value: t('oee.target_line', { value: target }), fontSize: 10, fill: 'hsl(var(--primary))' }}
                                />
                            )}
                            <Line type="monotone" dataKey="OEE" name="OEE" stroke="#10b981" strokeWidth={2.5} dot={false} />
                            <Line type="monotone" dataKey="Availability" name={t('oee.availability')} stroke="#3b82f6" strokeWidth={1.5} dot={false} />
                            <Line type="monotone" dataKey="Performance" name={t('oee.performance')} stroke="#f59e0b" strokeWidth={1.5} dot={false} />
                            <Line type="monotone" dataKey="Quality" name={t('oee.quality')} stroke="#a855f7" strokeWidth={1.5} dot={false} />
                        </LineChart>
                    </ResponsiveContainer>
                )}
            </CardContent>
        </Card>
    );
};
