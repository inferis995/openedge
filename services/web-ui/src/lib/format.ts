// Display formatting for live industrial values: short, human, consistent.
// Raw floats (e.g. -6.240328788757324) and raw second counts (12431s) must
// never reach the UI — every view goes through here.

export function formatLiveValue(value: unknown, decimals?: number): string {
    if (value === null || value === undefined) return '—';
    if (typeof value !== 'number' || !Number.isFinite(value)) return String(value);
    if (decimals !== undefined) return value.toFixed(decimals);
    // Default: at most 2 decimals, trailing zeros trimmed ("6.20" → "6.2").
    return value.toFixed(2).replace(/\.?0+$/, '');
}

export type AgoUnit = 'now' | 's' | 'm' | 'h' | 'd';

export function agoParts(agoSec: number): { n: number; unit: AgoUnit } {
    if (agoSec < 10) return { n: 0, unit: 'now' };
    if (agoSec < 60) return { n: agoSec, unit: 's' };
    const m = Math.floor(agoSec / 60);
    if (m < 60) return { n: m, unit: 'm' };
    const h = Math.floor(m / 60);
    if (h < 48) return { n: h, unit: 'h' };
    return { n: Math.floor(h / 24), unit: 'd' };
}
