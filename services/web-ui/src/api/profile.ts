import api from './client';

export interface UserProfile {
    id: number;
    username: string;
    full_name: string | null;
    email: string | null;
    role: string;
    org_id: number | null;
}

export const profileApi = {
    get: async (): Promise<UserProfile> => {
        const r = await api.get('/auth/me');
        return r.data;
    },

    changePassword: async (oldPassword: string, newPassword: string): Promise<void> => {
        await api.put('/auth/me/password', { old_password: oldPassword, new_password: newPassword });
    },
};
