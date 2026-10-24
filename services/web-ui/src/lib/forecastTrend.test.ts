import { describe, expect, it } from 'vitest';
import type { TagForecast } from '@/api/forecast';
import { baseTagId, forecastEnd, forecastPenId, forecastPoints, isDrawable, isForecastPen, lastReading } from './forecastTrend';

const fc = (status: TagForecast['status'], q50: (number | null)[]): TagForecast => ({
    tag_id: 7,
    status,
    points: q50.map((v, i) => ({ t: new Date(Date.UTC(2026, 9, 22, 12 + i)).toISOString(), q10: v === null ? null : v - 1, q50: v, q90: v === null ? null : v + 1 })),
    settings: { enabled: true, kind: 'auto', horizon_hours: 24, context_days: 28 },
    risks: [],
});

describe('forecast pens', () => {
    it('map to and from the tag', () => {
        expect(baseTagId(forecastPenId(7))).toBe(7);
        expect(baseTagId(-7)).toBe(7);
        expect(baseTagId(7)).toBe(7);
        expect(isForecastPen(forecastPenId(7))).toBe(true);
        expect(isForecastPen(-7)).toBe(false);
    });

    it('are drawn only when the forecast is reliable', () => {
        expect(isDrawable(fc('ok', [1, 2]))).toBe(true);
        expect(isDrawable(fc('unreliable', [1, 2]))).toBe(false);
        expect(isDrawable(fc('ok', [null]))).toBe(false);
        expect(isDrawable(undefined)).toBe(false);
    });
});

describe('forecastPoints', () => {
    it('starts from the last real value and carries the band', () => {
        const last = { timestamp: Date.UTC(2026, 9, 22, 11, 40), value: 5 };
        const pts = forecastPoints(fc('ok', [6, null, 8]), last);
        expect(pts.map(p => p.value)).toEqual([5, 6, 8]);
        expect(pts[1]).toMatchObject({ min: 5, max: 7, quality: 0 });
        expect(pts[0]).toMatchObject({ min: 5, max: 5 });
    });

    it('does not anchor on a value newer than the forecast', () => {
        const late = { timestamp: Date.UTC(2026, 9, 22, 13), value: 5 };
        expect(forecastPoints(fc('ok', [6]), late).map(p => p.value)).toEqual([6]);
    });
});

describe('forecastEnd', () => {
    it('is the end of the furthest drawable forecast', () => {
        expect(forecastEnd([fc('ok', [1, 2, null]), fc('unreliable', [1, 2, 3, 4, 5])])).toBe(Date.UTC(2026, 9, 22, 13));
        expect(forecastEnd([undefined])).toBe(0);
    });
});

describe('lastReading', () => {
    it('skips seeds and bad quality', () => {
        expect(lastReading([
            { timestamp: 1, value: 1, quality: 0 },
            { timestamp: 2, value: 2, quality: 0, source: 'seed' },
            { timestamp: 3, value: 3, quality: 192 },
        ])).toEqual({ timestamp: 1, value: 1 });
        expect(lastReading(undefined)).toBeNull();
    });
});
