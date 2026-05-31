import { useTranslation } from 'react-i18next';

// Tiny dropdown to switch the UI language at runtime. The choice is
// persisted by i18next-browser-languagedetector under
// localStorage["openedge.lang"], so it survives a page reload.
//
// To add a new language: drop a JSON file under src/i18n/locales/<code>.json,
// register it in src/i18n/index.ts, then add an <option> here.

const LANGS = [
    { code: 'en', label: 'EN' },
    { code: 'it', label: 'IT' },
];

const LanguageSwitch = () => {
    const { i18n, t } = useTranslation();
    return (
        <select
            aria-label={t('language.label')}
            value={i18n.resolvedLanguage ?? 'en'}
            onChange={(e) => void i18n.changeLanguage(e.target.value)}
            className="bg-transparent border border-input rounded-md text-xs px-2 py-1"
        >
            {LANGS.map((l) => (
                <option key={l.code} value={l.code}>{l.label}</option>
            ))}
        </select>
    );
};

export default LanguageSwitch;
