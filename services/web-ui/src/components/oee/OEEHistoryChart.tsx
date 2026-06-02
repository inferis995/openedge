import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
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

const RANGE_CONFIG: Record<Range, { days: number; bucket: 'hour' | 'day'; label: string }> = {
    '7d':  { days: 7,  bucket: 'hour', label: '7 giorni (orario)' },
    '30d': { days: 30, bucket: 'day',  label: '30 giorni (giornaliero)' },
    '90d': { days: 90, bucket: 'day',  label: '3 mesi (giornaliero)' },
};

const formatBucketLabel = (iso: string, bucket: 'hour' | 'day'): string => {
    const d = new Date(iso);
    if (bucket === 'hour') {
        return d.toLocaleString('it-IT', { day: '2-digit', month: '2-digit', hour: '2-digit' });
    }
    return d.toLocaleDateString('it-IT', { day: '2-digit', month: '2-digit' });
};

export const OEEHistoryChart = ({ profileId, target }: Props) => {
    const [range, setRange] = useState<Range>('7d');
    const cfg = RANGE_CONFIG[range];

    const to = new Date().toISOString();
    const from = new Date(Date.now() - cfg.days * 86400_000).toISOString();

    const { data, isLoading, isError } = useQuery({
        queryKey: ['oee-history-v2', profileId, range],
        queryFn: () => oeeApi.historyV2({
            profile_id: profileId,
            from,
            to,
            bucket: cfg.bucket,
        }),
    });

    const chartData = (data ?? []).map((r: OEEHistoryRow) => ({
        label: formatBucketLabel(r.bucket_start, cfg.bucket),
        OEE: parseFloat(r.oee.toFixed(1)),
        Availability: parseFloat(r.availability.toFixed(1)),
        Performance: parseFloat(r.performance.toFixed(1)),
        Quality: parseFloat(r.quality.toFixed(1)),
    }));

    return (
        <Card>
            <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
                <CardTitle className="text-sm font-semibold text-muted-foreground">
                    Storico OEE
                </CardTitle>
                <div className="flex items-center gap-1">
                    {(Object.keys(RANGE_CONFIG) as Range[]).map((r) => (
                        <Button
                            key={r}
                            size="sm"
                            variant={r === range ? 'default' : 'outline'}
                            onClick={() => setRange(r)}
                            className="h-7 px-2 text-xs"
                        >
                            {RANGE_CONFIG[r].label}
                        </Button>
                    ))}
                </div>
            </CardHeader>
            <CardContent>
                {isLoading ? (
                    <div className="flex items-center justify-center h-64 text-muted-foreground">
                        <Loader2 className="animate-spin" /> &nbsp; Caricamento…
                    </div>
                ) : isError ? (
                    <div className="h-64 flex items-center justify-center text-sm text-red-500">
                        Errore nel caricamento dello storico.
                    </div>
                ) : chartData.length === 0 ? (
                    <div className="h-64 flex items-center justify-center text-center text-sm text-muted-foreground p-4">
                        Nessun dato storico per questo periodo.<br />
                        <span className="text-xs">
                            Il cron salva snapshot orari — primi dati disponibili dopo la prima ora di runtime.
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
                                    label={{ value: `Target ${target}%`, fontSize: 10, fill: 'hsl(var(--primary))' }}
                                />
                            )}
                            <Line type="monotone" dataKey="OEE" stroke="#10b981" strokeWidth={2.5} dot={false} />
                            <Line type="monotone" dataKey="Availability" stroke="#3b82f6" strokeWidth={1.5} dot={false} />
                            <Line type="monotone" dataKey="Performance" stroke="#f59e0b" strokeWidth={1.5} dot={false} />
                            <Line type="monotone" dataKey="Quality" stroke="#a855f7" strokeWidth={1.5} dot={false} />
                        </LineChart>
                    </ResponsiveContainer>
                )}
            </CardContent>
        </Card>
    );
};
