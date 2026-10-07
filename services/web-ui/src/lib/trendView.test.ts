import { describe, expect, it } from 'vitest';
import { decodeView, encodeView, resolveRange, type TrendView } from './trendView';

describe('trend view in the address', () => {
    it('round-trips pens, colours, hidden pens, range, aggregation and live', () => {
        const v: TrendView = {
            pens: [{ tagId: 7, color: '#3b82f6', visible: true }, { tagId: 9, color: '#ef4444', visible: false }],
            range: { preset: '24h' }, agg: 'max', live: true, cmp: '7d',
            y: { bar: 'eu', 'l/s': { min: 0, max: 50 } },
        };
        const q = encodeView(v);
        expect(q.toString()).toBe('pens=7%3A3b82f6%2C9%3Aef4444%3Ah&range=24h&agg=max&live=1&cmp=7d&y=bar%3Aeu%2Cl%252Fs%3A0%7E50');
        expect(decodeView(new URLSearchParams(q.toString()))).toEqual(v);
    });

    it('keeps a picked window as absolute times, and never live', () => {
        const v = decodeView(new URLSearchParams('pens=3:00ff00&from=2026-10-01T00:00:00.000Z&to=2026-10-02T00:00:00.000Z&live=1'));
        expect(v?.range).toEqual({ from: Date.parse('2026-10-01T00:00:00Z'), to: Date.parse('2026-10-02T00:00:00Z') });
        expect(v?.live).toBe(false);
        expect(v?.cmp).toBe('off');
    });

    it('ignores what it cannot read rather than failing the page', () => {
        const v = decodeView(new URLSearchParams('pens=abc,5:zzz,5:112233,-1&range=99y&agg=evil'));
        expect(v).toEqual({ pens: [{ tagId: 5, color: '#3b82f6', visible: true }], range: { preset: '1h' }, agg: 'mean', live: false, cmp: 'off', y: {} });
        expect(decodeView(new URLSearchParams('foo=1'))).toBeNull();
    });

    it('re-anchors a preset to now', () => {
        const now = new Date('2026-10-07T12:00:00Z');
        const r = resolveRange({ preset: '4h' }, now);
        expect(r.to).toEqual(now);
        expect(now.getTime() - r.from.getTime()).toBe(4 * 3600e3);
    });
});
