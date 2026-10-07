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
    // The organization's logo as a data URL, or null when it has none.
    getLogo: async (id: number): Promise<string | null> => {
        const response = await api.get(`/organizations/${id}/logo`, {
            responseType: 'blob',
            // No logo is an answer, not an error.
            validateStatus: s => (s >= 200 && s < 300) || s === 404,
        });
        if (response.status === 404 || !(response.data instanceof Blob) || response.data.size === 0) return null;
        const blob = response.data as Blob;
        return new Promise((resolve, reject) => {
            const r = new FileReader();
            r.onload = () => resolve(typeof r.result === 'string' ? r.result : null);
            r.onerror = () => reject(r.error);
            r.readAsDataURL(blob);
        });
    },

    setLogo: async (id: number, file: File): Promise<void> => {
        const form = new FormData();
        form.append('file', file);
        await api.put(`/organizations/${id}/logo`, form, { headers: { 'Content-Type': 'multipart/form-data' } });
    },

    deleteLogo: async (id: number): Promise<void> => {
        await api.delete(`/organizations/${id}/logo`);
    },

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

    // Ogni download registra una scatola nuova: agentName è il nome con cui
    // comparirà nell'elenco e nel selettore dei gateway.
    downloadEdgeInstaller: async (id: number, orgName: string, agentName?: string): Promise<void> => {
        const response = await api.get(`/organizations/${id}/edge-installer`, {
            responseType: 'blob',
            params: agentName ? { agent_name: agentName } : undefined,
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
