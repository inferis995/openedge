import { describe, expect, it } from 'vitest';
import { alignPens } from './trendAlign';

const min = 60_000;
const pt = (t: number, value: number, quality = 0) => ({ timestamp: t * min, value, quality });

describe('alignPens', () => {
    it('joins across another pen\'s instants (undefined), and keeps bad quality as a gap (null)', () => {
        const { timestamps, values } = alignPens([{ tagId: 1 }, { tagId: 2 }], {
            1: [pt(0, 1), pt(15, 2), pt(30, 3, 192)],
            2: [pt(5, 10)],
        });
        expect(timestamps).toEqual([0, 5, 15, 30].map((m) => m * 60));
        expect(values[0]).toEqual([1, undefined, 2, null]);
        expect(values[1]).toEqual([undefined, 10, undefined, undefined]);
    });

    it('breaks the line over a stretch far longer than the pen\'s usual spacing, other pens included', () => {
        const pen1 = [0, 15, 30, 45, 60, 180, 195, 210].map((m, i) => pt(m, i));
        const { timestamps, values } = alignPens([{ tagId: 1 }, { tagId: 2 }], {
            1: pen1,
            2: [pt(120, 5)], // another pen sampled inside the hole
        });
        const at = (m: number) => values[0][timestamps.indexOf(m * 60)];
        expect(at(60)).toBe(4);
        expect(at(120)).toBeNull(); // inside the hole: not "no sample here"
        expect(at(180)).toBe(5);
        const holeCells = timestamps.filter((t) => t > 60 * 60 && t < 180 * 60).map((t) => values[0][timestamps.indexOf(t)]);
        expect(holeCells.every((v) => v === null)).toBe(true);
    });

    it('a logger with one or two samples is left alone', () => {
        const { values } = alignPens([{ tagId: 1 }], { 1: [pt(0, 3.6), pt(360, 3.5)] });
        expect(values[0]).toEqual([3.6, 3.5]);
    });
});
