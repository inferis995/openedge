import api from './client';

export interface HistoryQueryParams {
    tag_id: number;
    start: string; // ISO 8601 format
    end: string; // ISO 8601 format
    agg?: 'mean' | 'max' | 'min' | 'sum' | 'first' | 'last' | 'count' | 'median' | 'stddev';
    interval?: string; // e.g., '1m', '5m', '1h', '1d'
}

export interface HistoryDataPoint {
    timestamp: number;
    value: number;
    quality: number;
}

export const historyApi = {
    query: async (params: HistoryQueryParams): Promise<HistoryDataPoint[]> => {
        const response = await api.get('/history', { params });
        return response.data;
    }
};
