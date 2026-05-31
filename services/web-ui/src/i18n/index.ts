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

void i18n
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
        resources: {
            en: { translation: en },
            it: { translation: it },
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
