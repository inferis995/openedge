import api from './client';
import { Organization, CreateOrganizationDto } from '@/types';

export const organizationsApi = {
    getAll: async (): Promise<Organization[]> => {
        const response = await api.get('/organizations');
        return response.data;
    },

    get: async (id: number): Promise<Organization> => {
        const response = await api.get(`/organizations/${id}`);
        return response.data;
    },

    create: async (data: CreateOrganizationDto): Promise<Organization> => {
        const response = await api.post('/organizations', data);
        return response.data;
    },

    delete: async (id: number): Promise<void> => {
        await api.delete(`/organizations/${id}`);
    },

    update: async (id: number, data: CreateOrganizationDto): Promise<Organization> => {
        const response = await api.put(`/organizations/${id}`, data);
        return response.data;
    }
};
