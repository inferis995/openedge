import { useEffect } from 'react';
import { organizationsApi } from '@/api/organizations';
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

export function accentForTheme(themeColor: string, dark: boolean): string | null {
    const lum = luminance(themeColor);
    if (lum === null) return null;
    let hex = themeColor;
    if (lum < 0.08 || lum > 0.92) {
        // Near-black or near-white: flip so it reads on the background.
        hex = dark ? '#ffffff' : '#000000';
    }
    const hsl = hexToHsl(hex);
    return hsl ? `${hsl[0]} ${hsl[1]}% ${hsl[2]}%` : null;
}

// contrastFg returns the readable text color (black or white) for any
// background hex. Single rule, both themes, no special cases.
function contrastFg(bgHex: string): string {
    const lum = luminance(bgHex);
    return lum !== null && lum > 0.4 ? '0 0% 2%' : '0 0% 100%';
}

function applyOrgAccent(themeColor: string | undefined, dark: boolean) {
    const root = document.documentElement;
    if (!themeColor) {
        root.style.removeProperty('--primary');
        root.style.removeProperty('--primary-foreground');
        root.style.removeProperty('--accent');
        return;
    }
    // Raw accent for brand spots (logo, animations): B/W resolved per theme.
    const lum0 = luminance(themeColor);
    const extreme = lum0 !== null && (lum0 < 0.08 || lum0 > 0.92);
    root.style.setProperty('--accent', extreme ? (dark ? '#ffffff' : '#000000') : themeColor);
    const accent = accentForTheme(themeColor, dark);
    if (!accent) return;
    if (dark) {
        // Lime button background → org color; text contrasts with it.
        // The foreground is computed on the RESOLVED background (after the
        // B/W flip), so black and white both land readable.
        root.style.setProperty('--primary', accent);
        const lum0 = luminance(themeColor);
        const extreme = lum0 !== null && (lum0 < 0.08 || lum0 > 0.92);
        const bgHex = extreme ? '#ffffff' : themeColor;
        root.style.setProperty('--primary-foreground', contrastFg(bgHex));
    } else {
        // Light theme: buttons stay black and the accent is text ON black.
        // White stays white (readable); only near-black flips to white,
        // because black-on-black is the one combination that cannot read.
        const lum = luminance(themeColor);
        if (lum !== null && lum < 0.08) {
            root.style.setProperty('--primary-foreground', '0 0% 100%');
        } else {
            const hsl = hexToHsl(themeColor);
            if (hsl) root.style.setProperty('--primary-foreground', `${hsl[0]} ${hsl[1]}% ${hsl[2]}%`);
        }
    }
}

export function useOrgTheme() {
    const selectedOrgId = useNavigationStore((s) => s.selectedOrgId);
    const theme = useThemeStore((s) => s.theme);

    useEffect(() => {
        if (!selectedOrgId) {
            applyOrgAccent(undefined, false);
            return;
        }
        let ignore = false;
        organizationsApi
            .get(selectedOrgId)
            .then((org) => {
                if (!ignore) applyOrgAccent(org.theme_color, theme === 'dark');
            })
            .catch(() => {
                if (!ignore) applyOrgAccent(undefined, false);
            });
        return () => {
            ignore = true;
        };
    }, [selectedOrgId]);

    // Re-apply when the light/dark theme flips (B/W inversion depends on it).
    useEffect(() => {
        if (!selectedOrgId) return;
        organizationsApi
            .get(selectedOrgId)
            .then((org) => applyOrgAccent(org.theme_color, theme === 'dark'))
            .catch(() => {});
    }, [theme]); // eslint-disable-line react-hooks/exhaustive-deps
}
