// Descrizione unica delle condizioni di allarme, condivisa fra la scheda di
// configurazione del tag e la pagina degli allarmi. Stava dentro TagAlarmsTab:
// da quando esistono condizioni senza soglia serve anche altrove, e due copie
// dello stesso elenco divergono al primo tipo aggiunto.

export interface AlarmTypeMeta {
    label: string;
    thresholdLabel: string;
    helper: string;
}

export const ALARM_TYPE_LABELS: Record<string, AlarmTypeMeta> = {
    bool_true:  { label: 'Quando diventa VERO (ON)',  thresholdLabel: '', helper: 'Scatta sul fronte di salita 0 → 1.' },
    bool_false: { label: 'Quando diventa FALSO (OFF)', thresholdLabel: '', helper: 'Scatta sul fronte di discesa 1 → 0.' },
    high:       { label: 'Valore troppo alto',        thresholdLabel: 'Soglia massima',     helper: 'Scatta quando valore > soglia massima.' },
    low:        { label: 'Valore troppo basso',       thresholdLabel: 'Soglia minima',      helper: 'Scatta quando valore < soglia minima.' },
    high_high:  { label: 'Valore CRITICO alto',       thresholdLabel: 'Soglia critica alta', helper: 'Soglia oltre il "troppo alto" — usata per allarmi urgenti.' },
    low_low:    { label: 'Valore CRITICO basso',      thresholdLabel: 'Soglia critica bassa', helper: 'Soglia oltre il "troppo basso" — usata per allarmi urgenti.' },
    // Condizioni di "salute": non guardano il valore ma il collegamento.
    // Non hanno soglia — il ritardo È la loro soglia (quanti secondi di
    // silenzio, o di immobilità, si tollerano prima di allarmare).
    comm_loss:  { label: 'Comunicazione persa (PLC non risponde)', thresholdLabel: '', helper: 'Scatta quando non arriva più nessuna lettura valida per il tempo indicato sotto.' },
    frozen:     { label: 'Valore bloccato (sensore o PLC fermo)',  thresholdLabel: '', helper: 'Le letture arrivano ma il valore non si muove per il tempo indicato. Utile su un contatore watchdog.' },
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
const BADGE_LABELS: Record<string, string> = {
    comm_loss: 'Comunicazione persa',
    frozen: 'Valore bloccato',
};

export function alarmTypeBadgeLabel(alarmType: string): string {
    return BADGE_LABELS[alarmType] ?? alarmType;
}
