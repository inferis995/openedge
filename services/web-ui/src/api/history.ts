import api from './client';
import {
    BatchHistoryRequest,
    BatchHistoryResponse,
    AggregationType,
} from '@/types/trend';

export interface HistoryQueryParams {
    tag_id: number;
    start: string; // ISO 8601 format
    end: string; // ISO 8601 format
    agg?: AggregationType;
    interval?: string; // e.g., '1m', '5m', '1h', '1d'
    stats?: boolean; // Include statistics in response
    raw?: boolean; // Force raw data query (bypass aggregates)
}

export interface HistoryDataPoint {
    timestamp: number;
    value: number | null; // null = gap in chart
    quality: number;
    source?: string; // "raw", "1m", "1h", "1d"
    sample_count?: number; // Points aggregated into this bucket
}

export interface TagStats {
    min_value: number | null;
    max_value: number | null;
    avg_value: number | null;
    std_dev: number | null;
    sample_count: number;
    first_value: number | null;
    last_value: number | null;
    first_timestamp: number | null;
    last_timestamp: number | null;
}

export interface HistoryResponse {
    data: HistoryDataPoint[];
    source: string; // Which aggregate level was used ("raw", "1m", "1h", "1d")
    total_points: number;
    auto_interval: boolean;
    stats?: TagStats; // Only present if stats=true was requested
}

export const historyApi = {
    // Single tag query - returns enhanced response with metadata
    query: async (params: HistoryQueryParams): Promise<HistoryDataPoint[]> => {
        const response = await api.get<HistoryResponse>('/history', { params });
        // Handle both old array format and new object format
        if (Array.isArray(response.data)) {
            return response.data;
        }
        return response.data.data;
    },

    // Query with full response including metadata
    queryWithMeta: async (params: HistoryQueryParams): Promise<HistoryResponse> => {
        const response = await api.get<HistoryResponse>('/history', { params });
        // Handle both old array format and new object format
        if (Array.isArray(response.data)) {
            return {
                data: response.data,
                source: 'raw',
                total_points: response.data.length,
                auto_interval: false,
            };
        }
        return response.data;
    },

    // Get statistics for a tag over a time range
    getStats: async (tagId: number, start: string, end: string): Promise<TagStats> => {
        const response = await api.get<TagStats>('/history/stats', {
            params: { tag_id: tagId, start, end }
        });
        return response.data;
    },

    // Batch query for multiple tags - more efficient than multiple single queries
    batchQuery: async (request: BatchHistoryRequest): Promise<BatchHistoryResponse> => {
        const response = await api.post('/history/batch', request);
        return response.data;
    },

    // Get data range (oldest/newest timestamps) for the organization
    getDataRange: async (): Promise<{ oldest: string | null; newest: string | null; hasData: boolean }> => {
        const response = await api.get('/history/data-range');
        return response.data;
    },
};
