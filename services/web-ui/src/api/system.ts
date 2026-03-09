import api from './client';

export interface GlobalSettings {
    publish_mode: string;
    rbe_heartbeat_seconds: string;
    rbe_deadband_percent: string;
    mqtt_broker_mode?: string;
    mqtt_external_host?: string;
    mqtt_external_port?: string;
    mqtt_username?: string;
    mqtt_password?: string;
    mqtt_client_id?: string;
    db_retention_days?: string;
    cloud_sync_enabled?: string;
    cloud_mqtt_host?: string;
    cloud_mqtt_port?: string;
    cloud_mqtt_username?: string;
    cloud_mqtt_password?: string;
    cloud_mqtt_topic?: string;
}

export interface UpdateSettingsRequest {
    publish_mode?: string;
    rbe_heartbeat_seconds?: number;
    rbe_deadband_percent?: number;
    mqtt_broker_mode?: string;
    mqtt_external_host?: string;
    mqtt_external_port?: number;
    mqtt_username?: string;
    mqtt_password?: string;
    mqtt_client_id?: string;
    db_retention_days?: number;
    cloud_sync_enabled?: boolean;
    cloud_mqtt_host?: string;
    cloud_mqtt_port?: number;
    cloud_mqtt_username?: string;
    cloud_mqtt_password?: string;
    cloud_mqtt_topic?: string;
}

export interface PublishMetrics {
    published: number;
    skipped: number;
    saved_percent: number;
}

export interface BackupSettings {
    enabled: boolean;
    interval: string;
    backup_type: 'full';
    retention: number;
    next_run: string;
    last_run: string;
    last_status: string;
}

export interface BackupFileInfo {
    filename: string;
    size: number;
    created_at: string;
    type: 'full';
}

export interface ServiceStatus {
    name: string;
    status: string; // "healthy", "error"
}

export interface PostRestoreResponse {
    message: string;
    steps: ServiceStatus[];
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

    // Backup download (full backup always)
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
    },

    // Automatic backup settings
    getBackupSettings: async (): Promise<BackupSettings> => {
        const response = await api.get('/system/backup/settings');
        return response.data;
    },

    updateBackupSettings: async (settings: BackupSettings): Promise<void> => {
        await api.put('/system/backup/settings', settings);
    },

    // List available backups
    listBackups: async (): Promise<BackupFileInfo[]> => {
        const response = await api.get('/system/backup/list');
        return response.data;
    },

    // Download a specific backup file
    downloadBackup: async (filename: string): Promise<Blob> => {
        const response = await api.get(`/system/backup/files/${filename}`, { responseType: 'blob' });
        return response.data;
    },

    // Delete a specific backup file
    deleteBackup: async (filename: string): Promise<void> => {
        await api.delete(`/system/backup/files/${filename}`);
    },

    // Post-restore service restart
    postRestoreRestart: async (): Promise<PostRestoreResponse> => {
        const response = await api.post('/system/restore/restart');
        return response.data;
    }
};
