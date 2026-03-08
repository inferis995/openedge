import api from './client';
import { Area, CreateAreaDto } from '@/types';

export const areasApi = {
    getAll: async (siteId?: number): Promise<Area[]> => {
        const response = await api.get('/areas', { params: { site_id: siteId } });
        return response.data;
    },

    get: async (id: number): Promise<Area> => {
        const response = await api.get(`/areas/${id}`);
        return response.data;
    },

    create: async (data: CreateAreaDto): Promise<Area> => {
        const config = data.org_id ? {
            headers: {
                'X-Organization-ID': data.org_id.toString()
            }
        } : undefined;
        const response = await api.post('/areas', data, config);
        return response.data;
    },

    delete: async (id: number): Promise<void> => {
        await api.delete(`/areas/${id}`);
    },

    update: async (id: number, data: { name: string }): Promise<Area> => {
        const response = await api.put(`/areas/${id}`, data);
        return response.data;
    }
};
