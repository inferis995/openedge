import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Loader2, AlertCircle, Wrench, Activity, Gauge } from 'lucide-react';

import { oeeApi } from '@/api/dashboard';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';

// OEELossPareto — il "Pareto delle cause di perdita". Mostra le 6 Big
// Losses (ISO 22400-2) ordinate per durata totale decrescente, con barra
// percentuale e count eventi. Sotto: MTBF / MTTR cards.
//
// Popolato automaticamente dal cron worker (PopulateLossEvents):
// allarmi critical chiusi → breakdown, maintenance setup → setup.
// Niente data entry operatore — il caporeparto vede solo il risultato.

interface Props {
    profileId: number;
    profileName: string;
}

type Range = '24h' | '7d' | '30d';

const RANGE_CFG: Record<Range, { hours: number; label: string }> = {
    '24h': { hours: 24,      label: '24 ore' },
    '7d':  { hours: 24 * 7,  label: '7 giorni' },
    '30d': { hours: 24 * 30, label: '30 giorni' },
};

const formatMinutes = (m: number): string => {
    if (m < 60) return `${m.toFixed(0)} min`;
    if (m < 1440) return `${(m / 60).toFixed(1)} h`;
    return `${(m / 1440).toFixed(1)} g`;
};

const pillarBadge = (pillar: string): string => {
    if (pillar === 'availability') return 'bg-blue-500/10 text-blue-500';
    if (pillar === 'performance')  return 'bg-amber-500/10 text-amber-500';
    return 'bg-purple-500/10 text-purple-500';
};

export const OEELossPareto = ({ profileId, profileName }: Props) => {
    const [range, setRange] = useState<Range>('7d');
    const cfg = RANGE_CFG[range];

    const to = new Date().toISOString();
    const from = new Date(Date.now() - cfg.hours * 3600_000).toISOString();

    const { data: lossData, isLoading } = useQuery({
        queryKey: ['oee-loss-tree', profileId, range],
        queryFn: () => oeeApi.lossTree({ profile_id: profileId, from, to }),
    });

    const { data: reliab } = useQuery({
        queryKey: ['oee-reliability', profileId, range],
        queryFn: () => oeeApi.reliability(profileId, from, to),
    });

    return (
        <div className="space-y-4">
            <div className="flex items-center justify-between flex-wrap gap-2">
                <div>
                    <h3 className="text-base font-semibold">{profileName}</h3>
                    <p className="text-xs text-muted-foreground">
                        Pareto delle cause di perdita (Six Big Losses, ISO 22400-2)
                    </p>
                </div>
                <div className="flex items-center gap-1">
                    {(Object.keys(RANGE_CFG) as Range[]).map((r) => (
                        <Button
                            key={r}
                            size="sm"
                            variant={r === range ? 'default' : 'outline'}
                            onClick={() => setRange(r)}
                            className="h-9 sm:h-7 px-2 text-xs"
                        >
                            {RANGE_CFG[r].label}
                        </Button>
                    ))}
                </div>
            </div>

            {/* MTBF / MTTR cards */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                <Card>
                    <CardHeader className="pb-2">
                        <CardTitle className="text-xs uppercase tracking-wider text-muted-foreground flex items-center gap-2">
                            <Activity size={12} /> MTBF
                        </CardTitle>
                    </CardHeader>
                    <CardContent>
                        <div className="text-3xl font-bold tracking-tight">
                            {reliab && reliab.mtbf_hours > 0 ? reliab.mtbf_hours.toFixed(1) : '—'}
                            <span className="text-base text-muted-foreground ml-1">h</span>
                        </div>
                        <p className="text-[11px] text-muted-foreground">
                            Mean Time Between Failures
                        </p>
                    </CardContent>
                </Card>
                <Card>
                    <CardHeader className="pb-2">
                        <CardTitle className="text-xs uppercase tracking-wider text-muted-foreground flex items-center gap-2">
                            <Wrench size={12} /> MTTR
                        </CardTitle>
                    </CardHeader>
                    <CardContent>
                        <div className="text-3xl font-bold tracking-tight">
                            {reliab && reliab.mttr_minutes > 0 ? formatMinutes(reliab.mttr_minutes) : '—'}
                        </div>
                        <p className="text-[11px] text-muted-foreground">
                            Mean Time To Repair
                        </p>
                    </CardContent>
                </Card>
                <Card>
                    <CardHeader className="pb-2">
                        <CardTitle className="text-xs uppercase tracking-wider text-muted-foreground flex items-center gap-2">
                            <AlertCircle size={12} /> Guasti
                        </CardTitle>
                    </CardHeader>
                    <CardContent>
                        <div className="text-3xl font-bold tracking-tight">
                            {reliab?.breakdown_count ?? 0}
                        </div>
                        <p className="text-[11px] text-muted-foreground">
                            Eventi breakdown nel periodo
                        </p>
                    </CardContent>
                </Card>
            </div>

            {/* Pareto chart */}
            <Card>
                <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
                    <CardTitle className="text-sm font-semibold text-muted-foreground flex items-center gap-2">
                        <Gauge size={14} /> Pareto delle cause di perdita
                    </CardTitle>
                    {lossData && lossData.total_minutes > 0 && (
                        <Badge variant="outline" className="text-xs">
                            Totale: {formatMinutes(lossData.total_minutes)}
                        </Badge>
                    )}
                </CardHeader>
                <CardContent>
                    {isLoading ? (
                        <div className="flex items-center justify-center h-48 text-muted-foreground">
                            <Loader2 className="animate-spin" /> &nbsp; Caricamento…
                        </div>
                    ) : !lossData || lossData.total_minutes === 0 ? (
                        <div className="py-12 text-center text-sm text-muted-foreground">
                            Nessuna perdita registrata nel periodo.
                            <p className="text-xs mt-2">
                                Il cron worker registra automaticamente: allarmi critical chiusi (breakdown),
                                manutenzioni con "setup"/"cambio" nel titolo (setup).
                            </p>
                        </div>
                    ) : (
                        <div className="space-y-2">
                            {lossData.categories.map((c) => (
                                <div key={c.category_id} className="space-y-1">
                                    <div className="flex items-center justify-between text-xs">
                                        <div className="flex items-center gap-2 min-w-0">
                                            <Badge className={`text-[10px] ${pillarBadge(c.pillar)} border-none`}>
                                                {c.pillar}
                                            </Badge>
                                            <span className="font-medium truncate">{c.display_label}</span>
                                        </div>
                                        <div className="flex items-center gap-2 text-[11px] text-muted-foreground font-mono whitespace-nowrap">
                                            <span>{c.events_count} eventi</span>
                                            <span>·</span>
                                            <span className="text-foreground font-semibold">{formatMinutes(c.total_minutes)}</span>
                                            <span>·</span>
                                            <span>{c.percent_of_total.toFixed(0)}%</span>
                                        </div>
                                    </div>
                                    <div className="w-full h-3 bg-muted rounded overflow-hidden">
                                        <div
                                            className="h-3 rounded transition-all"
                                            style={{
                                                width: `${c.percent_of_total}%`,
                                                background: c.color,
                                            }}
                                        />
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </CardContent>
            </Card>
        </div>
    );
};
