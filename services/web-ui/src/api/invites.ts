import api from './client';

export interface Invite {
    id: number;
    org_id: number;
    email: string;
    role: string;
    token: string;
    expires_at: string;
}

export interface CreateInviteDto {
    email: string;
    role: 'admin' | 'user';
}

export interface AcceptInviteDto {
    token: string;
    username: string;
    password: string;
    full_name?: string;
}

export const invitesApi = {
    create: async (orgId: number, data: CreateInviteDto): Promise<Invite> => {
        const response = await api.post(`/organizations/${orgId}/invites`, data);
        return response.data;
    },

    accept: async (data: AcceptInviteDto): Promise<{ user_id: number; username: string; org_id: number; role: string }> => {
        const response = await api.post('/auth/accept-invite', data);
        return response.data;
    },
};
