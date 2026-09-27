/// <reference types="vite/client" />
// i18n bootstrap.
//
// Strategy:
//  - Detection order: localStorage → navigator language → fallback ('en').
//  - Translations live as plain JSON under ./locales/<lang>.json so they
//    are bundled at build time (no runtime fetch, no flash-of-untranslated).
//  - Add a new language by dropping <lang>.json into ./locales and adding
//    it to the `resources` object below.
//
// Usage in a component:
//
//    import { useTranslation } from 'react-i18next';
//    const { t } = useTranslation();
//    return <button>{t('common.save')}</button>;
//
// String keys follow a "namespace.specific" convention; keep them stable —
// when a string ROUTE doesn't change, the key shouldn't either, otherwise
// every locale needs a re-translation.

import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

import en from './locales/en.json';
import it from './locales/it.json';

// Page-specific strings live in locales/<lang>/<page>.json, one file per page
// or group of pages, each under its own top-level key. The shared strings
// (common, nav, errors…) stay in locales/<lang>.json. Splitting them keeps a
// change to one page from touching — and conflicting with — every other.
type Tree = { [k: string]: string | Tree };
const pageFiles = import.meta.glob<{ default: Tree }>('./locales/*/*.json', { eager: true });

function withPages(base: Tree, lang: string): Tree {
    const out: Tree = { ...base };
    for (const [path, mod] of Object.entries(pageFiles)) {
        if (path.split('/')[2] !== lang) continue;
        for (const [key, value] of Object.entries(mod.default)) {
            if (key in out) {
                throw new Error(`i18n: "${key}" in ${path} is already defined elsewhere`);
            }
            out[key] = value;
        }
    }
    return out;
}

void i18n
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
        resources: {
            en: { translation: withPages(en as Tree, 'en') },
            it: { translation: withPages(it as Tree, 'it') },
        },
        fallbackLng: 'en',
        // 'localStorage' first → operator's choice persists across reloads
        // 'navigator' second → first-visit picks the browser language
        detection: {
            order: ['localStorage', 'navigator'],
            caches: ['localStorage'],
            lookupLocalStorage: 'openedge.lang',
        },
        interpolation: {
            // React already escapes; double-escaping mangles the placeholders.
            escapeValue: false,
        },
        // Missing keys log a warning in dev and return the key itself in
        // prod — never display "undefined" in the UI.
        returnNull: false,
    });

export default i18n;
