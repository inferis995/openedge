import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { organizationsApi } from '@/api/organizations';
import { isPlatformPath } from '@/components/layout/navigation';
import { useAuthStore } from '@/stores/useAuthStore';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useThemeStore } from '@/stores/useThemeStore';

// Per-organization accent color. The org's theme_color replaces the product
// lime wherever the theme uses it as an accent:
//   dark theme  → --primary (lime buttons)
//   light theme → --primary-foreground (lime text on the black buttons)
//
// Black & white invert: an org that picks black gets black in the light
// theme and white in the dark theme (and vice versa for white), so the
// accent is always visible. Any other color is used as-is in both themes.
function hexToHsl(hex: string): [number, number, number] | null {
    let h = hex.replace('#', '');
    if (h.length === 3) h = h.split('').map((c) => c + c).join('');
    if (!/^[0-9a-fA-F]{6}$/.test(h)) return null;
    const r = parseInt(h.slice(0, 2), 16) / 255;
    const g = parseInt(h.slice(2, 4), 16) / 255;
    const b = parseInt(h.slice(4, 6), 16) / 255;
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    const l = (max + min) / 2;
    if (max === min) return [0, 0, l * 100];
    const d = max - min;
    const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    let hh: number;
    if (max === r) hh = ((g - b) / d + (g < b ? 6 : 0)) / 6;
    else if (max === g) hh = ((b - r) / d + 2) / 6;
    else hh = ((r - g) / d + 4) / 6;
    return [Math.round(hh * 360), Math.round(s * 100), Math.round(l * 100)];
}

function luminance(hex: string): number | null {
    let h = hex.replace('#', '');
    if (h.length === 3) h = h.split('').map((c) => c + c).join('');
    if (!/^[0-9a-fA-F]{6}$/.test(h)) return null;
    const f = (i: number) => {
        const c = parseInt(h.slice(i, i + 2), 16) / 255;
        return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * f(0) + 0.7152 * f(2) + 0.0722 * f(4);
}

// contrastFg returns the readable text color (black or white) for any
// background hex. Single rule, both themes, no special cases.
function contrastFg(bgHex: string): string {
    const lum = luminance(bgHex);
    return lum !== null && lum > 0.4 ? '0 0% 2%' : '0 0% 100%';
}

function applyOrgAccent(themeColor: string | undefined, dark: boolean) {
    const root = document.documentElement;
    // Always reset first: a theme switch must never inherit the other
    // theme's background (stale pink button in the light theme).
    root.style.removeProperty('--primary');
    root.style.removeProperty('--primary-foreground');
    if (!themeColor) {
        root.style.removeProperty('--brand');
        return;
    }
    // Raw accent for brand spots (logo, animations): B/W resolved per theme.
    // Its own variable: --accent is an HSL triplet the theme reads as
    // hsl(var(--accent)) for hover and selection, and a hex written there
    // made every one of those backgrounds invalid — no hover at all.
    const lum0 = luminance(themeColor);
    const extreme = lum0 !== null && (lum0 < 0.08 || lum0 > 0.92);
    root.style.setProperty('--brand', extreme ? (dark ? '#ffffff' : '#000000') : themeColor);
    // Primary buttons take the accent as background IN BOTH themes, with
    // contrasting text — same treatment, reads everywhere. The only flip:
    // white cannot be a background in the light theme (invisible button),
    // black cannot be one in the dark theme — those fall back to default.
    let bgHex = themeColor;
    if (extreme) {
        bgHex = dark ? '#ffffff' : '#000000';
    }
    const hsl = hexToHsl(bgHex);
    if (!hsl) return;
    root.style.setProperty('--primary', `${hsl[0]} ${hsl[1]}% ${hsl[2]}%`);
    root.style.setProperty('--primary-foreground', contrastFg(bgHex));
}

const THEME_CACHE_KEY = 'oe-org-themes-v1';

function cachedTheme(orgId: number): string | undefined {
    try {
        const all = JSON.parse(localStorage.getItem(THEME_CACHE_KEY) || '{}');
        const v = all[String(orgId)];
        return typeof v === 'string' && v ? v : undefined;
    } catch {
        return undefined;
    }
}

function storeTheme(orgId: number, color: string | undefined) {
    try {
        const all = JSON.parse(localStorage.getItem(THEME_CACHE_KEY) || '{}');
        if (color) all[String(orgId)] = color;
        else delete all[String(orgId)];
        localStorage.setItem(THEME_CACHE_KEY, JSON.stringify(all));
    } catch {
        /* storage unavailable: API remains the source of truth */
    }
}

// The organization whose colour should be on screen now (null: the
// product's). An answer about any other — a slow response for the
// organization the user just left — is dropped instead of painted.
let wanted: number | null = null;

export function refreshOrgAccent(orgId: number, dark: boolean) {
    return organizationsApi
        .get(orgId)
        .then((org) => {
            storeTheme(orgId, org.theme_color);
            if (wanted === orgId) applyOrgAccent(org.theme_color, dark);
        })
        .catch(() => {
            if (wanted === orgId) applyOrgAccent(cachedTheme(orgId), dark);
        });
}

export function useOrgTheme() {
    const selectedOrgId = useNavigationStore((s) => s.selectedOrgId);
    const theme = useThemeStore((s) => s.theme);
    const isGlobalAdmin = useAuthStore((s) => s.user?.role === 'admin' && !s.user?.org_id);
    const { pathname } = useLocation();
    const dark = theme === 'dark';
    // A global admin always has an organization selected (the first one, if
    // none was picked), so the selection alone does not mean "inside a
    // tenant": on the platform's own pages they get the product's colours.
    const orgId = selectedOrgId && !(isGlobalAdmin && isPlatformPath(pathname)) ? selectedOrgId : null;

    useEffect(() => {
        wanted = orgId;
        if (!orgId) {
            applyOrgAccent(undefined, dark);
            return;
        }
        // Instant: cached color first (no flash), API confirms right after.
        // Again on a light/dark switch: black and white invert with it.
        applyOrgAccent(cachedTheme(orgId), dark);
        refreshOrgAccent(orgId, dark);
    }, [orgId, dark]);
}
