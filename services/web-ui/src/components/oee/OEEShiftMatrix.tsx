import { useQuery } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { Loader2 } from 'lucide-react';

import { oeeApi, OEEShiftRow } from '@/api/dashboard';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';

// OEEShiftMatrix — la matrice "turni × giorni" che la direzione di
// produzione richiede sempre come prima cosa. Aggregato lato server da
// /api/oee/by-shift (basato sugli snapshot orari del cron).
//
// UX: righe = turni (mattina/pomeriggio/notte ordinati per id),
//     colonne = giorni (più recente a sinistra),
//     celle = numero OEE colorato per banda ISO 22400.
// Click su cella → potrebbe espandere il dettaglio (TODO commit successivo).

interface Props {
    profileId: number | null; // null = rollup fabbrica
}

type Range = '7d' | '30d';

const RANGE_CFG: Record<Range, { days: number; label: string }> = {
    '7d':  { days: 7,  label: '7 giorni' },
    '30d': { days: 30, label: '30 giorni' },
};

const bandColor = (v: number): string => {
    if (v === 0) return 'bg-muted/30 text-muted-foreground';
    if (v >= 85) return 'bg-emerald-500/20 text-emerald-600 dark:text-emerald-400 border-emerald-500/40';
    if (v >= 65) return 'bg-amber-500/20 text-amber-600 dark:text-amber-400 border-amber-500/40';
    if (v >= 40) return 'bg-orange-500/20 text-orange-600 dark:text-orange-400 border-orange-500/40';
    return 'bg-red-500/20 text-red-600 dark:text-red-400 border-red-500/40';
};

const formatDayLabel = (iso: string): string => {
    const d = new Date(iso);
    return d.toLocaleDateString('it-IT', { day: '2-digit', month: '2-digit' });
};

export const OEEShiftMatrix = ({ profileId }: Props) => {
    const [range, setRange] = useState<Range>('7d');
    const cfg = RANGE_CFG[range];

    const to = new Date().toISOString();
    const from = new Date(Date.now() - cfg.days * 86400_000).toISOString();

    const { data, isLoading, isError } = useQuery({
        queryKey: ['oee-by-shift', profileId, range],
        queryFn: () => oeeApi.byShift({ profile_id: profileId, from, to }),
    });

    // Trasformazione: { shift_id → { shift_name, byDate: {date → row} } }.
    const { shifts, dates } = useMemo(() => {
        if (!data || data.length === 0) return { shifts: [], dates: [] };
        const shiftMap = new Map<number, { id: number; name: string; byDate: Map<string, OEEShiftRow> }>();
        const dateSet = new Set<string>();
        for (const r of data) {
            dateSet.add(r.date);
            if (!shiftMap.has(r.shift_id)) {
                shiftMap.set(r.shift_id, { id: r.shift_id, name: r.shift_name, byDate: new Map() });
            }
            shiftMap.get(r.shift_id)!.byDate.set(r.date, r);
        }
        return {
            shifts: Array.from(shiftMap.values()).sort((a, b) => a.id - b.id),
            // Date ordinate dalla più recente alla più vecchia (visivo).
            dates: Array.from(dateSet).sort().reverse(),
        };
    }, [data]);

    return (
        <Card>
            <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
                <CardTitle className="text-sm font-semibold text-muted-foreground">
                    OEE per turno × giorno
                </CardTitle>
                <div className="flex items-center gap-1">
                    {(Object.keys(RANGE_CFG) as Range[]).map((r) => (
                        <Button
                            key={r}
                            size="sm"
                            variant={r === range ? 'default' : 'outline'}
                            onClick={() => setRange(r)}
                            className="h-7 px-2 text-xs"
                        >
                            {RANGE_CFG[r].label}
                        </Button>
                    ))}
                </div>
            </CardHeader>
            <CardContent>
                {isLoading ? (
                    <div className="flex items-center justify-center h-48 text-muted-foreground">
                        <Loader2 className="animate-spin" /> &nbsp; Caricamento…
                    </div>
                ) : isError ? (
                    <div className="h-48 flex items-center justify-center text-sm text-red-500">
                        Errore nel caricamento della matrice.
                    </div>
                ) : shifts.length === 0 ? (
                    <div className="py-12 text-center text-sm text-muted-foreground">
                        Nessun dato per turno nel periodo. Verifica:
                        <ul className="text-xs mt-2 inline-block text-left">
                            <li>• I turni sono configurati e attivi (Sidebar → Turni)</li>
                            <li>• Il cron worker ha girato almeno 1 ora dopo l'attivazione turni</li>
                            <li>• Il profilo ha <code>respect_shifts</code> abilitato</li>
                        </ul>
                    </div>
                ) : (
                    <div className="overflow-x-auto">
                        <table className="w-full text-sm border-collapse">
                            <thead>
                                <tr>
                                    <th className="text-left text-xs font-semibold text-muted-foreground p-2 border-b">Turno</th>
                                    {dates.map((d) => (
                                        <th key={d} className="text-center text-xs font-medium text-muted-foreground p-2 border-b min-w-[60px]">
                                            {formatDayLabel(d)}
                                        </th>
                                    ))}
                                </tr>
                            </thead>
                            <tbody>
                                {shifts.map((s) => (
                                    <tr key={s.id}>
                                        <td className="p-2 text-xs font-semibold border-b">
                                            {s.name}
                                        </td>
                                        {dates.map((d) => {
                                            const row = s.byDate.get(d);
                                            const v = row?.oee ?? 0;
                                            return (
                                                <td
                                                    key={d}
                                                    className={`p-1 border-b text-center`}
                                                    title={row
                                                        ? `OEE ${v.toFixed(1)}% — A ${row.availability.toFixed(0)}% P ${row.performance.toFixed(0)}% Q ${row.quality.toFixed(0)}% (${row.hours}h)`
                                                        : '—'}
                                                >
                                                    {row ? (
                                                        <div className={`rounded border px-2 py-1.5 ${bandColor(v)}`}>
                                                            <span className="font-mono font-bold text-sm">{v.toFixed(0)}</span>
                                                        </div>
                                                    ) : (
                                                        <span className="text-muted-foreground">—</span>
                                                    )}
                                                </td>
                                            );
                                        })}
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                        <p className="text-[11px] text-muted-foreground mt-3">
                            Verde ≥85 (world-class), Ambra 65-85 (tipico), Arancio 40-65, Rosso &lt;40.
                            Hover su una cella per vedere A/P/Q.
                        </p>
                    </div>
                )}
            </CardContent>
        </Card>
    );
};
