import type { ForecastKind, ForecastRisk, ForecastScope, ForecastStatus, RiskLikelihood } from '@/api/forecast';

/** Shared by the forecast page, the tag's Forecast tab and the settings form. */
export const FORECAST_KINDS: ForecastKind[] = ['auto', 'process', 'cyclic', 'degradation', 'counter', 'state'];
export const MAX_HORIZON_HOURS = 24 * 30;

export const FORECAST_STATUS_VARIANT: Record<ForecastStatus, 'success' | 'warning' | 'muted' | 'alarm'> = {
    ok: 'success', unreliable: 'warning', learning: 'muted', disabled: 'muted', error: 'alarm', pending: 'muted',
};

export const forecastProfileKey = (scope: ForecastScope, id: number) => ['forecast-profile', scope, id];
export const tagForecastKey = (tagId: number) => ['tag-forecast', tagId];


export const RISK_VARIANT: Record<RiskLikelihood, 'alarm' | 'warning' | 'muted' | 'info'> = {
    almost_certain: 'alarm', likely: 'warning', possible: 'muted', trend: 'info',
};

/** A rule likely or almost certain to trip within the horizon. */
export const isSerious = (r: ForecastRisk) => r.likelihood === 'likely' || r.likelihood === 'almost_certain';

export const HEALTH_TONE: Record<'good' | 'watch' | 'critical' | 'unknown', { bar: string; text: string }> = {
    good: { bar: 'bg-emerald-500', text: 'text-emerald-600 dark:text-emerald-400' },
    watch: { bar: 'bg-amber-500', text: 'text-amber-600 dark:text-amber-400' },
    critical: { bar: 'bg-red-500', text: 'text-red-600 dark:text-red-400' },
    unknown: { bar: 'bg-muted-foreground/40', text: 'text-muted-foreground' },
};
