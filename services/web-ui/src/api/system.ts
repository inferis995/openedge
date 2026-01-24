import api from './client';

export const systemApi = {
    reload: async (): Promise<void> => {
        await api.post('/system/reload');
    },

    exportConfig: async (): Promise<Blob> => {
        const response = await api.get('/config/export', { responseType: 'blob' });
        return response.data;
    },

    importConfig: async (file: File): Promise<void> => {
        const formData = new FormData();
        formData.append('file', file);
        await api.post('/config/import', formData, {
            headers: {
                'Content-Type': 'multipart/form-data',
            },
        });
    }
};
