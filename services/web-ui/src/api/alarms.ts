import api from './client';
import { Alarm } from '@/types';

export const alarmsApi = {
    getAll: async (state?: string): Promise<Alarm[]> => {
        const response = await api.get('/alarms', { params: { state } });
        return response.data;
    },

    acknowledge: async (id: number): Promise<void> => {
        await api.post(`/alarms/${id}/acknowledge`);
    }
};
