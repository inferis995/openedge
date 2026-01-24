import api from './client';
import { Area, CreateAreaDto } from '@/types';

export const areasApi = {
    getAll: async (siteId?: number): Promise<Area[]> => {
        const response = await api.get('/areas', { params: { site_id: siteId } });
        return response.data;
    },

    create: async (data: CreateAreaDto): Promise<Area> => {
        const response = await api.post('/areas', data);
        return response.data;
    },

    delete: async (id: number): Promise<void> => {
        await api.delete(`/areas/${id}`);
    }
};
