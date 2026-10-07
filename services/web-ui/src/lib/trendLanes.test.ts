import { describe, expect, it } from 'vitest';
import { laneRange, LANE_PAD } from './trendLanes';

// Where a value lands, as a fraction of the plot height from the bottom.
const at = (v: number, [min, max]: [number, number]) => (v - min) / (max - min);

describe('laneRange', () => {
    it('puts each axis in its own band, the first on top', () => {
        const n = 3;
        const lanes = [0, 1, 2].map(i => laneRange(8.7, 8.8, i, n));
        expect(at(8.7, lanes[0])).toBeCloseTo(2 / 3 + LANE_PAD / 3);
        expect(at(8.8, lanes[0])).toBeCloseTo(1 - LANE_PAD / 3);
        expect(at(8.7, lanes[2])).toBeCloseTo(LANE_PAD / 3);
        expect(at(8.8, lanes[2])).toBeCloseTo(1 / 3 - LANE_PAD / 3);
    });

    it('keeps flat curves of different axes apart', () => {
        // Three levels that did not move: centred in their lanes, not on each other.
        const ys = [[8.7, 8.8], [3.6, 3.76], [4.2, 4.43]].map(([lo, hi], i) => at((lo + hi) / 2, laneRange(lo, hi, i, 3)));
        expect(ys[0]).toBeCloseTo(5 / 6);
        expect(ys[1]).toBeCloseTo(1 / 2);
        expect(ys[2]).toBeCloseTo(1 / 6);
    });
});
