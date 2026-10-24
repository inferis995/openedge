import { useTranslation } from 'react-i18next';
import { AlertTriangle, Activity } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import type { ForecastAnomaly, ForecastRisk } from '@/api/forecast';
import { formatLiveValue } from '@/lib/format';
import { RISK_VARIANT } from '@/lib/forecastUi';

function when(at: string, lang: string) {
    const d = new Date(at);
    return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString(lang, { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
}

/** One line per alarm rule the forecast may trip: which limit, how likely, when. */
export function ForecastRiskList({ risks, unit, compact = false }: { risks: ForecastRisk[]; unit?: string; compact?: boolean }) {
    const { t, i18n } = useTranslation();
    if (risks.length === 0) return null;
    return (
        <ul className="space-y-1.5">
            {risks.map(r => (
                <li key={r.rule_id} className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
                    <Badge variant={RISK_VARIANT[r.likelihood]}>{t(`forecast.risk_${r.likelihood}`)}</Badge>
                    <span>
                        {t(`forecast.limit_${r.type}`)}
                        {r.type.startsWith('bool') ? '' : ` ${formatLiveValue(r.threshold, undefined, i18n.language)}${unit ? ` ${unit}` : ''}`}
                    </span>
                    <span className="text-muted-foreground">
                        · {t(r.extrapolated ? 'forecast.risk_at_trend' : 'forecast.risk_at', { when: when(r.at, i18n.language) })}
                    </span>
                    {!compact && r.message && <span className="w-full text-xs text-muted-foreground">{r.message}</span>}
                </li>
            ))}
        </ul>
    );
}

/** Whether the values that arrived stayed inside what was forecast. */
export function ForecastAnomalyNote({ anomaly, unit }: { anomaly?: ForecastAnomaly; unit?: string }) {
    const { t, i18n } = useTranslation();
    if (!anomaly) return null;
    const fmt = (v?: number) => (v === undefined ? '—' : formatLiveValue(v, undefined, i18n.language));
    if (anomaly.outside === 0) {
        return (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
                <Activity className="size-4" /> {t('forecast.anomaly_none', { count: anomaly.checked })}
            </p>
        );
    }
    return (
        <p className="flex items-start gap-2 text-sm text-amber-700 dark:text-amber-400">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
            <span>
                {t('forecast.anomaly_found', { outside: anomaly.outside, checked: anomaly.checked })}{' '}
                {anomaly.at && t('forecast.anomaly_last', {
                    when: when(anomaly.at, i18n.language),
                    value: `${fmt(anomaly.value)}${unit ? ` ${unit}` : ''}`,
                    low: fmt(anomaly.low), high: fmt(anomaly.high),
                })}
            </span>
        </p>
    );
}
