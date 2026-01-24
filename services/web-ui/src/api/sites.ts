import api from './client';
import { Site, CreateSiteDto } from '@/types';

export const sitesApi = {
    getAll: async (orgId?: number): Promise<Site[]> => {
        const response = await api.get('/sites', { params: { org_id: orgId } });
        return response.data;
    },

    create: async (data: CreateSiteDto): Promise<Site> => {
        const response = await api.post('/sites', data);
        return response.data;
    },

    delete: async (id: number): Promise<void> => {
        await api.delete(`/sites/${id}`);
    }
};
