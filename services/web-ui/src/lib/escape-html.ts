/**
 * Escapes text for an HTML string.
 *
 * For the few places that must hand HTML to a library — an ECharts tooltip
 * formatter returns a string that ECharts puts into innerHTML. A tag alias, a
 * STRING tag's value or anything else a user or a PLC can set goes through
 * this first, or it runs as markup in every browser that hovers the chart.
 */
export function escapeHtml(s: unknown): string {
    return String(s ?? '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}
