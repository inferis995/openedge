import type { ForecastKind, ForecastScope, ForecastStatus } from '@/api/forecast';

/** Shared by the forecast page, the tag's Forecast tab and the settings form. */
export const FORECAST_KINDS: ForecastKind[] = ['auto', 'process', 'cyclic', 'degradation', 'counter', 'state'];
export const MAX_HORIZON_HOURS = 24 * 30;

export const FORECAST_STATUS_VARIANT: Record<ForecastStatus, 'success' | 'warning' | 'muted' | 'alarm'> = {
    ok: 'success', unreliable: 'warning', learning: 'muted', disabled: 'muted', error: 'alarm', pending: 'muted',
};

export const forecastProfileKey = (scope: ForecastScope, id: number) => ['forecast-profile', scope, id];
export const tagForecastKey = (tagId: number) => ['tag-forecast', tagId];
