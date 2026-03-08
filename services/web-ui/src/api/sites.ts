import api from './client';
import { Site, CreateSiteDto } from '@/types';

export const sitesApi = {
    getAll: async (orgId?: number): Promise<Site[]> => {
        const response = await api.get('/sites', { params: { org_id: orgId } });
        return response.data;
    },

    create: async (data: CreateSiteDto): Promise<Site> => {
        const config = {
            headers: {
                'X-Organization-ID': data.org_id.toString()
            }
        };
        const response = await api.post('/sites', data, config);
        return response.data;
    },

    delete: async (id: number): Promise<void> => {
        await api.delete(`/sites/${id}`);
    },

    get: async (id: number): Promise<Site> => {
        const response = await api.get(`/sites/${id}`);
        return response.data;
    },

    update: async (id: number, data: { name: string }): Promise<Site> => {
        const response = await api.put(`/sites/${id}`, data);
        return response.data;
    }
};
