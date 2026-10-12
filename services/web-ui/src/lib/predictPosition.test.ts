import { describe, expect, it } from 'vitest';
import { predictPositions } from './predictPosition';

describe('predictPositions', () => {
    it('puts a gateway with no position at the centre of its area', () => {
        const p = predictPositions([{ id: 1, area_id: 5 }], new Map([[5, [16.5, 39.3]]]));
        expect(p.get(1)).toEqual({ longitude: 16.5, latitude: 39.3 });
    });

    it('without a boundary, near the placed gateways of the same area, spread apart', () => {
        const p = predictPositions([
            { id: 1, area_id: 5, latitude: 39.30, longitude: 16.50 },
            { id: 2, area_id: 5, latitude: 39.32, longitude: 16.52 },
            { id: 3, area_id: 5 },
            { id: 4, area_id: 5 },
        ], new Map());
        const a = p.get(3)!, b = p.get(4)!;
        expect(Math.abs(a.latitude - 39.31)).toBeLessThan(0.01);
        expect(Math.abs(a.longitude - 16.51)).toBeLessThan(0.01);
        expect(a).not.toEqual(b);
        expect(p.has(1)).toBe(false); // placed ones keep their own position
    });

    it('leaves a gateway off the map when nothing says where its area is', () => {
        expect(predictPositions([{ id: 9, area_id: 7 }], new Map()).size).toBe(0);
    });
});
