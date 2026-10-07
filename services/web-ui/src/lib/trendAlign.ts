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
    // The envelope of bucketed points, aligned the same way; a pen whose
    // points are samples has none.
    const bands = pens.map(pen => {
        const lo = new Map<number, number>(), hi = new Map<number, number>();
        for (const p of batch[pen.tagId] ?? []) {
            if (p.min === undefined || p.max === undefined || p.quality !== 0) continue;
            const t = Math.round(p.timestamp / 1000);
            lo.set(t, p.min); hi.set(t, p.max);
        }
        if (lo.size === 0) return null;
        return { min: timestamps.map(t => lo.get(t)), max: timestamps.map(t => hi.get(t)) };
    });
    return { timestamps, values, bands };
}

/**
 * Merges history and live points per tag: one sample per instant (a later
 * source wins), sorted, and nothing older than `fromMs`. Live mode keeps the
 * window's history, a periodically refetched tail and the values pushed over
 * the socket; they overlap, and a duplicate instant would draw a zero-length
 * spike in the gap logic.
 */
export function mergeSeries(
    tagIds: number[],
    sources: (Record<number, TrendDataPoint[]> | undefined)[],
    fromMs = -Infinity,
): Record<number, TrendDataPoint[]> {
    const out: Record<number, TrendDataPoint[]> = {};
    for (const id of tagIds) {
        const byTs = new Map<number, TrendDataPoint>();
        for (const src of sources) {
            for (const p of src?.[id] ?? []) {
                if (p.timestamp >= fromMs) byTs.set(p.timestamp, p);
            }
        }
        out[id] = [...byTs.values()].sort((a, b) => a.timestamp - b.timestamp);
    }
    return out;
}

/**
 * A value pushed over the realtime socket, as a history point: booleans as
 * 0/1, timestamps in ms (a seconds timestamp is converted), quality 0 for good
 * (the socket carries 0 or 192 for good, history 0).
 */
export function realtimePoint(v: { value: unknown; timestamp: number; quality: number }): TrendDataPoint | null {
    const n = typeof v.value === 'boolean' ? (v.value ? 1 : 0) : Number(v.value);
    if (v.value === null || v.value === undefined || !Number.isFinite(n)) return null;
    const ts = v.timestamp < 1e11 ? v.timestamp * 1000 : v.timestamp;
    const good = v.quality === 0 || v.quality === 192;
    return { timestamp: ts, value: n, quality: good ? 0 : 1 };
}

/**
 * A pen's value at an instant, as the chart shows it: the newest good sample
 * at or before `atMs` (a historian holds a value until the next one), or null
 * when there is none or the newest one before it is bad.
 */
export function valueAt(points: TrendDataPoint[] | undefined, atMs: number): { value: number; at: number } | null {
    if (!points?.length) return null;
    let lo = 0, hi = points.length - 1, found = -1;
    while (lo <= hi) {
        const mid = (lo + hi) >> 1;
        if (points[mid].timestamp <= atMs) { found = mid; lo = mid + 1; } else hi = mid - 1;
    }
    if (found < 0) return null;
    const p = points[found];
    return p.quality === 0 && p.value !== null ? { value: p.value, at: p.timestamp } : null;
}

/** The same points `offsetMs` later: a previous period laid over the current one. */
export function shiftPoints(points: TrendDataPoint[] | undefined, offsetMs: number): TrendDataPoint[] {
    return (points ?? []).map(p => ({ ...p, timestamp: p.timestamp + offsetMs }));
}
