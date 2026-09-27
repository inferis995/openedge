import { describe, expect, it } from 'vitest';
import i18n from '@/i18n';
import { findNavItem, isActivePath, navSections, scorePage } from './navigation';

type Tree = { [k: string]: string | Tree };

// The merged bundles: locales/<lang>.json plus every locales/<lang>/*.json.
const itLocale = i18n.getResourceBundle('it', 'translation') as Tree;
const en = i18n.getResourceBundle('en', 'translation') as Tree;

function lookup(tree: Tree, key: string): unknown {
    return key.split('.').reduce<unknown>(
        (node, part) => (node && typeof node === 'object' ? (node as Tree)[part] : undefined),
        tree,
    );
}

function keys(tree: Tree, prefix = ''): string[] {
    return Object.entries(tree).flatMap(([k, v]) =>
        typeof v === 'string' ? [prefix + k] : keys(v, prefix + k + '.'));
}

const everyone = [
    { isAdmin: true, isGlobalAdmin: true, isOrgScoped: false },
    { isAdmin: true, isGlobalAdmin: false, isOrgScoped: true },
    { isAdmin: false, isGlobalAdmin: false, isOrgScoped: true },
];

describe('navigation', () => {
    it('has a label, in both languages, for every entry anyone can see', () => {
        for (const access of everyone) {
            for (const s of navSections(access)) {
                for (const key of [s.key, ...s.items.flatMap((i) => [i.key, i.hint ?? i.key])]) {
                    // A missing key shows the key itself — "nav.udt_types" in the menu.
                    expect(typeof lookup(itLocale as Tree, key), `it: ${key}`).toBe('string');
                    expect(typeof lookup(en as Tree, key), `en: ${key}`).toBe('string');
                }
            }
        }
    });

    it('keeps the two languages in step, page files included', () => {
        expect(keys(itLocale as Tree).sort()).toEqual(keys(en as Tree).sort());
    });

    it('shows administration only to admins, and the platform pages only to global admins', () => {
        const paths = (a: (typeof everyone)[number]) =>
            navSections(a).flatMap((s) => s.items.map((i) => i.path));
        expect(paths(everyone[0])).toContain('/system');
        expect(paths(everyone[1])).not.toContain('/system');
        expect(paths(everyone[1])).toContain('/users');
        expect(paths(everyone[2])).not.toContain('/users');
    });

    it('lists every page once', () => {
        const paths = navSections(everyone[0]).flatMap((s) => s.items.map((i) => i.path));
        expect(new Set(paths).size).toBe(paths.length);
    });

    it('keeps the menu entry lit on a detail page', () => {
        expect(isActivePath('/synoptics', '/synoptics/4/edit')).toBe(true);
        expect(isActivePath('/udt/types', '/udt/instances')).toBe(false);
        expect(isActivePath('/', '/tags')).toBe(false);
        const item = findNavItem(navSections(everyone[0]), '/udt/types/7');
        expect(item?.key).toBe('nav.udt_types');
    });

    it('finds a page by a word, not by letters scattered across other pages', () => {
        const udt = scorePage('Tipi (UDT)', 'turni', ['Modelli riutilizzabili di macchina (motore, pompa…)']);
        const shifts = scorePage('Turni', 'turni', ['Orari di lavoro e turni della produzione']);
        expect(udt).toBe(0);
        expect(shifts).toBeGreaterThan(0);
        expect(scorePage('Allarmi', 'import tag', ['Allarmi attivi'])).toBe(0);
        expect(scorePage('Tag', 'importa', ['Le variabili lette dai PLC; importa ed esporta'])).toBeGreaterThan(0);
        expect(scorePage('Manutenzione', 'attivita', ['attività programmate'])).toBeGreaterThan(0);
        expect(scorePage('Turni', 'tur')).toBeGreaterThan(scorePage('Allarmi', 'tur', ['turni di notte']));
    });
});
