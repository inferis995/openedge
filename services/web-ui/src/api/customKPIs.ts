import api from './client';

export type AggregationType = 'avg' | 'sum' | 'min' | 'max' | 'last' | 'delta' | 'count';

export interface CustomKPI {
    id: number;
    name: string;
    tag_id: number;
    tag_alias?: string;
    aggregation: AggregationType;
    window_minutes: number;
    unit: string;
    multiplier: number;
    good_when: 'up' | 'down';
    target_value?: number | null;
    active: boolean;
}

export interface CreateCustomKPIDto {
    name: string;
    tag_id: number;
    aggregation: AggregationType;
    window_minutes: number;
    unit?: string;
    multiplier?: number;
    good_when?: 'up' | 'down';
    target_value?: number | null;
    active?: boolean;
}

// Etichette plain-Italian per il dropdown aggregazione — più chiaro di
// "avg/sum" per un operatore non tecnico.
export const AGGREGATION_LABELS: Record<AggregationType, string> = {
    avg: 'Media (avg)',
    sum: 'Somma (sum)',
    min: 'Minimo (min)',
    max: 'Massimo (max)',
    last: 'Ultimo valore',
    delta: 'Variazione (max-min cronologico)',
    count: 'Numero di campioni',
};

// Preset finestra in minuti, dal più recente al più lungo.
export const WINDOW_PRESETS: { value: number; label: string }[] = [
    { value: 60,    label: 'Ultima ora' },
    { value: 240,   label: 'Ultime 4 ore' },
    { value: 480,   label: 'Ultime 8 ore (turno)' },
    { value: 1440,  label: 'Ultime 24 ore' },
    { value: 10080, label: 'Ultimi 7 giorni' },
];

export const customKPIsApi = {
    list: async (): Promise<CustomKPI[]> => {
        const r = await api.get('/custom-kpis');
        return r.data;
    },
    create: async (data: CreateCustomKPIDto): Promise<{ id: number }> => {
        const r = await api.post('/custom-kpis', data);
        return r.data;
    },
    update: async (id: number, data: CreateCustomKPIDto): Promise<void> => {
        await api.put(`/custom-kpis/${id}`, data);
    },
    delete: async (id: number): Promise<void> => {
        await api.delete(`/custom-kpis/${id}`);
    },
};
