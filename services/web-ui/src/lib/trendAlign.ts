import type { TrendDataPoint } from '@/types/trend';

/**
 * Aligns every pen on one time axis for uPlot.
 *
 * Where a pen has no sample at an instant another pen has, the cell is
 * undefined and the line joins across it. Bad quality is null and breaks the
 * line; so does a stretch without data much longer than the pen's usual
 * spacing (an outage, a logger that stopped) — joining it drew a straight
 * line through hours nobody measured.
 */
export function alignPens(pens: { tagId: number }[], batch: Record<number, TrendDataPoint[]>) {
    const tsSet = new Set<number>();
    const maps = pens.map(pen => {
        const pts = [...(batch[pen.tagId] ?? [])].sort((a, b) => a.timestamp - b.timestamp);
        const m = new Map<number, number | null>();
        const holes: [number, number][] = [];
        const gaps: number[] = [];
        for (let i = 1; i < pts.length; i++) gaps.push(pts[i].timestamp - pts[i - 1].timestamp);
        const median = gaps.length >= 2 ? [...gaps].sort((a, b) => a - b)[Math.floor(gaps.length / 2)] : Infinity;
        for (let i = 0; i < pts.length; i++) {
            const p = pts[i];
            const tsSec = Math.round(p.timestamp / 1000);
            if (i > 0 && p.timestamp - pts[i - 1].timestamp > 3 * median) {
                // uPlot breaks a line only over a run of nulls with nothing
                // else between them; other pens' instants inside the hole
                // would read "no sample here" and join it again. So the hole
                // is remembered and every cell inside it becomes null.
                const a = Math.round(pts[i - 1].timestamp / 1000) + 1, b = tsSec - 1;
                holes.push([a, b]);
                m.set(a, null); m.set(b, null);
                tsSet.add(a); tsSet.add(b);
            }
            m.set(tsSec, p.quality === 0 ? p.value : null);
            tsSet.add(tsSec);
        }
        return { m, holes };
    });
    const timestamps = Array.from(tsSet).sort((a, b) => a - b);
    const values = maps.map(({ m, holes }) => timestamps.map(t => {
        if (m.has(t)) return m.get(t);
        return holes.some(([a, b]) => t > a && t < b) ? null : undefined;
    }));
    return { timestamps, values };
}
