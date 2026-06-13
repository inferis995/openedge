import api from './client';

export interface AuditLog {
    id: number;
    user_id: number | null;
    username: string;
    action: string;
    ip_address: string;
    user_agent: string;
    details: Record<string, unknown> | null;
    success: boolean;
    created_at: string;
}

export interface AuditFilters {
    start?: string;
    end?: string;
    action?: string;
    user_id?: number;
    success?: 'true' | 'false';
    limit?: number;
}

export const auditApi = {
    getLogs: async (filters: AuditFilters = {}): Promise<AuditLog[]> => {
        const params: Record<string, string | number> = {};
        if (filters.start) params.start = filters.start;
        if (filters.end) params.end = filters.end;
        if (filters.action) params.action = filters.action;
        if (filters.user_id) params.user_id = filters.user_id;
        if (filters.success !== undefined) params.success = filters.success;
        if (filters.limit) params.limit = filters.limit;
        const response = await api.get('/audit/logs', { params });
        return response.data;
    },

    getActions: async (): Promise<string[]> => {
        const response = await api.get('/audit/actions');
        return response.data;
    },
};
