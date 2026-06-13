import api from './client';

export interface ApiKey {
    id: number;
    org_id: number;
    name: string;
    key_prefix: string;
    created_at: string;
    last_used_at: string | null;
    revoked_at: string | null;
}

export interface CreateApiKeyResponse extends ApiKey {
    full_key: string; // shown only once
}

export const apiKeysApi = {
    list: async (orgId: number): Promise<ApiKey[]> => {
        const response = await api.get(`/organizations/${orgId}/api-keys`);
        return response.data;
    },

    create: async (orgId: number, name = 'default'): Promise<CreateApiKeyResponse> => {
        const response = await api.post(`/organizations/${orgId}/api-keys`, { name });
        return response.data;
    },

    revoke: async (orgId: number, keyId: number): Promise<void> => {
        await api.delete(`/organizations/${orgId}/api-keys/${keyId}`);
    },
};
