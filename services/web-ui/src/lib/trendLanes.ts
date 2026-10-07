/** Fraction of a lane left empty above and below its curves. */
export const LANE_PAD = 0.08;

/**
 * The scale range that puts [lo, hi] in lane `i` of `n` (0 = top): each
 * axis keeps its own values and is drawn only where its lane is.
 */
export function laneRange(lo: number, hi: number, i: number, n: number): [number, number] {
    const bottom = (n - 1 - i) / n + LANE_PAD / n;
    const top = (n - i) / n - LANE_PAD / n;
    const span = (hi - lo) / (top - bottom);
    const min = lo - bottom * span;
    return [min, min + span];
}
