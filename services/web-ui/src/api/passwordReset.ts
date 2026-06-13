import api from './client';

export const passwordResetApi = {
    forgot: async (email: string): Promise<void> => {
        await api.post('/auth/forgot-password', { email });
    },

    reset: async (token: string, password: string): Promise<void> => {
        await api.post('/auth/reset-password', { token, password });
    },
};
