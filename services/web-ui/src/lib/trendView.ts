import { endOfDay, startOfDay, subDays } from 'date-fns';
import type { AggregationType } from '@/types/trend';

/**
 * What a trend shows, as it is kept in the page address (?pens=…&range=…)
 * and, for the next visit from the menu, in localStorage. Reloading the page
 * used to bring back an empty chart; the address is also a link to the view.
 */
export interface TrendView {
    pens: { tagId: number; color: string; visible: boolean }[];
    /** A preset ("1h", "today"…) is re-anchored to now when the view is opened. */
    range: { preset: string } | { from: number; to: number };
    agg: AggregationType;
    live: boolean;
    /** A previous period laid over the window: the one before it, a day or a week earlier. */
    cmp?: CompareMode;
    /** Per axis (keyed by unit, or #tagId for a pen without one): the tag's range, or fixed bounds. */
    y?: Record<string, YAxisMode>;
    /** Curves of different axes in lanes, one under the other (the default), or overlaid. */
    layout?: TrendLayout;
    /** Forecasts drawn after now. */
    fc?: boolean;
}

export type TrendLayout = 'lanes' | 'overlay';

export type YAxisMode = 'eu' | { min: number; max: number };

export type CompareMode = 'off' | 'prev' | '1d' | '7d';
const CMPS: CompareMode[] = ['prev', '1d', '7d'];

/** How far back a comparison looks, for a window of `spanMs`. */
export function compareOffset(mode: CompareMode | undefined, spanMs: number): number {
    if (mode === 'prev') return spanMs;
    if (mode === '1d') return 86400e3;
    if (mode === '7d') return 7 * 86400e3;
    return 0;
}

export const TREND_VIEW_KEY = 'openedge.trend.view';
const PRESETS: Record<string, number> = {
    '1h': 3600e3, '4h': 4 * 3600e3, '8h': 8 * 3600e3, '12h': 12 * 3600e3,
    '24h': 24 * 3600e3, '3d': 3 * 86400e3, '7d': 7 * 86400e3, '30d': 30 * 86400e3,
};
// No "first": the history aggregates keep avg, min, max and last, and the
// server has always refused it.
const AGGS: AggregationType[] = ['mean', 'max', 'min', 'last'];

export function encodeView(v: TrendView): URLSearchParams {
    const q = new URLSearchParams();
    if (v.pens.length) {
        q.set('pens', v.pens.map(p => `${p.tagId}:${p.color.replace('#', '')}${p.visible ? '' : ':h'}`).join(','));
    }
    if ('preset' in v.range) q.set('range', v.range.preset);
    else { q.set('from', new Date(v.range.from).toISOString()); q.set('to', new Date(v.range.to).toISOString()); }
    if (v.agg !== 'mean') q.set('agg', v.agg);
    if (v.live) q.set('live', '1');
    if (v.cmp && v.cmp !== 'off') q.set('cmp', v.cmp);
    if (v.fc) q.set('fc', '1');
    const y = Object.entries(v.y ?? {}).map(([k, m]) =>
        `${encodeURIComponent(k)}:${m === 'eu' ? 'eu' : `${m.min}~${m.max}`}`);
    if (y.length) q.set('y', y.join(','));
    if (v.layout === 'overlay') q.set('lay', 'ov');
    return q;
}

/** null when the address says nothing about a trend (then the saved view is used). */
export function decodeView(q: URLSearchParams): TrendView | null {
    if (!q.has('pens') && !q.has('range') && !q.has('from')) return null;
    const pens: TrendView['pens'] = [];
    const seen = new Set<number>();
    for (const part of (q.get('pens') ?? '').split(',')) {
        const [id, color, flag] = part.split(':');
        const tagId = Number(id);
        if (!Number.isInteger(tagId) || tagId <= 0 || seen.has(tagId)) continue;
        seen.add(tagId);
        pens.push({ tagId, color: /^[0-9a-f]{6}$/i.test(color ?? '') ? `#${color}` : '#3b82f6', visible: flag !== 'h' });
    }
    let range: TrendView['range'] = { preset: '1h' };
    const preset = q.get('range');
    const from = Date.parse(q.get('from') ?? ''), to = Date.parse(q.get('to') ?? '');
    if (preset && (preset in PRESETS || preset === 'today' || preset === 'yesterday')) range = { preset };
    else if (Number.isFinite(from) && Number.isFinite(to) && to > from) range = { from, to };
    const agg = AGGS.includes(q.get('agg') as AggregationType) ? (q.get('agg') as AggregationType) : 'mean';
    const cmp = CMPS.includes(q.get('cmp') as CompareMode) ? (q.get('cmp') as CompareMode) : 'off';
    const y: Record<string, YAxisMode> = {};
    for (const part of (q.get('y') ?? '').split(',')) {
        const i = part.lastIndexOf(':');
        if (i <= 0) continue;
        let key: string;
        try { key = decodeURIComponent(part.slice(0, i)); } catch { continue; }
        const m = part.slice(i + 1);
        if (m === 'eu') { y[key] = 'eu'; continue; }
        const [lo, hi] = m.split('~').map(Number);
        if (Number.isFinite(lo) && Number.isFinite(hi) && hi > lo) y[key] = { min: lo, max: hi };
    }
    return { pens: pens.slice(0, 30), range, agg, live: q.get('live') === '1' && 'preset' in range, cmp, y, layout: q.get('lay') === 'ov' ? 'overlay' : 'lanes', ...(q.get('fc') === '1' ? { fc: true } : {}) };
}

/** The concrete window of a view's range, now. */
export function resolveRange(r: TrendView['range'], now = new Date()): { from: Date; to: Date; preset: string } {
    if (!('preset' in r)) return { from: new Date(r.from), to: new Date(r.to), preset: 'custom' };
    if (r.preset === 'today') return { from: startOfDay(now), to: now, preset: 'today' };
    if (r.preset === 'yesterday') {
        const d = subDays(now, 1);
        return { from: startOfDay(d), to: endOfDay(d), preset: 'yesterday' };
    }
    return { from: new Date(now.getTime() - (PRESETS[r.preset] ?? 3600e3)), to: now, preset: r.preset in PRESETS ? r.preset : '1h' };
}
