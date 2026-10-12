/**
 * "2 min fa", "3 h fa", "ieri": how long ago an instant was, in the user's
 * language, short. A time that is not a finite number reads as an em dash.
 */
export function timeAgo(ms: number | null | undefined, lang: string, now = Date.now()): string {
    if (ms == null || !Number.isFinite(ms)) return '—';
    const s = (ms - now) / 1000;
    const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'auto', style: 'short' });
    const a = Math.abs(s);
    if (a < 45) return rtf.format(0, 'second');
    if (a < 3600) return rtf.format(Math.round(s / 60), 'minute');
    if (a < 86400) return rtf.format(Math.round(s / 3600), 'hour');
    if (a < 30 * 86400) return rtf.format(Math.round(s / 86400), 'day');
    return new Date(ms).toLocaleDateString(lang, { day: '2-digit', month: '2-digit', year: '2-digit' });
}
