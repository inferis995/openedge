import i18n from '@/i18n';

// Descrizione unica delle condizioni di allarme, condivisa fra la scheda di
// configurazione del tag e la pagina degli allarmi. Stava dentro TagAlarmsTab:
// da quando esistono condizioni senza soglia serve anche altrove, e due copie
// dello stesso elenco divergono al primo tipo aggiunto.

export interface AlarmTypeMeta {
    label: string;
    thresholdLabel: string;
    helper: string;
}

// Read through getters so the text follows the language the user picks: the
// components that show it re-render on a language change and read it again.
// The strings live in locales/<lang>/alarm-types.json.
function meta(type: string, withThreshold: boolean): AlarmTypeMeta {
    return {
        get label() { return i18n.t(`alarmTypes.${type}.label`); },
        get thresholdLabel() { return withThreshold ? i18n.t(`alarmTypes.${type}.threshold`) : ''; },
        get helper() { return i18n.t(`alarmTypes.${type}.helper`); },
    };
}

export const ALARM_TYPE_LABELS: Record<string, AlarmTypeMeta> = {
    bool_true:  meta('bool_true', false),
    bool_false: meta('bool_false', false),
    high:       meta('high', true),
    low:        meta('low', true),
    high_high:  meta('high_high', true),
    low_low:    meta('low_low', true),
    // Condizioni di "salute": non guardano il valore ma il collegamento.
    // Non hanno soglia — il ritardo È la loro soglia (quanti secondi di
    // silenzio, o di immobilità, si tollerano prima di allarmare).
    comm_loss:  meta('comm_loss', false),
    frozen:     meta('frozen', false),
};

// Condizioni che non hanno una soglia numerica: booleane e di salute.
export const NO_THRESHOLD_TYPES = new Set(['bool_true', 'bool_false', 'comm_loss', 'frozen']);

// Condizioni di salute: il "ritardo" è il timeout di silenzio/immobilità,
// non un anti-rimbalzo, e va spiegato diversamente all'operatore.
export const HEALTH_TYPES = new Set(['comm_loss', 'frozen']);

// Etichetta breve per le tabelle. Solo le condizioni di salute vengono
// tradotte: gli altri tipi sono in tabella da sempre e gli operatori li
// riconoscono così come sono. L'export CSV resta sul valore grezzo, che è
// quello che serve a chi lo rilegge da un programma.
const BADGE_TYPES = new Set(['comm_loss', 'frozen']);

export function alarmTypeBadgeLabel(alarmType: string): string {
    return BADGE_TYPES.has(alarmType) ? i18n.t(`alarmTypes.${alarmType}.badge`) : alarmType;
}
