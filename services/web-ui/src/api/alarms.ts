import api from './client';
import { Alarm } from '@/types';

export interface AlarmsFilters {
    state?: string;
    start?: string;
    end?: string;
    tag_id?: number | null;
}

export const alarmsApi = {
    getAll: async (filters?: AlarmsFilters): Promise<Alarm[]> => {
        const params: Record<string, string | number> = {};
        if (filters?.state && filters.state !== 'all') {
            params.state = filters.state;
        }
        if (filters?.start) {
            params.start = filters.start;
        }
        if (filters?.end) {
            params.end = filters.end;
        }
        if (filters?.tag_id) {
            params.tag_id = filters.tag_id;
        }
        const response = await api.get('/alarms', { params });
        return response.data;
    },

    acknowledge: async (id: number): Promise<void> => {
        await api.post(`/alarms/${id}/acknowledge`);
    }
};
