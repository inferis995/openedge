import api from './client';

export interface GlobalSettings {
    publish_mode: string;
    rbe_heartbeat_seconds: string;
    rbe_deadband_percent: string;
}

export interface UpdateSettingsRequest {
    publish_mode?: string;
    rbe_heartbeat_seconds?: number;
    rbe_deadband_percent?: number;
}

export interface PublishMetrics {
    published: number;
    skipped: number;
    saved_percent: number;
}

export const systemApi = {
    reload: async (): Promise<void> => {
        await api.post('/system/reload');
    },

    getSettings: async (): Promise<GlobalSettings> => {
        const response = await api.get('/system/settings');
        return response.data;
    },

    updateSettings: async (settings: UpdateSettingsRequest): Promise<void> => {
        await api.put('/system/settings', settings);
    },

    getMetrics: async (): Promise<PublishMetrics> => {
        const response = await api.get('/system/metrics');
        return response.data;
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
    },

    exportBackup: async (): Promise<Blob> => {
        const response = await api.get('/system/backup', { responseType: 'blob' });
        return response.data;
    },

    restoreBackup: async (file: File): Promise<void> => {
        const formData = new FormData();
        formData.append('file', file);
        await api.post('/system/restore', formData, {
            headers: {
                'Content-Type': 'multipart/form-data',
            },
        });
    }
};
