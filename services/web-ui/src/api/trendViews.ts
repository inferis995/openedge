import api from './client';

export interface SavedTrendView {
    id: number;
    name: string;
    /** The trend page's address query (pens, window, aggregation, comparison, axes). */
    query: string;
    created_by?: number;
    author?: string;
    updated_at: string;
}

/** Named trend views of the organization: any member saves and opens; author or admin changes. */
export const trendViewsApi = {
    list: async (): Promise<SavedTrendView[]> => (await api.get('/trend-views')).data,
    save: async (name: string, query: string): Promise<{ id: number }> => (await api.post('/trend-views', { name, query })).data,
    remove: async (id: number): Promise<void> => { await api.delete(`/trend-views/${id}`); },
};
