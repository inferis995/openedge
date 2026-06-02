import api from './client';

export interface MaintenanceWindow {
    id: number;
    title: string;
    start_at: string;
    end_at: string;
    reason?: string;
    created_by?: number | null;
    created_at: string;
    active: boolean;
}

export interface CreateMaintenanceDto {
    title: string;
    start_at: string;       // RFC3339
    end_at: string;
    reason?: string;
}

export const maintenanceApi = {
    list: async (onlyActive = false): Promise<MaintenanceWindow[]> => {
        const r = await api.get('/maintenance', { params: onlyActive ? { active: 'true' } : {} });
        return r.data;
    },
    create: async (data: CreateMaintenanceDto): Promise<{ id: number }> => {
        const r = await api.post('/maintenance', data);
        return r.data;
    },
    update: async (id: number, data: CreateMaintenanceDto): Promise<void> => {
        await api.put(`/maintenance/${id}`, data);
    },
    delete: async (id: number): Promise<void> => {
        await api.delete(`/maintenance/${id}`);
    },
};
