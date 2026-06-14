import api from './client';
import { Organization, CreateOrganizationDto } from '@/types';

export interface EdgeStatus {
    online: boolean;
    last_ping: string | null;
    reason?: string;
}

export interface SSOProvider {
    id: number;
    provider: 'google' | 'azure';
    client_id: string;
    tenant_id?: string;
    domain_hint?: string;
    enabled: boolean;
    created_at: string;
}

export interface SSOProviderInput {
    provider: 'google' | 'azure';
    client_id: string;
    client_secret: string;
    tenant_id?: string;
    domain_hint?: string;
    enabled: boolean;
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

    listSSOProviders: async (orgId: number): Promise<SSOProvider[]> => {
        const response = await api.get(`/organizations/${orgId}/sso-providers`);
        return response.data;
    },

    upsertSSOProvider: async (orgId: number, data: SSOProviderInput): Promise<void> => {
        await api.post(`/organizations/${orgId}/sso-providers`, data);
    },

    deleteSSOProvider: async (orgId: number, provider: string): Promise<void> => {
        await api.delete(`/organizations/${orgId}/sso-providers/${provider}`);
    },
};
