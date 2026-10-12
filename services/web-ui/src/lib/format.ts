// Display formatting for live industrial values: short, human, consistent.
// Raw floats (e.g. -6.240328788757324) and raw second counts (12431s) must
// never reach the UI — every view goes through here.

import i18n from '@/i18n';

export function formatLiveValue(value: unknown, decimals?: number, lang: string = i18n.language): string {
    if (value === null || value === undefined) return '—';
    if (typeof value !== 'number' || !Number.isFinite(value)) return String(value);
    // In the user's language (2,35 in Italian), the tag's decimals when set,
    // otherwise at most two with trailing zeros dropped.
    if (decimals !== undefined && decimals !== null && decimals >= 0 && decimals <= 10) {
        return value.toLocaleString(lang, { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
    }
    return value.toLocaleString(lang, { maximumFractionDigits: 2 });
}

/**
 * Short names for a device's tags: what is left once the part every one of
 * them shares is removed, cut at a separator — CARIATI_CA_P_1_AN1,
 * CARIATI_CA_P_1_AN2 and CARIATI_CA_P_1_BATTERIA read AN1, AN2, BATTERIA;
 * plant/line1/T1 and plant/line1/T2 read T1 and T2. A lone tag, or names
 * with nothing in common, keep their last path segment.
 */
export function shortTagNames(names: { id: number; name: string }[]): Map<number, string> {
    const out = new Map<number, string>();
    const lastSegment = (n: string) => n.split('/').filter(Boolean).pop() || n;
    if (names.length < 2) {
        for (const { id, name } of names) out.set(id, lastSegment(name));
        return out;
    }
    let prefix = names[0].name;
    for (const { name } of names) {
        let i = 0;
        while (i < prefix.length && i < name.length && prefix[i] === name[i]) i++;
        prefix = prefix.slice(0, i);
    }
    // Only up to the last separator: AN1/AN2 share "AN", which is part of the name.
    const cut = Math.max(...['_', '/', '.', '-', ' ', ':'].map((c) => prefix.lastIndexOf(c)));
    const strip = cut >= 0 ? cut + 1 : 0;
    for (const { id, name } of names) {
        const rest = name.slice(strip);
        out.set(id, rest ? lastSegment(rest) : lastSegment(name));
    }
    return out;
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
