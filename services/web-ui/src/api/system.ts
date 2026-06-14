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
    // Notification channel settings — flat passthrough from the server's
    // global_settings table. Strings everywhere (the DB type is text);
    // boolean-ish fields come as "true"/"false".
    notif_email_enabled?: string;
    notif_email_smtp_host?: string;
    notif_email_smtp_port?: string;
    notif_email_use_tls?: string;
    notif_email_username?: string;
    notif_email_password?: string;
    notif_email_from?: string;
    notif_email_to?: string;
    notif_telegram_enabled?: string;
    notif_telegram_bot_token?: string;
    notif_telegram_chat_id?: string;
    notif_min_severity?: string;
    notif_on_cleared?: string;
    notif_rate_limit_per_min?: string;
    // Backup schedule + retention + encryption (operator-editable).
    backup_enabled?: string;
    backup_schedule?: string;
    backup_retention_days?: string;
    backup_age_recipient?: string;
    kpi_target_alarms_per_day?: string;
    kpi_target_open_critical?: string;
    kpi_target_bad_quality_1h?: string;
    kpi_target_writes_24h_min?: string;
    kpi_target_recipe_loads_24h_min?: string;
    kpi_target_logins_24h_min?: string;
    // OEE configuration. Senza tag_id la card OEE in dashboard usa
    // fallback euristici; impostando i tag id (>0) passa in modalità reale.
    oee_window_minutes?: string;
    oee_target?: string;
    oee_run_time_tag?: string;
    oee_produced_tag?: string;
    oee_good_tag?: string;
    oee_target_pieces_per_hour?: string;
    deployment_mode?: string; // 'onprem' | 'cloud' (from the server env)
    // InfluxDB v2 export connector
    influx_enabled?: string;
    influx_url?: string;
    influx_token?: string;
    influx_org?: string;
    influx_bucket?: string;
    influx_batch_size?: string;
    influx_flush_interval?: string;
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
    // Flat key→value map of `notif_*` settings — flexible enough to add
    // new channels later without bumping this interface.
    notifications?: Record<string, string>;
    // Same flat-passthrough pattern for backup_* settings.
    backup?: Record<string, string>;
    // KPI target thresholds (kpi_target_*).
    kpi_targets?: Record<string, string>;
    // OEE config (oee_*) — finestra, target, tag_ids di produzione.
    oee?: Record<string, string>;
    // InfluxDB and other integrations (influx_*).
    integrations?: Record<string, string>;
}

// What the operator edits in the Notifications panel. We keep the shape
// flat (one key per setting) to match the backend's notif_* catalog.
export interface NotificationSettings {
    notif_email_enabled?: string;
    notif_email_smtp_host?: string;
    notif_email_smtp_port?: string;
    notif_email_use_tls?: string;
    notif_email_username?: string;
    notif_email_password?: string;
    notif_email_from?: string;
    notif_email_to?: string;
    notif_telegram_enabled?: string;
    notif_telegram_bot_token?: string;
    notif_telegram_chat_id?: string;
    notif_min_severity?: string;
    notif_on_cleared?: string;
    notif_rate_limit_per_min?: string;
}

// Per-channel result of the "Send test" button. ok=false rolls up to a
// red badge + the per-channel error message; partial success (email ok,
// telegram missing chat_id) is rendered as a list rather than a single
// pass/fail so the operator sees which one to fix.
export interface NotificationTestResult {
    ok: boolean;
    errors: string[];
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

    // Save the notification panel as a single PUT. Empty secret values
    // ("password", "token", ...) are filtered out at this layer so the
    // server-side rule "empty secret = leave stored value untouched"
    // works even if a UI bug ever drops the placeholder.
    updateNotifications: async (notifications: NotificationSettings): Promise<void> => {
        const cleaned: Record<string, string> = {};
        Object.entries(notifications).forEach(([k, v]) => {
            if (v === undefined || v === null) return;
            const isSecret = /(password|token|secret|private|api_key)/.test(k);
            if (isSecret && v === '') return;
            cleaned[k] = v;
        });
        await api.put('/system/settings', { notifications: cleaned });
    },

    // Fire a synthetic alarm into every enabled channel and return the
    // per-channel result so the panel can show "email OK, telegram chat
    // not found" rather than a generic failure.
    testNotifications: async (): Promise<NotificationTestResult> => {
        const response = await api.post('/system/notifications/test');
        return response.data;
    },

    // Salva i target KPI come passthrough flat — il backend valida solo
    // che le chiavi inizino con kpi_target_ e fa upsert in global_settings.
    updateKPITargets: async (targets: Record<string, string>): Promise<void> => {
        await api.put('/system/settings', { kpi_targets: targets });
    },

    // Salva la configurazione OEE — finestra, target %, tag IDs.
    // Settando i tag id a 0 si torna in modalità fallback euristica.
    updateOEE: async (oee: Record<string, string>): Promise<void> => {
        await api.put('/system/settings', { oee });
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
