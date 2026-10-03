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

    /** Returns a fresh token when the session was one opened with the default password. */
    changePassword: async (oldPassword: string, newPassword: string): Promise<{ token?: string }> => {
        const res = await api.put<{ token?: string }>('/auth/me/password', { old_password: oldPassword, new_password: newPassword });
        return res.data ?? {};
    },
};
