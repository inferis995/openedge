import type { TagForecast } from '@/api/forecast';
import type { TrendDataPoint } from '@/types/trend';

/**
 * A forecast is drawn as a pen of its own. Its id is the tag's plus this
 * offset, the way a comparison pen carries the tag's id negated, so that the
 * chart's alignment and colours need nothing new.
 */
export const FORECAST_ID_OFFSET = 1_000_000_000;

export const forecastPenId = (tagId: number) => tagId + FORECAST_ID_OFFSET;

/** The tag a pen draws: itself, the one it compares, or the one it forecasts. */
export function baseTagId(penId: number): number {
    if (penId < 0) return -penId;
    if (penId >= FORECAST_ID_OFFSET) return penId - FORECAST_ID_OFFSET;
    return penId;
}

export const isForecastPen = (penId: number) => penId >= FORECAST_ID_OFFSET;

/**
 * Only a forecast that beat repeating the last value on the known history is
 * drawn. One that did not is reported, never passed off as a good one.
 */
export const isDrawable = (fc: TagForecast | undefined): fc is TagForecast =>
    !!fc && fc.status === 'ok' && fc.points.some(p => p.q50 !== null);

/**
 * The forecast as chart points: the median with the P10–P90 band as the
 * envelope. It starts from the last real value, so the dashed line continues
 * the curve instead of floating next to it.
 */
export function forecastPoints(fc: TagForecast, last?: { timestamp: number; value: number | null } | null): TrendDataPoint[] {
    const out: TrendDataPoint[] = [];
    const first = fc.points.length ? Date.parse(fc.points[0].t) : NaN;
    if (last && last.value !== null && Number.isFinite(first) && last.timestamp < first) {
        out.push({ timestamp: last.timestamp, value: last.value, quality: 0, min: last.value, max: last.value });
    }
    for (const p of fc.points) {
        const t = Date.parse(p.t);
        if (!Number.isFinite(t) || p.q50 === null) continue;
        out.push({ timestamp: t, value: p.q50, quality: 0, min: p.q10 ?? p.q50, max: p.q90 ?? p.q50 });
    }
    return out;
}

/** Where the furthest drawable forecast ends, in ms (0: none). */
export function forecastEnd(fcs: (TagForecast | undefined)[]): number {
    let end = 0;
    for (const fc of fcs) {
        if (!isDrawable(fc)) continue;
        for (const p of fc.points) {
            const t = Date.parse(p.t);
            if (p.q50 !== null && t > end) end = t;
        }
    }
    return end;
}

/** The newest good reading of a series, for forecastPoints. */
export function lastReading(pts: TrendDataPoint[] | undefined): { timestamp: number; value: number } | null {
    if (!pts) return null;
    for (let i = pts.length - 1; i >= 0; i--) {
        const p = pts[i];
        if (p.quality === 0 && p.value !== null && p.source !== 'seed') return { timestamp: p.timestamp, value: p.value };
    }
    return null;
}
