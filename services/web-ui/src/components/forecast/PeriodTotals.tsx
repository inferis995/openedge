import { useTranslation } from 'react-i18next';
import type { ForecastKind, PeriodTotal } from '@/api/forecast';
import { formatLiveValue } from '@/lib/format';
import { totalUnit } from '@/lib/forecastUi';

/** The period's name: the shift's own, or "Today", "This month". */
export function PeriodName({ p }: { p: Pick<PeriodTotal, 'period' | 'name'> }) {
    const { t } = useTranslation();
    if (p.period === 'shift') return <>{p.name ? t('forecast.period_shift_named', { name: p.name }) : t('forecast.period_shift')}</>;
    return <>{t(`forecast.period_${p.period}`)}</>;
}

/** One total: the expected value at the period's end, its band, what is counted so far. */
export function PeriodValue({ p, kind, unit, compact }: { p: PeriodTotal; kind?: ForecastKind; unit?: string; compact?: boolean }) {
    const { t, i18n } = useTranslation();
    const u = totalUnit(kind, unit);
    const n = (x: number) => formatLiveValue(x, x >= 100 ? 0 : undefined);
    const end = new Date(p.end).toLocaleString(i18n.language, p.period === 'month'
        ? { day: 'numeric', month: 'short' } : { weekday: 'short', hour: '2-digit', minute: '2-digit' });
    return (
        <div className="min-w-0">
            <div className="font-medium tabular-nums">
                {n(p.q50)}{u && <span className="ml-1 text-xs font-normal text-muted-foreground">{u}</span>}
            </div>
            <div className="text-xs tabular-nums text-muted-foreground">
                {p.q10 !== p.q90 && <>{n(p.q10)}–{n(p.q90)} · </>}
                {t('forecast.period_so_far', { value: n(p.so_far) })}
                {!compact && <> · {t('forecast.period_until', { at: end })}</>}
                {p.extrapolated && <span title={t('forecast.period_extrapolated_hint')}> · {t('forecast.period_extrapolated')}</span>}
            </div>
        </div>
    );
}

/** A tag's totals, one row per period. */
export function PeriodTotalsList({ periods, kind, unit }: { periods: PeriodTotal[]; kind?: ForecastKind; unit?: string }) {
    return (
        <ul className="divide-y rounded-md border">
            {periods.map(p => (
                <li key={p.period + p.start} className="flex items-start justify-between gap-4 px-3 py-2 text-sm">
                    <span className="text-muted-foreground"><PeriodName p={p} /></span>
                    <span className="text-right"><PeriodValue p={p} kind={kind} unit={unit} /></span>
                </li>
            ))}
        </ul>
    );
}
