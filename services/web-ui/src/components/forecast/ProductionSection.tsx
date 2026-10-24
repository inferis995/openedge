import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Gauge, Search } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { oeeForecastApi, type Band, type ForecastOverviewTag, type OEEForecast, type PeriodTotal } from '@/api/forecast';
import { PeriodName, PeriodValue } from '@/components/forecast/PeriodTotals';
import { formatLiveValue } from '@/lib/format';
import { useNavigationStore } from '@/stores/useNavigationStore';

const PERIODS: PeriodTotal['period'][] = ['shift', 'day', 'month'];

/**
 * Production: where the counters and the OEE will be at the end of the shift,
 * the day and the month — measured so far, forecast for the rest.
 */
export function ProductionSection({ tags }: { tags: ForecastOverviewTag[] }) {
    const { t } = useTranslation();
    const { selectedOrgId } = useNavigationStore();
    const { data: oee, isLoading } = useQuery({
        queryKey: ['forecast-oee', selectedOrgId],
        queryFn: () => oeeForecastApi.list(),
        refetchInterval: 60_000,
    });
    const [query, setQuery] = useState('');
    const totals = useMemo(() => {
        const q = query.trim().toLowerCase();
        return tags.filter(tg => (tg.periods?.length ?? 0) > 0 && (!q ||
            [tg.alias, tg.gateway_name, tg.area_name, tg.site_name].some(s => s.toLowerCase().includes(q))));
    }, [tags, query]);
    const anyTotals = tags.some(tg => (tg.periods?.length ?? 0) > 0);
    const hasShift = totals.some(tg => tg.periods?.some(p => p.period === 'shift'));
    const cols = PERIODS.filter(p => p !== 'shift' || hasShift);

    return (
        <div className="space-y-8">
            <section className="space-y-3">
                <div className="space-y-1">
                    <h2 className="text-base font-semibold">{t('forecast.oee_title')}</h2>
                    <p className="max-w-2xl text-sm text-muted-foreground">{t('forecast.oee_intro')}</p>
                </div>
                {isLoading && <p className="text-sm text-muted-foreground">{t('forecast.loading')}</p>}
                {!isLoading && (oee ?? []).length === 0 && (
                    <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
                        <Gauge className="mx-auto mb-3 size-8 opacity-40" />
                        {t('forecast.oee_empty')}{' '}
                        <Link to="/oee-profiles" className="font-medium text-foreground underline underline-offset-4">{t('forecast.oee_empty_link')}</Link>
                    </div>
                )}
                <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                    {(oee ?? []).map(o => <OEECard key={o.profile_id} o={o} />)}
                </div>
            </section>

            <section className="space-y-3">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
                    <div className="space-y-1">
                        <h2 className="text-base font-semibold">{t('forecast.totals_title')}</h2>
                        <p className="max-w-2xl text-sm text-muted-foreground">{t('forecast.totals_intro')}</p>
                    </div>
                    {anyTotals && (
                        <div className="relative sm:w-72">
                            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                            <Input value={query} onChange={e => setQuery(e.target.value)} placeholder={t('forecast.search')} className="pl-9" />
                        </div>
                    )}
                </div>
                {!anyTotals && (
                    <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
                        {t('forecast.totals_empty')}
                    </div>
                )}
                {totals.length > 0 && (
                    <>
                        <div className="hidden rounded-lg border md:block">
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>{t('forecast.col_tag')}</TableHead>
                                        {cols.map(p => <TableHead key={p}><PeriodName p={{ period: p }} /></TableHead>)}
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {totals.map(tg => (
                                        <TableRow key={tg.tag_id}>
                                            <TableCell className="align-top">
                                                <div className="font-medium">{tg.alias}</div>
                                                <div className="text-xs text-muted-foreground">{tg.site_name} · {tg.gateway_name}</div>
                                            </TableCell>
                                            {cols.map(c => {
                                                const p = tg.periods?.find(x => x.period === c);
                                                return (
                                                    <TableCell key={c} className="align-top">
                                                        {p ? <PeriodValue p={p} kind={tg.kind} unit={tg.unit} compact /> : '—'}
                                                    </TableCell>
                                                );
                                            })}
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        </div>
                        <div className="space-y-3 md:hidden">
                            {totals.map(tg => (
                                <div key={tg.tag_id} className="rounded-lg border bg-card p-4">
                                    <div className="font-medium">{tg.alias}</div>
                                    <div className="mb-3 text-xs text-muted-foreground">{tg.site_name} · {tg.gateway_name}</div>
                                    <dl className="space-y-2 text-sm">
                                        {(tg.periods ?? []).map(p => (
                                            <div key={p.period} className="flex items-start justify-between gap-3">
                                                <dt className="text-muted-foreground"><PeriodName p={p} /></dt>
                                                <dd className="text-right"><PeriodValue p={p} kind={tg.kind} unit={tg.unit} compact /></dd>
                                            </div>
                                        ))}
                                    </dl>
                                </div>
                            ))}
                        </div>
                    </>
                )}
            </section>
        </div>
    );
}

const pct = (x: number) => `${formatLiveValue(x, 0)}%`;

function OEECard({ o }: { o: OEEForecast }) {
    const { t, i18n } = useTranslation();
    const end = o.end ? new Date(o.end).toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' }) : '';
    const below = o.oee && o.target_oee > 0 && o.oee.q50 < o.target_oee;
    return (
        <div className="space-y-3 rounded-lg border bg-card p-4">
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                    <div className="truncate font-medium">{o.name}</div>
                    <div className="text-xs text-muted-foreground">
                        {o.period ? <>{o.period === 'shift' ? <PeriodName p={{ period: 'shift', name: o.period_name }} /> : t('forecast.period_day')} · {t('forecast.oee_until', { at: end })}</> : '—'}
                    </div>
                </div>
                {o.oee && (
                    <div className="shrink-0 text-right">
                        <div className={`text-2xl font-semibold tabular-nums ${below ? 'text-amber-600 dark:text-amber-400' : 'text-emerald-600 dark:text-emerald-400'}`}>
                            {pct(o.oee.q50)}
                        </div>
                        <div className="text-xs tabular-nums text-muted-foreground">{pct(o.oee.q10)}–{pct(o.oee.q90)}</div>
                    </div>
                )}
            </div>
            {o.oee ? (
                <>
                    {o.target_oee > 0 && (
                        <Badge variant={below ? 'warning' : 'success'} className="w-fit">
                            {t(below ? 'forecast.oee_below' : 'forecast.oee_on_target', { target: pct(o.target_oee) })}
                        </Badge>
                    )}
                    <div className="space-y-2">
                        <Factor label={t('forecast.oee_availability')} b={o.availability} missing={o.missing.includes('run_time')} />
                        <Factor label={t('forecast.oee_performance')} b={o.performance} />
                        <Factor label={t('forecast.oee_quality')} b={o.quality} missing={o.missing.includes('good')} />
                    </div>
                    {o.produced && (
                        <p className="text-xs text-muted-foreground">
                            {t('forecast.oee_pieces', { value: formatLiveValue(o.produced.q50, 0) })}
                            {o.good && <> · {t('forecast.oee_good', { value: formatLiveValue(o.good.q50, 0) })}</>}
                        </p>
                    )}
                    {o.extrapolated && <p className="text-xs text-muted-foreground">{t('forecast.period_extrapolated_hint')}</p>}
                </>
            ) : (
                <p className="flex items-start gap-2 text-sm text-muted-foreground">
                    <Gauge className="mt-0.5 size-4 shrink-0 opacity-60" />{t('forecast.oee_no_pieces')}
                </p>
            )}
        </div>
    );
}

function Factor({ label, b, missing }: { label: string; b?: Band; missing?: boolean }) {
    const { t } = useTranslation();
    if (!b) return null;
    return (
        <div className="space-y-1">
            <div className="flex items-baseline justify-between gap-2 text-xs">
                <span className="text-muted-foreground">{label}</span>
                <span className="tabular-nums">{missing ? <span className="text-muted-foreground">{t('forecast.oee_not_forecast')}</span> : pct(b.q50)}</span>
            </div>
            <div className="relative h-1.5 overflow-hidden rounded-full bg-muted">
                {!missing && <>
                    <div className="absolute inset-y-0 left-0 bg-primary/25" style={{ width: `${b.q90}%` }} />
                    <div className="absolute inset-y-0 left-0 bg-primary" style={{ width: `${b.q50}%` }} />
                </>}
            </div>
        </div>
    );
}
