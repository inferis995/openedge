import api from './client';
import { Organization, CreateOrganizationDto } from '@/types';

export interface EdgeStatus {
    online: boolean;
    last_ping: string | null;
    reason?: string;
}

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
    },

    getEdgeStatus: async (id: number): Promise<EdgeStatus> => {
        const response = await api.get(`/organizations/${id}/edge-status`);
        return response.data;
    },

    downloadEdgeInstaller: async (id: number, orgName: string): Promise<void> => {
        const response = await api.get(`/organizations/${id}/edge-installer`, {
            responseType: 'blob',
        });
        const url = window.URL.createObjectURL(new Blob([response.data]));
        const link = document.createElement('a');
        link.href = url;
        link.setAttribute('download', `openedge-${orgName.toLowerCase().replace(/\s+/g, '-')}.zip`);
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.URL.revokeObjectURL(url);
    },
};
