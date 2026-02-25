import api from './client';
import {
    BatchHistoryRequest,
    BatchHistoryResponse,
    CompareHistoryRequest,
    CompareHistoryResponse,
    AggregationType,
} from '@/types/trend';

export interface HistoryQueryParams {
    tag_id: number;
    start: string; // ISO 8601 format
    end: string; // ISO 8601 format
    agg?: AggregationType;
    interval?: string; // e.g., '1m', '5m', '1h', '1d'
}

export interface HistoryDataPoint {
    timestamp: number;
    value: number;
    quality: number;
}

export const historyApi = {
    // Single tag query
    query: async (params: HistoryQueryParams): Promise<HistoryDataPoint[]> => {
        const response = await api.get('/history', { params });
        return response.data;
    },

    // Batch query for multiple tags - more efficient than multiple single queries
    batchQuery: async (request: BatchHistoryRequest): Promise<BatchHistoryResponse> => {
        const response = await api.post('/history/batch', request);
        return response.data;
    },

    // Compare two time ranges (for compare mode)
    compareQuery: async (request: CompareHistoryRequest): Promise<CompareHistoryResponse> => {
        const response = await api.post('/history/compare', request);
        return response.data;
    },
};
