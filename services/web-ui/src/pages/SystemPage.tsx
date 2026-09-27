import { useState, useEffect } from 'react';
import { Trans, useTranslation } from 'react-i18next';
import { systemApi, GlobalSettings, UpdateSettingsRequest, BackupFileInfo, ServiceStatus } from '@/api/system';
import { healthApi } from '@/api/health';
import NotificationsSettings from '@/components/system/NotificationsSettings';
import BackupConfig from '@/components/system/BackupConfig';
import KPITargets from '@/components/system/KPITargets';
import { DBStatsResponse } from '@/api/health';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useAuthStore } from '@/stores/useAuthStore';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { Slider } from '@/components/ui/slider';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs';

import {
    Download, AlertTriangle, CheckCircle, RefreshCw, Zap, ScrollText,
    ChevronDown, Settings2, Trash2, FileArchive,
    HardDrive, Server, Network, Eye, EyeOff, User, Key, Shield, Plus, Copy
} from 'lucide-react';
import { Input } from '@/components/ui/input';
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';

const PUBLISH_MODES = [
    {
        value: 'dual',
        label: 'systemPage.mode_dual',
        icon: RefreshCw,
        description: 'systemPage.mode_dual_desc',
        tooltip: 'systemPage.mode_dual_tip'
    },
    {
        value: 'sparkplug_only',
        label: 'systemPage.mode_sparkplug',
        icon: Zap,
        description: 'systemPage.mode_sparkplug_desc',
        tooltip: 'systemPage.mode_sparkplug_tip'
    },
    {
        value: 'legacy_only',
        label: 'systemPage.mode_legacy',
        icon: ScrollText,
        description: 'systemPage.mode_legacy_desc',
        tooltip: 'systemPage.mode_legacy_tip'
    }
];

const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
};

const formatDate = (dateStr: string): string => {
    if (!dateStr) return '-';
    const date = new Date(dateStr);
    return date.toLocaleString(i18n.language, {
        day: '2-digit',
        month: '2-digit',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit'
    });
};

const SystemPage = () => {
    const { t } = useTranslation();
    const [loading, setLoading] = useState(false);
    const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);

    // MQTT Settings
    const [settings, setSettings] = useState<GlobalSettings | null>(null);
    const [settingsLoading, setSettingsLoading] = useState(true);
    const [publishMode, setPublishMode] = useState<string>('dual');
    const [heartbeat, setHeartbeat] = useState<number>(60);
    const [deadband, setDeadband] = useState<number>(0.5);
    // Validità dei comandi di scrittura, in secondi. Vedi internal/commands.
    const [writeMaxAge, setWriteMaxAge] = useState<number>(30);
    const [advancedOpen, setAdvancedOpen] = useState(false);

    // MQTT Broker Settings
    const [mqttBrokerMode, setMqttBrokerMode] = useState<string>('internal');
    const [mqttExternalHost, setMqttExternalHost] = useState<string>('');
    const [mqttExternalPort, setMqttExternalPort] = useState<number>(1883);
    const [mqttUsername, setMqttUsername] = useState<string>('');
    const [mqttPassword, setMqttPassword] = useState<string>('');
    const [mqttClientId, setMqttClientId] = useState<string>('industrial-edge');
    const [showPassword, setShowPassword] = useState<boolean>(false);
    const [dbRetention, setDbRetention] = useState<number>(30); // Default 30 days
    const [cloudSyncEnabled, setCloudSyncEnabled] = useState<boolean>(false);
    const [cloudMqttHost, setCloudMqttHost] = useState<string>('');
    const [cloudMqttPort, setCloudMqttPort] = useState<number>(1883);
    const [cloudMqttUsername, setCloudMqttUsername] = useState<string>('');
    const [cloudMqttPassword, setCloudMqttPassword] = useState<string>('');
    const [cloudMqttTopic, setCloudMqttTopic] = useState<string>('spBv1.0/EdgeNode/');

    // Backup file list (la config automatic backup vive in BackupConfig
    // component che persiste via flat-passthrough nei backup_* settings).
    const [backupList, setBackupList] = useState<BackupFileInfo[]>([]);

    // Post-restore state
    const [postRestoreLoading, setPostRestoreLoading] = useState<boolean>(false);
    const [postRestoreResults, setPostRestoreResults] = useState<ServiceStatus[] | null>(null);

    // Database stats
    const [dbStats, setDbStats] = useState<DBStatsResponse | null>(null);
    const [dbStatsLoading, setDbStatsLoading] = useState(false);
    const [historianRetentionDays, setHistorianRetentionDays] = useState<number>(365);
    const [historianRetentionSaving, setHistorianRetentionSaving] = useState(false);

    // SSO providers state
    interface SSOProvider {
        id?: number;
        provider: string;
        client_id: string;
        client_secret?: string;
        tenant_id?: string;
        domain_hint?: string;
        enabled: boolean;
        created_at?: string;
    }
    const { selectedOrgId } = useNavigationStore();
    const { isGlobalAdmin } = useAuthStore();
    const [ssoProviders, setSsoProviders] = useState<SSOProvider[]>([]);
    const [ssoLoading, setSsoLoading] = useState(false);
    const [ssoEdit, setSsoEdit] = useState<SSOProvider | null>(null);
    const [ssoSaving, setSsoSaving] = useState(false);
    const [ssoMsg, setSsoMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

    const loadSSOProviders = async () => {
        const orgId = selectedOrgId;
        if (!orgId) return;
        setSsoLoading(true);
        try {
            const token = useAuthStore.getState().token;
            const r = await fetch(`/api/organizations/${orgId}/sso-providers`, {
                headers: { Authorization: `Bearer ${token}` },
            });
            if (r.ok) setSsoProviders(await r.json());
        } catch { /* ignore */ }
        finally { setSsoLoading(false); }
    };

    const saveSSOProvider = async () => {
        if (!ssoEdit || !selectedOrgId) return;
        setSsoSaving(true);
        setSsoMsg(null);
        try {
            const token = useAuthStore.getState().token;
            const r = await fetch(`/api/organizations/${selectedOrgId}/sso-providers`, {
                method: 'POST',
                headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
                body: JSON.stringify(ssoEdit),
            });
            if (!r.ok) throw new Error('save failed');
            setSsoMsg({ type: 'success', text: t('systemPage.sso_saved') });
            setSsoEdit(null);
            loadSSOProviders();
        } catch {
            setSsoMsg({ type: 'error', text: t('systemPage.sso_save_failed') });
        } finally { setSsoSaving(false); }
    };

    const deleteSSOProvider = async (provider: string) => {
        if (!selectedOrgId) return;
        const token = useAuthStore.getState().token;
        await fetch(`/api/organizations/${selectedOrgId}/sso-providers/${provider}`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${token}` },
        });
        loadSSOProviders();
    };

    useEffect(() => {
        loadSettings();
        loadBackupList();
        loadDBStats();
        loadSSOProviders();
        const dbStatsInterval = setInterval(loadDBStats, 60000);
        return () => clearInterval(dbStatsInterval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [selectedOrgId]);

    const loadSettings = async () => {
        try {
            const data = await systemApi.getSettings();
            setSettings(data);
            setPublishMode(data.publish_mode || 'dual');
            const parsedHeartbeat = parseInt(data.rbe_heartbeat_seconds);
            setHeartbeat(isNaN(parsedHeartbeat) ? 60 : parsedHeartbeat);
            const parsedDeadband = parseFloat(data.rbe_deadband_percent);
            setDeadband(isNaN(parsedDeadband) ? 0.5 : parsedDeadband);
            const parsedMaxAge = parseInt(data.write_command_max_age_seconds ?? '');
            setWriteMaxAge(isNaN(parsedMaxAge) ? 30 : parsedMaxAge);
            setMqttBrokerMode(data.mqtt_broker_mode || 'internal');
            if (data.mqtt_external_host) setMqttExternalHost(data.mqtt_external_host);
            if (data.mqtt_external_port) setMqttExternalPort(parseInt(data.mqtt_external_port, 10) || 1883);
            if (data.mqtt_username) setMqttUsername(data.mqtt_username);
            if (data.mqtt_password) setMqttPassword(data.mqtt_password);
            if (data.mqtt_client_id) setMqttClientId(data.mqtt_client_id);

            // Handle db retention
            if (data.db_retention_days !== undefined) {
                const parsedDays = parseInt(data.db_retention_days, 10);
                setDbRetention(isNaN(parsedDays) ? 30 : parsedDays);
            }

            // Handle Cloud Sync
            if (data.cloud_sync_enabled) setCloudSyncEnabled(data.cloud_sync_enabled === 'true');
            if (data.cloud_mqtt_host) setCloudMqttHost(data.cloud_mqtt_host);
            if (data.cloud_mqtt_port) setCloudMqttPort(parseInt(data.cloud_mqtt_port, 10) || 1883);
            if (data.cloud_mqtt_username) setCloudMqttUsername(data.cloud_mqtt_username);
            if (data.cloud_mqtt_password) setCloudMqttPassword(data.cloud_mqtt_password);
            if (data.cloud_mqtt_topic) setCloudMqttTopic(data.cloud_mqtt_topic);

            if (data.historian_retention_days !== undefined) {
                const days = parseInt(data.historian_retention_days, 10);
                setHistorianRetentionDays(isNaN(days) ? 365 : days);
            }

        } catch (error) {
            console.error('Failed to load settings:', error);
        } finally {
            setSettingsLoading(false);
        }
    };

    const loadDBStats = async () => {
        setDbStatsLoading(true);
        try {
            const data = await healthApi.dbStats();
            setDbStats(data);
        } catch (error) {
            console.error('Failed to load DB stats:', error);
        } finally {
            setDbStatsLoading(false);
        }
    };

    const loadBackupList = async () => {
        try {
            const data = await systemApi.listBackups();
            setBackupList(data || []);
        } catch (error) {
            console.error('Failed to load backup list:', error);
            setBackupList([]);
        }
    };

    const handleSaveSettings = async () => {
        setLoading(true);
        setMessage(null);
        try {
            const update: UpdateSettingsRequest = {
                publish_mode: publishMode,
                mqtt_broker_mode: mqttBrokerMode,
                db_retention_days: dbRetention,
            };
            update.rbe_heartbeat_seconds = heartbeat;
            update.rbe_deadband_percent = deadband;
            update.write_command_max_age_seconds = writeMaxAge;

            if (mqttBrokerMode === 'external') {
                update.mqtt_external_host = mqttExternalHost;
                update.mqtt_external_port = mqttExternalPort;
                update.mqtt_username = mqttUsername;
                update.mqtt_password = mqttPassword;
                update.mqtt_client_id = mqttClientId;
            }

            // Always send cloud sync settings
            update.cloud_sync_enabled = cloudSyncEnabled;
            update.cloud_mqtt_host = cloudMqttHost;
            update.cloud_mqtt_port = cloudMqttPort;
            update.cloud_mqtt_username = cloudMqttUsername;
            update.cloud_mqtt_password = cloudMqttPassword;
            update.cloud_mqtt_topic = cloudMqttTopic;

            await systemApi.updateSettings(update);
            setMessage({ type: 'success', text: t('systemPage.settings_saved') });
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: t('systemPage.settings_save_failed') });
        } finally {
            setLoading(false);
        }
    };

    const handleBackup = async () => {
        setLoading(true);
        setMessage({ type: 'success', text: t('systemPage.backup_running') });
        try {
            const blob = await systemApi.exportBackup();
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `backup-${new Date().toISOString().replace(/[:.]/g, '-')}.zip`;
            document.body.appendChild(a);
            a.click();
            window.URL.revokeObjectURL(url);
            document.body.removeChild(a);
            setMessage({ type: 'success', text: t('systemPage.backup_done') });
            loadBackupList();
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: t('systemPage.backup_failed') });
        } finally {
            setLoading(false);
        }
    };

    const handleRestore = async (e: React.ChangeEvent<HTMLInputElement>) => {
        if (!e.target.files || !e.target.files[0]) return;
        const file = e.target.files[0];
        if (await confirmAction({ title: i18n.t('ask.restore'), description: i18n.t('ask.restore_desc'), confirmLabel: i18n.t('ask.restore_go'), destructive: true })) {
            setLoading(true);
            setMessage({ type: 'success', text: t('systemPage.restore_running') });
            try {
                await systemApi.restoreBackup(file);
                setMessage({ type: 'success', text: t('systemPage.restore_done') });
                setTimeout(() => window.location.reload(), 2000);
            } catch (error) {
                console.error(error);
                setMessage({ type: 'error', text: t('systemPage.restore_failed') });
            } finally {
                setLoading(false);
            }
        } else {
            e.target.value = '';
        }
    };

    const handlePostRestore = async () => {
        if (!(await confirmAction({ title: i18n.t('ask.restart_services'), description: i18n.t('ask.restart_services_desc'), confirmLabel: i18n.t('ask.restart') }))) return;

        setPostRestoreLoading(true);
        setPostRestoreResults(null);
        setMessage({ type: 'success', text: t('systemPage.restart_running') });

        try {
            const response = await systemApi.postRestoreRestart();
            setPostRestoreResults(response.steps);
            setMessage({ type: 'success', text: response.message });
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: t('systemPage.restart_failed') });
        } finally {
            setPostRestoreLoading(false);
        }
    };

    const handleDownloadBackup = async (filename: string) => {
        try {
            const blob = await systemApi.downloadBackup(filename);
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            window.URL.revokeObjectURL(url);
            document.body.removeChild(a);
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: t('systemPage.download_failed') });
        }
    };

    const handleSaveHistorianRetention = async () => {
        setHistorianRetentionSaving(true);
        try {
            await systemApi.updateSettings({ historian_retention_days: historianRetentionDays });
            setMessage({ type: 'success', text: t('systemPage.retention_saved') });
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: t('systemPage.retention_save_failed') });
        } finally {
            setHistorianRetentionSaving(false);
        }
    };

    const handleDeleteBackup = async (filename: string) => {
        if (await confirmAction({ title: i18n.t('ask.delete_named', { name: filename }), description: i18n.t('ask.irreversible'), destructive: true })) {
            try {
                await systemApi.deleteBackup(filename);
                setBackupList(backupList.filter(b => b.filename !== filename));
                setMessage({ type: 'success', text: t('systemPage.backup_deleted') });
            } catch (error) {
                console.error(error);
                setMessage({ type: 'error', text: t('systemPage.backup_delete_failed') });
            }
        }
    };

    return (
        <div className="min-h-full bg-background">
            {/* Page header */}
            <div className="bg-card border-b border-border px-6 py-5">
                <div className="max-w-5xl mx-auto flex items-center justify-between">
                    <div>
                        <h1 className="text-xl font-semibold text-foreground">{t('system.title')}</h1>
                        <p className="text-sm text-muted-foreground mt-0.5">
                            {t('system.subtitle')}
                        </p>
                    </div>
                    <div className="flex items-center gap-2 px-3 py-1.5 bg-primary/10 border border-primary/20 clip-chamfer-sm">
                        <div className="w-2 h-2 bg-primary clip-hex animate-pulse" />
                        <span className="text-[10px] tracking-widest uppercase font-bold text-primary">{t('system.status_ok')}</span>
                    </div>
                </div>
            </div>

            <div className="max-w-5xl mx-auto px-6 py-8 space-y-6">

                {/* Alert message */}
                {message && (
                    <div className={`flex items-center gap-3 px-4 py-3 rounded-lg border text-sm font-medium ${message.type === 'success'
                        ? 'bg-primary/10 border-primary/20 text-primary'
                        : 'bg-destructive/10 border-destructive/20 text-destructive'
                        }`}>
                        {message.type === 'success'
                            ? <CheckCircle className="h-4 w-4 flex-shrink-0" />
                            : <AlertTriangle className="h-4 w-4 flex-shrink-0" />
                        }
                        {message.text}
                    </div>
                )}

                {/* Tabs: 4 aree funzionali (MQTT, Notifiche, Backup, Target KPI).
                    Prima era una grid 2-colonne con backup duplicato in 4 punti —
                    ora ogni tab è single-column, layout uniforme, niente duplicati. */}
                <Tabs defaultValue="mqtt">
                    <TabsList className="flex-wrap h-auto">
                        <TabsTrigger value="mqtt">{t('system.tab_mqtt')}</TabsTrigger>
                        <TabsTrigger value="notifications">{t('system.tab_notifications')}</TabsTrigger>
                        <TabsTrigger value="backup">{t('system.tab_backup')}</TabsTrigger>
                        <TabsTrigger value="kpi">{t('system.tab_kpi')}</TabsTrigger>
                        <TabsTrigger value="integrations">{t('systemPage.tab_integrations')}</TabsTrigger>
                        <TabsTrigger value="database">{t('systemPage.tab_database')}</TabsTrigger>
                        {isGlobalAdmin() && (
                            <TabsTrigger value="sso">SSO / OIDC</TabsTrigger>
                        )}
                    </TabsList>

                    <TabsContent value="mqtt" className="space-y-6 mt-4">
                        {/* MQTT Broker Configuration */}
                        <Card className="border-border shadow-sm bg-card">
                            <CardHeader className="pb-4 border-b border-border">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                        <Server className="h-4 w-4 text-primary" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-foreground">{t('systemPage.broker_title')}</CardTitle>
                                        <CardDescription className="text-xs mt-0.5">
                                            {t('systemPage.broker_desc')}
                                        </CardDescription>
                                    </div>
                                </div>
                            </CardHeader>
                            <CardContent className="pt-5 space-y-5">
                                {settingsLoading ? (
                                    <div className="text-sm text-muted-foreground py-4 text-center">{t('common.loading')}</div>
                                ) : (
                                    <>
                                        <RadioGroup
                                            value={mqttBrokerMode}
                                            onValueChange={setMqttBrokerMode}
                                            className="space-y-2"
                                        >
                                            {/* Internal Broker Option */}
                                            <label
                                                htmlFor="broker-internal"
                                                className={`flex items-start gap-3 p-3.5 clip-chamfer border cursor-pointer transition-all ${mqttBrokerMode === 'internal'
                                                    ? 'border-primary bg-primary/5'
                                                    : 'border-border bg-card hover:border-primary/30'
                                                    }`}
                                            >
                                                <RadioGroupItem value="internal" id="broker-internal" className="mt-0.5 flex-shrink-0" />
                                                <div className="flex-1 min-w-0">
                                                    <div className="flex items-center gap-2">
                                                        <Network className={`h-3.5 w-3.5 flex-shrink-0 ${mqttBrokerMode === 'internal' ? 'text-primary' : 'text-muted-foreground'}`} />
                                                        <span className={`text-sm font-medium ${mqttBrokerMode === 'internal' ? 'text-foreground' : 'text-foreground'}`}>
                                                            {t('systemPage.broker_internal')}
                                                        </span>
                                                    </div>
                                                    <p className="text-xs text-muted-foreground mt-1">
                                                        {t('systemPage.broker_internal_desc')}
                                                    </p>
                                                    {mqttBrokerMode === 'internal' && (
                                                        <p className="text-xs text-primary mt-1.5 italic">
                                                            {t('systemPage.broker_internal_listen')}
                                                        </p>
                                                    )}
                                                </div>
                                            </label>

                                            {/* External Broker Option */}
                                            <label
                                                htmlFor="broker-external"
                                                className={`flex items-start gap-3 p-3.5 clip-chamfer border cursor-pointer transition-all ${mqttBrokerMode === 'external'
                                                    ? 'border-primary bg-primary/5'
                                                    : 'border-border bg-card hover:border-primary/30'
                                                    }`}
                                            >
                                                <RadioGroupItem value="external" id="broker-external" className="mt-0.5 flex-shrink-0" />
                                                <div className="flex-1 min-w-0">
                                                    <div className="flex items-center gap-2">
                                                        <Server className={`h-3.5 w-3.5 flex-shrink-0 ${mqttBrokerMode === 'external' ? 'text-primary' : 'text-muted-foreground'}`} />
                                                        <span className={`text-sm font-medium ${mqttBrokerMode === 'external' ? 'text-foreground' : 'text-foreground'}`}>
                                                            {t('systemPage.broker_external')}
                                                        </span>
                                                    </div>
                                                    <p className="text-xs text-muted-foreground mt-1">
                                                        {t('systemPage.broker_external_desc')}
                                                    </p>
                                                </div>
                                            </label>
                                        </RadioGroup>

                                        {/* External Broker Settings */}
                                        {mqttBrokerMode === 'external' && (
                                            <div className="space-y-4 pt-3 border-t border-border">
                                                {/* Connection Settings */}
                                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                                    <div className="space-y-2">
                                                        <Label className="text-xs text-muted-foreground flex items-center gap-1">
                                                            <Network className="h-3 w-3" />
                                                            {t('systemPage.host')}
                                                        </Label>
                                                        <Input
                                                            value={mqttExternalHost}
                                                            onChange={(e) => setMqttExternalHost(e.target.value)}
                                                            placeholder="192.168.1.100"
                                                            className="h-9"
                                                        />
                                                    </div>
                                                    <div className="space-y-2">
                                                        <Label className="text-xs text-muted-foreground">{t('systemPage.port')}</Label>
                                                        <Input
                                                            type="number"
                                                            value={mqttExternalPort}
                                                            onChange={(e) => setMqttExternalPort(parseInt(e.target.value) || 1883)}
                                                            placeholder="1883"
                                                            className="h-9"
                                                        />
                                                    </div>
                                                </div>

                                                {/* Authentication Settings */}
                                                <div className="bg-muted/50 clip-chamfer p-3 space-y-3">
                                                    <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground mb-2">
                                                        <Key className="h-3.5 w-3.5" />
                                                        {t('systemPage.auth_optional')}
                                                    </div>
                                                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                                        <div className="space-y-2">
                                                            <Label className="text-xs text-muted-foreground flex items-center gap-1">
                                                                <User className="h-3 w-3" />
                                                                {t('systemPage.username')}
                                                            </Label>
                                                            <Input
                                                                value={mqttUsername}
                                                                onChange={(e) => setMqttUsername(e.target.value)}
                                                                placeholder={t('systemPage.username_ph')}
                                                                className="h-9"
                                                                autoComplete="off"
                                                            />
                                                        </div>
                                                        <div className="space-y-2">
                                                            <Label className="text-xs text-muted-foreground">{t('systemPage.password')}</Label>
                                                            <div className="relative">
                                                                <Input
                                                                    type={showPassword ? "text" : "password"}
                                                                    value={mqttPassword}
                                                                    onChange={(e) => setMqttPassword(e.target.value)}
                                                                    placeholder="••••••••"
                                                                    className="h-9 pr-9"
                                                                    autoComplete="new-password"
                                                                />
                                                                <button
                                                                    type="button"
                                                                    onClick={() => setShowPassword(!showPassword)}
                                                                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                                                                >
                                                                    {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                                                                </button>
                                                            </div>
                                                        </div>
                                                    </div>
                                                    <div className="space-y-2">
                                                        <Label className="text-xs text-muted-foreground">{t('systemPage.client_id')}</Label>
                                                        <Input
                                                            value={mqttClientId}
                                                            onChange={(e) => setMqttClientId(e.target.value)}
                                                            placeholder="industrial-edge"
                                                            className="h-9"
                                                        />
                                                        <p className="text-xs text-muted-foreground">{t('systemPage.client_id_hint')}</p>
                                                    </div>
                                                </div>

                                                <p className="text-xs text-destructive flex items-center gap-1.5">
                                                    <AlertTriangle className="h-3 w-3 flex-shrink-0" />
                                                    {t('systemPage.restart_needed')}
                                                </p>
                                            </div>
                                        )}
                                    </>
                                )}
                            </CardContent>
                        </Card>

                        {/* Cloud Sync (Forwarder) Card */}
                        <Card className="border-border shadow-sm bg-card">
                            <CardHeader className="pb-4 border-b border-border">
                                <div className="flex items-center justify-between">
                                    <div className="flex items-center gap-3">
                                        <div className="w-9 h-9 clip-hex bg-blue-500/10 border border-blue-500/20 flex items-center justify-center flex-shrink-0">
                                            <Server className="h-4 w-4 text-blue-500" />
                                        </div>
                                        <div>
                                            <CardTitle className="text-base text-foreground flex items-center gap-2">
                                                {t('systemPage.cloud_title')} <Badge variant="secondary" className="text-[10px] uppercase font-mono tracking-wider bg-blue-500/10 text-blue-500 border-none px-1.5 py-0 h-4">Beta</Badge>
                                            </CardTitle>
                                            <CardDescription className="text-xs mt-0.5">
                                                {t('systemPage.cloud_desc')}
                                            </CardDescription>
                                        </div>
                                    </div>
                                    <div className="flex items-center gap-2">
                                        <Switch
                                            checked={cloudSyncEnabled}
                                            onCheckedChange={setCloudSyncEnabled}
                                            id="cloud-sync-toggle"
                                        />
                                        <Label htmlFor="cloud-sync-toggle" className="text-xs text-muted-foreground cursor-pointer">
                                            {cloudSyncEnabled ? t('systemPage.on') : t('systemPage.off')}
                                        </Label>
                                    </div>
                                </div>
                            </CardHeader>
                                <CardContent className="pt-5 space-y-4">
                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                        <div className="space-y-2">
                                            <Label className="text-xs text-muted-foreground flex items-center gap-1">
                                                <Network className="h-3 w-3" />
                                                {t('systemPage.cloud_host')}
                                            </Label>
                                            <Input
                                                value={cloudMqttHost}
                                                onChange={(e) => setCloudMqttHost(e.target.value)}
                                                placeholder={t('systemPage.cloud_host_ph')}
                                                className="h-9 font-mono text-sm"
                                            />
                                        </div>
                                        <div className="space-y-2">
                                            <Label className="text-xs text-muted-foreground">{t('systemPage.cloud_port')}</Label>
                                            <Input
                                                type="number"
                                                value={cloudMqttPort}
                                                onChange={(e) => setCloudMqttPort(parseInt(e.target.value) || 8883)}
                                                placeholder="8883"
                                                className="h-9 font-mono text-sm"
                                            />
                                        </div>
                                    </div>

                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                        <div className="space-y-2">
                                            <Label className="text-xs text-muted-foreground">{t('systemPage.cloud_username')}</Label>
                                            <Input
                                                value={cloudMqttUsername}
                                                onChange={(e) => setCloudMqttUsername(e.target.value)}
                                                placeholder="username-cloud"
                                                className="h-9"
                                                autoComplete="off"
                                            />
                                        </div>
                                        <div className="space-y-2">
                                            <Label className="text-xs text-muted-foreground">{t('systemPage.cloud_password')}</Label>
                                            <div className="relative">
                                                <Input
                                                    type={showPassword ? "text" : "password"}
                                                    value={cloudMqttPassword}
                                                    onChange={(e) => setCloudMqttPassword(e.target.value)}
                                                    placeholder="••••••••••••••••"
                                                    className="h-9 pr-10"
                                                    autoComplete="off"
                                                />
                                                <Button
                                                    type="button"
                                                    variant="ghost"
                                                    size="icon"
                                                    className="absolute right-0 top-0 h-9 w-9 hover:bg-transparent"
                                                    onClick={() => setShowPassword(!showPassword)}
                                                >
                                                    {showPassword ? (
                                                        <EyeOff className="h-4 w-4 text-muted-foreground" />
                                                    ) : (
                                                        <Eye className="h-4 w-4 text-muted-foreground" />
                                                    )}
                                                </Button>
                                            </div>
                                        </div>
                                    </div>

                                    <div className="space-y-2 pt-2 border-t border-border mt-4">
                                        <div className="flex items-center justify-between gap-2">
                                            <Label className="text-xs text-muted-foreground">{t('systemPage.cloud_topic')}</Label>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="sm"
                                                onClick={() => setCloudMqttTopic('')}
                                                className="h-9 sm:h-6 px-2 text-[10px] text-muted-foreground hover:text-foreground"
                                            >
                                                <Trash2 className="h-2.5 w-2.5 mr-1" />
                                                {t('systemPage.cloud_no_prefix')}
                                            </Button>
                                        </div>
                                        <Input
                                            value={cloudMqttTopic}
                                            onChange={(e) => setCloudMqttTopic(e.target.value)}
                                            placeholder={t('systemPage.cloud_topic_ph')}
                                            className="h-9 font-mono text-sm"
                                        />
                                        <p className="text-[10px] text-muted-foreground mt-1">
                                            {t('systemPage.cloud_topic_hint')}
                                            {cloudMqttTopic ? (
                                                <>{t('systemPage.example')} <code>{cloudMqttTopic}spBv1.0/DDATA/...</code></>
                                            ) : (
                                                <>{t('systemPage.cloud_no_prefix_hint')} <code>spBv1.0/DDATA/...</code>)</>
                                            )}
                                        </p>
                                    </div>
                                </CardContent>
                        </Card>

                        {/* MQTT Publish Mode Configuration */}
                        <Card className="border-border shadow-sm bg-card">
                            <CardHeader className="pb-4 border-b border-border">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                        <RefreshCw className="h-4 w-4 text-primary" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-foreground">{t('systemPage.publish_title')}</CardTitle>
                                        <CardDescription className="text-xs mt-0.5">
                                            {t('systemPage.publish_desc')}
                                        </CardDescription>
                                    </div>
                                </div>
                            </CardHeader>
                            <CardContent className="pt-5 space-y-5">
                                {settingsLoading ? (
                                    <div className="text-sm text-muted-foreground py-4 text-center">{t('common.loading')}</div>
                                ) : (
                                    <>
                                        <RadioGroup
                                            value={publishMode}
                                            onValueChange={setPublishMode}
                                            className="space-y-2"
                                        >
                                            {PUBLISH_MODES.map((mode) => {
                                                const Icon = mode.icon;
                                                const isSelected = publishMode === mode.value;
                                                return (
                                                    <label
                                                        key={mode.value}
                                                        htmlFor={mode.value}
                                                        className={`flex items-start gap-3 p-3.5 clip-chamfer border cursor-pointer transition-all ${isSelected
                                                            ? 'border-primary bg-primary/5'
                                                            : 'border-border bg-card hover:border-primary/30'
                                                            }`}
                                                    >
                                                        <RadioGroupItem value={mode.value} id={mode.value} className="mt-0.5 flex-shrink-0" />
                                                        <div className="flex-1 min-w-0">
                                                            <div className="flex items-center gap-2">
                                                                <Icon className={`h-3.5 w-3.5 flex-shrink-0 ${isSelected ? 'text-primary' : 'text-muted-foreground'}`} />
                                                                <span className={`text-sm font-medium ${isSelected ? 'text-foreground' : 'text-foreground'}`}>
                                                                    {t(mode.label)}
                                                                </span>
                                                            </div>
                                                            <p className="text-xs text-muted-foreground mt-1">{t(mode.description)}</p>
                                                            {isSelected && (
                                                                <p className="text-xs text-primary mt-1.5 italic">{t(mode.tooltip)}</p>
                                                            )}
                                                        </div>
                                                    </label>
                                                );
                                            })}
                                        </RadioGroup>

                                        {/* Advanced RBE options */}
                                        <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
                                            <CollapsibleTrigger className="flex items-center gap-2 text-xs font-medium text-muted-foreground hover:text-foreground w-full py-2 border-t border-border mt-1">
                                                <Settings2 className="h-3.5 w-3.5" />
                                                {t('systemPage.rbe_advanced')}
                                                <ChevronDown className={`h-3.5 w-3.5 ml-auto transition-transform ${advancedOpen ? 'rotate-180' : ''}`} />
                                            </CollapsibleTrigger>
                                            <CollapsibleContent className="space-y-5 pt-4">
                                                <div className="space-y-3">
                                                    <div className="flex justify-between items-center">
                                                        <Label className="text-sm">{t('systemPage.heartbeat')}</Label>
                                                        <span className="text-sm font-mono text-primary bg-primary/10 px-2 py-0.5 rounded">
                                                            {heartbeat === -1 ? t('systemPage.on_change') : heartbeat === 0 ? t('systemPage.realtime') : (heartbeat < 60 ? `${heartbeat}s` : `${heartbeat / 60}m`)}
                                                        </span>
                                                    </div>
                                                    <div className="flex gap-1.5 flex-wrap">
                                                        <Button
                                                            type="button"
                                                            variant={heartbeat === 0 ? 'default' : 'outline'}
                                                            size="sm"
                                                            className="h-9 sm:h-7 min-w-[44px] text-xs"
                                                            onClick={() => setHeartbeat(0)}
                                                        >
                                                            {t('systemPage.realtime')}
                                                        </Button>
                                                        <Button
                                                            type="button"
                                                            variant={heartbeat === -1 ? 'default' : 'outline'}
                                                            size="sm"
                                                            className="h-9 sm:h-7 min-w-[44px] text-xs"
                                                            onClick={() => setHeartbeat(-1)}
                                                        >
                                                            {t('systemPage.on_change')}
                                                        </Button>
                                                        {[10, 30, 60, 120, 300].map((val) => (
                                                            <Button
                                                                key={val}
                                                                type="button"
                                                                variant={heartbeat === val ? 'default' : 'outline'}
                                                                size="sm"
                                                                className="h-9 sm:h-7 min-w-[44px] text-xs"
                                                                onClick={() => setHeartbeat(val)}
                                                            >
                                                                {val < 60 ? `${val}s` : `${val / 60}m`}
                                                            </Button>
                                                        ))}
                                                    </div>
                                                    <p className="text-xs text-muted-foreground">
                                                        {heartbeat === -1
                                                            ? t('systemPage.hb_on_change')
                                                            : heartbeat === 0
                                                                ? t('systemPage.hb_realtime')
                                                                : t('systemPage.hb_interval')}
                                                    </p>
                                                </div>

                                                <div className="space-y-3">
                                                    <div className="flex justify-between items-center">
                                                        <Label className="text-sm">{t('systemPage.deadband')}</Label>
                                                        <span className="text-sm font-mono text-primary bg-primary/10 px-2 py-0.5 rounded">{deadband.toFixed(1)}%</span>
                                                    </div>
                                                    <Slider
                                                        value={[deadband * 10]}
                                                        onValueChange={(v) => setDeadband(v[0] / 10)}
                                                        min={0}
                                                        max={50}
                                                        step={1}
                                                        className="w-full"
                                                    />
                                                    <p className="text-xs text-muted-foreground">{t('systemPage.deadband_hint')}</p>
                                                </div>
                                            </CollapsibleContent>
                                        </Collapsible>

                                        {/* Validità dei comandi di scrittura */}
                                        <div className="pt-4 mt-2 border-t flex flex-col gap-2">
                                            <div className="flex justify-between items-center">
                                                <Label htmlFor="write_max_age" className="text-sm font-semibold text-foreground">
                                                    {t('systemPage.write_age')}
                                                </Label>
                                                <span className="text-xs font-mono text-primary bg-primary/10 px-2 py-0.5 rounded">
                                                    {writeMaxAge} s
                                                </span>
                                            </div>
                                            <Input
                                                id="write_max_age"
                                                type="number"
                                                min={5}
                                                max={3600}
                                                value={writeMaxAge}
                                                onChange={(e) => setWriteMaxAge(parseInt(e.target.value) || 0)}
                                                // Corretto all'uscita dal campo, non a ogni tasto: altrimenti
                                                // digitando "15" il primo "1" diventerebbe subito 5.
                                                onBlur={() => setWriteMaxAge((v) => Math.min(3600, Math.max(5, v || 30)))}
                                                className="max-w-[8rem]"
                                            />
                                            <p className="text-xs text-muted-foreground">
                                                {t('systemPage.write_age_hint')}
                                            </p>
                                        </div>

                                        {/* DB Retention Section */}
                                        <div className="pt-4 mt-2 border-t flex flex-col gap-3">
                                            <div className="flex justify-between items-center">
                                                <Label className="text-sm font-semibold text-foreground flex items-center gap-2">
                                                    {t('systemPage.db_retention')}
                                                </Label>
                                                <span className="text-xs font-mono text-primary bg-primary/10 px-2 py-0.5 rounded">
                                                    {dbRetention === 0 ? t('systemPage.forever') : t('systemPage.days_count', { count: dbRetention })}
                                                </span>
                                            </div>
                                            <p className="text-xs text-muted-foreground whitespace-pre-wrap">
                                                {t('systemPage.db_retention_hint')}{'\n'}
                                                <span className="text-destructive font-medium">{t('systemPage.db_retention_warn')}</span>
                                            </p>
                                            <div className="flex gap-2 items-center w-full">
                                                <Input
                                                    type="number"
                                                    min={0}
                                                    max={3650}
                                                    value={dbRetention}
                                                    onChange={(e) => setDbRetention(parseInt(e.target.value) || 0)}
                                                    className="w-full text-sm font-mono"
                                                />
                                            </div>
                                        </div>

                                        <div className="flex items-center gap-3 pt-6 border-t border-border mt-4">
                                            <Button
                                                onClick={handleSaveSettings}
                                                disabled={loading}
                                                className="gap-2 h-9 px-5"
                                            >
                                                <CheckCircle className="h-4 w-4" />
                                                {t('systemPage.save_settings')}
                                            </Button>
                                            {settings && settings.publish_mode === publishMode && !loading && (
                                                <span className="text-xs text-primary flex items-center gap-1">
                                                    <CheckCircle className="h-3 w-3" />
                                                    {t('systemPage.active')}
                                                </span>
                                            )}
                                        </div>
                                    </>
                                )}
                            </CardContent>
                        </Card>
                    </TabsContent>

                    <TabsContent value="backup" className="space-y-6 mt-4">
                        {/* Backup panel — schedule, retention, age encryption.
                            Settings persist via flat-passthrough nei backup_*
                            di global_settings. Unica fonte di verità per la
                            configurazione del backup automatico. */}
                        <BackupConfig
                            initial={settings}
                            onSaved={loadSettings}
                        />

                        {/* Backup manuale + ripristino, riga a 2 colonne */}
                        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                    {/* Manual Backup */}
                    <Card className="border-border shadow-sm bg-card">
                        <CardHeader className="pb-4 border-b border-border">
                            <div className="flex items-center gap-3">
                                <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                    <Download className="h-4 w-4 text-primary" />
                                </div>
                                <div>
                                    <CardTitle className="text-base text-foreground">{t('systemPage.manual_backup')}</CardTitle>
                                    <CardDescription className="text-xs mt-0.5">
                                        {t('systemPage.manual_backup_desc')}
                                    </CardDescription>
                                </div>
                            </div>
                        </CardHeader>
                        <CardContent className="pt-5 space-y-3">
                            <Button
                                onClick={() => handleBackup()}
                                disabled={loading}
                                variant="outline"
                                className="w-full gap-2 h-9"
                            >
                                <Download className="h-4 w-4" />
                                {t('systemPage.download_backup')}
                            </Button>
                        </CardContent>
                    </Card>

                    {/* Restore */}
                    <Card className="border-border shadow-sm bg-card">
                        <CardHeader className="pb-4 border-b border-border">
                            <div className="flex items-center gap-3">
                                <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                    <HardDrive className="h-4 w-4 text-primary" />
                                </div>
                                <div>
                                    <CardTitle className="text-base text-foreground">{t('systemPage.restore_title')}</CardTitle>
                                    <CardDescription className="text-xs mt-0.5">
                                        {t('systemPage.restore_desc')}
                                    </CardDescription>
                                </div>
                            </div>
                        </CardHeader>
                        <CardContent className="pt-5 space-y-3">
                            <Input
                                id="restore-file"
                                type="file"
                                accept=".zip"
                                onChange={handleRestore}
                                disabled={loading}
                                className="text-sm cursor-pointer h-9"
                            />
                            <div className="pt-2 border-t border-border">
                                <Button
                                    onClick={handlePostRestore}
                                    disabled={postRestoreLoading || loading}
                                    variant="outline"
                                    className="w-full gap-2 h-9"
                                >
                                    <RefreshCw className={`h-4 w-4 ${postRestoreLoading ? 'animate-spin' : ''}`} />
                                    {postRestoreLoading ? t('systemPage.restarting') : t('systemPage.restart_services')}
                                </Button>
                                {postRestoreResults && (
                                    <div className="mt-3 p-3 bg-muted rounded text-xs space-y-1">
                                        <div className="font-medium mb-2">{t('systemPage.services_status')}</div>
                                        {postRestoreResults.map((service, idx) => (
                                            <div key={idx} className="flex items-center gap-2">
                                                {service.status === 'healthy' ? (
                                                    <CheckCircle className="h-3 w-3 text-green-500" />
                                                ) : (
                                                    <AlertTriangle className="h-3 w-3 text-red-500" />
                                                )}
                                                <span className={service.status === 'healthy' ? 'text-green-600' : 'text-red-600'}>
                                                    {service.name}
                                                </span>
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </div>
                            <p className="text-xs text-destructive flex items-center gap-1.5">
                                <AlertTriangle className="h-3 w-3 flex-shrink-0" />
                                {t('systemPage.restore_warn')}
                            </p>
                        </CardContent>
                    </Card>
                </div>

                {/* Backup Files List */}
                {backupList.length > 0 && (
                    <Card className="border-border shadow-sm bg-card">
                        <CardHeader className="pb-4 border-b border-border">
                            <div className="flex items-center gap-3">
                                <div className="w-9 h-9 clip-hex bg-muted border border-border flex items-center justify-center flex-shrink-0">
                                    <FileArchive className="h-4 w-4 text-muted-foreground" />
                                </div>
                                <div>
                                    <CardTitle className="text-base text-foreground">{t('systemPage.backups_available')}</CardTitle>
                                    <CardDescription className="text-xs mt-0.5">
                                        {t('systemPage.backups_count', { count: backupList.length })}
                                    </CardDescription>
                                </div>
                            </div>
                        </CardHeader>
                        <CardContent className="pt-4">
                            <div className="space-y-2">
                                {backupList.map((backup) => (
                                    <div key={backup.filename} className="flex items-center justify-between p-3 bg-muted/50 rounded-lg border border-border">
                                        <div className="flex items-center gap-3">
                                            <FileArchive className="h-4 w-4 text-muted-foreground" />
                                            <div>
                                                <p className="text-sm font-medium text-foreground">{backup.filename}</p>
                                                <p className="text-xs text-muted-foreground">
                                                    {formatBytes(backup.size)} • {formatDate(backup.created_at)} •
                                                    <span className="ml-1 text-primary">
                                                        {t('systemPage.full_only')}
                                                    </span>
                                                </p>
                                            </div>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                className="h-10 sm:h-8 w-10 sm:w-8 p-0"
                                                onClick={() => handleDownloadBackup(backup.filename)}
                                            >
                                                <Download className="h-4 w-4 text-muted-foreground" />
                                            </Button>
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                className="h-10 sm:h-8 w-10 sm:w-8 p-0"
                                                onClick={() => handleDeleteBackup(backup.filename)}
                                            >
                                                <Trash2 className="h-4 w-4 text-destructive" />
                                            </Button>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </CardContent>
                    </Card>
                )}
                    </TabsContent>

                    <TabsContent value="notifications" className="mt-4">
                        {/* Notifications panel — email + Telegram, severity
                            filter, rate limit, "send test" per-channel.
                            Self-contained: gestisce il proprio save/state. */}
                        <NotificationsSettings
                            initial={settings ?? undefined}
                            onSaved={loadSettings}
                        />
                    </TabsContent>

                    <TabsContent value="kpi" className="mt-4">
                        {/* Target sui KPI della dashboard. Vuoto = nessun
                            target → valore neutro. */}
                        <KPITargets
                            initial={settings}
                            onSaved={loadSettings}
                        />
                    </TabsContent>

                    <TabsContent value="integrations" className="mt-4">
                        <InfluxDBSettings initial={settings ?? undefined} onSaved={loadSettings} />
                    </TabsContent>

                    <TabsContent value="database" className="space-y-6 mt-4">
                        {/* DB Stats Card */}
                        <Card className="border-border shadow-sm bg-card">
                            <CardHeader className="pb-4 border-b border-border">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                        <HardDrive className="h-4 w-4 text-primary" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-foreground">{t('systemPage.db_stats')}</CardTitle>
                                        <CardDescription className="text-xs mt-0.5">{t('systemPage.db_stats_desc')}</CardDescription>
                                    </div>
                                    <Button variant="ghost" size="icon" className="ml-auto h-10 sm:h-8 w-10 sm:w-8" onClick={loadDBStats} disabled={dbStatsLoading}>
                                        <RefreshCw className={`h-4 w-4 ${dbStatsLoading ? 'animate-spin' : ''}`} />
                                    </Button>
                                </div>
                            </CardHeader>
                            <CardContent className="pt-5 space-y-4">
                                {dbStatsLoading && !dbStats ? (
                                    <div className="text-sm text-muted-foreground py-4 text-center">{t('common.loading')}</div>
                                ) : dbStats ? (
                                    <>
                                        <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
                                            <div className="bg-muted/40 rounded-lg p-3 space-y-1">
                                                <p className="text-xs text-muted-foreground">{t('systemPage.db_size')}</p>
                                                <p className="text-lg font-semibold text-foreground font-mono">{dbStats.db_size_mb.toFixed(1)} MB</p>
                                            </div>
                                            <div className="bg-muted/40 rounded-lg p-3 space-y-1">
                                                <p className="text-xs text-muted-foreground">{t('systemPage.historian_rows')}</p>
                                                <p className="text-lg font-semibold text-foreground font-mono">{dbStats.historian_rows.toLocaleString(i18n.language)}</p>
                                            </div>
                                            <div className="bg-muted/40 rounded-lg p-3 space-y-1">
                                                <p className="text-xs text-muted-foreground">{t('systemPage.historian_size')}</p>
                                                <p className="text-lg font-semibold text-foreground font-mono">{dbStats.historian_size_mb.toFixed(1)} MB</p>
                                            </div>
                                            {dbStats.oldest_ts && (
                                                <div className="bg-muted/40 rounded-lg p-3 space-y-1">
                                                    <p className="text-xs text-muted-foreground">{t('systemPage.oldest')}</p>
                                                    <p className="text-sm font-mono text-foreground">{formatDate(dbStats.oldest_ts)}</p>
                                                </div>
                                            )}
                                            {dbStats.newest_ts && (
                                                <div className="bg-muted/40 rounded-lg p-3 space-y-1">
                                                    <p className="text-xs text-muted-foreground">{t('systemPage.newest')}</p>
                                                    <p className="text-sm font-mono text-foreground">{formatDate(dbStats.newest_ts)}</p>
                                                </div>
                                            )}
                                        </div>
                                        {dbStats.tables && dbStats.tables.length > 0 && (
                                            <div className="mt-4">
                                                <p className="text-xs font-medium text-muted-foreground mb-2">{t('systemPage.top_tables')}</p>
                                                <div className="border border-border rounded-lg overflow-hidden">
                                                    <table className="w-full text-xs">
                                                        <thead className="bg-muted/50">
                                                            <tr>
                                                                <th className="text-left px-3 py-2 text-muted-foreground font-medium">{t('systemPage.col_table')}</th>
                                                                <th className="text-right px-3 py-2 text-muted-foreground font-medium">{t('systemPage.col_rows')}</th>
                                                                <th className="text-right px-3 py-2 text-muted-foreground font-medium">{t('systemPage.col_size')}</th>
                                                            </tr>
                                                        </thead>
                                                        <tbody className="divide-y divide-border">
                                                            {dbStats.tables.map((t, i) => (
                                                                <tr key={i} className="hover:bg-muted/20">
                                                                    <td className="px-3 py-2 font-mono text-foreground">{t.table}</td>
                                                                    <td className="px-3 py-2 text-right text-muted-foreground">{t.rows.toLocaleString(i18n.language)}</td>
                                                                    <td className="px-3 py-2 text-right text-muted-foreground">{t.size_mb.toFixed(2)}</td>
                                                                </tr>
                                                            ))}
                                                        </tbody>
                                                    </table>
                                                </div>
                                            </div>
                                        )}
                                    </>
                                ) : (
                                    <div className="text-sm text-muted-foreground py-4 text-center">{t('systemPage.no_data')}</div>
                                )}
                            </CardContent>
                        </Card>

                        {/* Historian Retention Card */}
                        <Card className="border-border shadow-sm bg-card">
                            <CardHeader className="pb-4 border-b border-border">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 clip-hex bg-amber-500/10 border border-amber-500/20 flex items-center justify-center flex-shrink-0">
                                        <Settings2 className="h-4 w-4 text-amber-500" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-foreground">{t('systemPage.hist_retention')}</CardTitle>
                                        <CardDescription className="text-xs mt-0.5">{t('systemPage.hist_retention_desc')}</CardDescription>
                                    </div>
                                </div>
                            </CardHeader>
                            <CardContent className="pt-5 space-y-4">
                                <div className="flex gap-3 items-center">
                                    <Input
                                        type="number"
                                        min={0}
                                        max={3650}
                                        value={historianRetentionDays}
                                        onChange={(e) => setHistorianRetentionDays(parseInt(e.target.value) || 0)}
                                        className="w-40 font-mono text-sm"
                                    />
                                    <span className="text-sm text-muted-foreground">{t('systemPage.days')}</span>
                                    <Button
                                        onClick={handleSaveHistorianRetention}
                                        disabled={historianRetentionSaving}
                                        size="sm"
                                        className="gap-2"
                                    >
                                        <CheckCircle className="h-3.5 w-3.5" />
                                        {historianRetentionSaving ? t('systemPage.saving') : t('common.save')}
                                    </Button>
                                </div>
                                <p className="text-xs text-muted-foreground">
                                    <Trans i18nKey="systemPage.hist_retention_hint" components={{ code: <code /> }} />
                                </p>
                                <p className="text-xs text-destructive flex items-center gap-1.5">
                                    <AlertTriangle className="h-3 w-3 flex-shrink-0" />
                                    {t('systemPage.hist_retention_warn')}
                                </p>
                            </CardContent>
                        </Card>

                        {/* CLI Backup Card */}
                        <Card className="border-border shadow-sm bg-card">
                            <CardHeader className="pb-4 border-b border-border">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 clip-hex bg-muted border border-border flex items-center justify-center flex-shrink-0">
                                        <FileArchive className="h-4 w-4 text-muted-foreground" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-foreground">{t('systemPage.cli_title')}</CardTitle>
                                        <CardDescription className="text-xs mt-0.5">{t('systemPage.cli_desc')}</CardDescription>
                                    </div>
                                </div>
                            </CardHeader>
                            <CardContent className="pt-5 space-y-3">
                                <div className="bg-muted/50 rounded-lg p-4 space-y-3 font-mono text-xs">
                                    <div>
                                        <p className="text-muted-foreground mb-1">{t('systemPage.cli_backup')}</p>
                                        <code className="text-foreground">./scripts/backup.sh [days_to_keep]</code>
                                    </div>
                                    <div className="border-t border-border pt-3">
                                        <p className="text-muted-foreground mb-1">{t('systemPage.cli_restore')}</p>
                                        <code className="text-foreground">./scripts/restore.sh backups/openedge_YYYYMMDD_HHMMSS.sql.gz</code>
                                    </div>
                                    <div className="border-t border-border pt-3">
                                        <p className="text-muted-foreground mb-1">{t('systemPage.cli_location')}</p>
                                        <code className="text-foreground">./backups/</code>
                                    </div>
                                </div>
                            </CardContent>
                        </Card>
                    </TabsContent>

                    {/* SSO / OIDC Tab */}
                    <TabsContent value="sso" className="space-y-6 mt-4">
                        <Card className="border-border shadow-sm bg-card">
                            <CardHeader className="pb-4 border-b border-border">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                        <Shield className="h-4 w-4 text-primary" />
                                    </div>
                                    <div className="flex-1">
                                        <CardTitle className="text-base text-foreground">{t('systemPage.sso_title')}</CardTitle>
                                        <CardDescription className="text-xs mt-0.5">
                                            {t('systemPage.sso_desc')}
                                        </CardDescription>
                                    </div>
                                    <Button size="sm" variant="outline" onClick={() => setSsoEdit({
                                        provider: 'google',
                                        client_id: '',
                                        client_secret: '',
                                        tenant_id: '',
                                        domain_hint: '',
                                        enabled: true,
                                    })}>
                                        <Plus className="h-4 w-4 mr-1" /> {t('systemPage.sso_add')}
                                    </Button>
                                </div>
                            </CardHeader>
                            <CardContent className="pt-4 space-y-4">
                                {ssoMsg && (
                                    <div className={`flex items-center gap-2 text-sm px-3 py-2 rounded-md border ${ssoMsg.type === 'success' ? 'bg-green-500/10 border-green-500/30 text-green-600' : 'bg-destructive/10 border-destructive/30 text-destructive'}`}>
                                        {ssoMsg.type === 'success' ? <CheckCircle className="h-4 w-4" /> : <AlertTriangle className="h-4 w-4" />}
                                        {ssoMsg.text}
                                    </div>
                                )}

                                {/* Callback URLs info */}
                                <div className="rounded-md bg-muted/40 border border-border p-3 space-y-2">
                                    <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">{t('systemPage.sso_callbacks')}</p>
                                    {(['google', 'azure'] as const).map(p => {
                                        const url = `${window.location.origin}/api/auth/sso/${p}/callback`;
                                        return (
                                            <div key={p} className="flex items-center gap-2">
                                                <span className="text-xs text-muted-foreground w-14 capitalize">{p}:</span>
                                                <code className="text-xs flex-1 truncate text-foreground">{url}</code>
                                                <Button size="sm" variant="ghost" className="h-9 sm:h-6 w-9 sm:w-6 p-0" onClick={() => navigator.clipboard.writeText(url)}>
                                                    <Copy className="h-3 w-3" />
                                                </Button>
                                            </div>
                                        );
                                    })}
                                </div>

                                {/* Configured providers list */}
                                {ssoLoading ? (
                                    <div className="text-center py-6 text-sm text-muted-foreground">{t('common.loading')}</div>
                                ) : ssoProviders.length === 0 ? (
                                    <div className="text-center py-6 text-sm text-muted-foreground">{t('systemPage.sso_empty')}</div>
                                ) : ssoProviders.map(p => (
                                    <div key={p.provider} className="flex items-center justify-between rounded-lg border border-border px-4 py-3">
                                        <div className="flex items-center gap-3">
                                            <Shield className="h-4 w-4 text-primary" />
                                            <div>
                                                <div className="font-medium capitalize text-sm">{p.provider === 'azure' ? 'Microsoft Azure AD' : 'Google'}</div>
                                                <div className="text-xs text-muted-foreground">{t('systemPage.sso_client_id', { id: p.client_id })}</div>
                                                {p.domain_hint && <div className="text-xs text-muted-foreground">{t('systemPage.sso_domain', { domain: p.domain_hint })}</div>}
                                            </div>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <span className={`text-xs font-semibold px-2 py-0.5 rounded-full ${p.enabled ? 'bg-green-500/10 text-green-600' : 'bg-muted text-muted-foreground'}`}>
                                                {p.enabled ? t('systemPage.enabled') : t('systemPage.disabled')}
                                            </span>
                                            <Button size="sm" variant="outline" onClick={() => setSsoEdit({ ...p, client_secret: '' })}>{t('common.edit')}</Button>
                                            <Button size="sm" variant="ghost" className="text-destructive" onClick={() => deleteSSOProvider(p.provider)}>
                                                <Trash2 className="h-4 w-4" />
                                            </Button>
                                        </div>
                                    </div>
                                ))}

                                {/* Edit form */}
                                {ssoEdit && (
                                    <div className="rounded-lg border border-primary/30 bg-primary/5 p-4 space-y-3">
                                        <p className="text-sm font-semibold">
                                            {ssoEdit.id ? t('systemPage.sso_edit_title') : t('systemPage.sso_add_title')}
                                        </p>
                                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                            <div className="space-y-1">
                                                <Label className="text-xs">{t('systemPage.sso_provider')}</Label>
                                                <select
                                                    className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm"
                                                    value={ssoEdit.provider}
                                                    onChange={e => setSsoEdit(s => s ? { ...s, provider: e.target.value } : s)}
                                                    disabled={!!ssoEdit.id}
                                                >
                                                    <option value="google">Google</option>
                                                    <option value="azure">Microsoft Azure AD</option>
                                                </select>
                                            </div>
                                            <div className="space-y-1">
                                                <Label className="text-xs">{t('systemPage.sso_enabled')}</Label>
                                                <div className="flex items-center h-10 sm:h-8">
                                                    <Switch
                                                        checked={ssoEdit.enabled}
                                                        onCheckedChange={v => setSsoEdit(s => s ? { ...s, enabled: v } : s)}
                                                    />
                                                </div>
                                            </div>
                                            <div className="space-y-1">
                                                <Label className="text-xs">{t('systemPage.client_id')} <span className="text-destructive">*</span></Label>
                                                <Input
                                                    value={ssoEdit.client_id}
                                                    onChange={e => setSsoEdit(s => s ? { ...s, client_id: e.target.value } : s)}
                                                    placeholder={t('systemPage.client_id_ph')}
                                                    className="h-10 sm:h-8 text-sm"
                                                />
                                            </div>
                                            <div className="space-y-1">
                                                <Label className="text-xs">{t('systemPage.client_secret')} <span className="text-destructive">*</span></Label>
                                                <Input
                                                    type="password"
                                                    value={ssoEdit.client_secret ?? ''}
                                                    onChange={e => setSsoEdit(s => s ? { ...s, client_secret: e.target.value } : s)}
                                                    placeholder={ssoEdit.id ? t('systemPage.unchanged') : t('systemPage.client_secret_ph')}
                                                    className="h-10 sm:h-8 text-sm"
                                                />
                                            </div>
                                            {ssoEdit.provider === 'azure' && (
                                                <div className="space-y-1">
                                                    <Label className="text-xs">{t('systemPage.tenant_id')}</Label>
                                                    <Input
                                                        value={ssoEdit.tenant_id ?? ''}
                                                        onChange={e => setSsoEdit(s => s ? { ...s, tenant_id: e.target.value } : s)}
                                                        placeholder={t('systemPage.tenant_ph')}
                                                        className="h-10 sm:h-8 text-sm"
                                                    />
                                                </div>
                                            )}
                                            <div className="space-y-1">
                                                <Label className="text-xs">{t('systemPage.domain_hint')}</Label>
                                                <Input
                                                    value={ssoEdit.domain_hint ?? ''}
                                                    onChange={e => setSsoEdit(s => s ? { ...s, domain_hint: e.target.value } : s)}
                                                    placeholder={t('systemPage.domain_hint_ph')}
                                                    className="h-10 sm:h-8 text-sm"
                                                />
                                            </div>
                                        </div>
                                        <div className="flex gap-2 justify-end pt-1">
                                            <Button variant="outline" size="sm" onClick={() => setSsoEdit(null)}>{t('common.cancel')}</Button>
                                            <Button
                                                size="sm"
                                                onClick={saveSSOProvider}
                                                disabled={ssoSaving || !ssoEdit.client_id}
                                            >
                                                {ssoSaving ? t('systemPage.saving') : t('systemPage.sso_save')}
                                            </Button>
                                        </div>
                                    </div>
                                )}
                            </CardContent>
                        </Card>
                    </TabsContent>
                </Tabs>

                {/* Footer */}
                <div className="pt-6 border-t border-border flex flex-col items-center gap-3">
                    <img src="/avatar.png" alt="OpenEdge" className="h-12 w-12 rounded-xl object-cover" />
                    <p className="text-center text-xs text-muted-foreground">
                        {t('systemPage.footer_by')}{' '}
                        <span className="font-semibold text-foreground">Giovanni Addeo</span>
                        {' '}{t('systemPage.footer_tagline')}
                    </p>
                </div>

            </div>
        </div>
    );
};

// InfluxDB v2 integration settings component
const InfluxDBSettings = ({ initial, onSaved }: { initial?: GlobalSettings; onSaved: () => void }) => {
    const { t } = useTranslation();
    const [form, setForm] = useState({
        influx_enabled: initial?.influx_enabled ?? 'false',
        influx_url: initial?.influx_url ?? '',
        influx_token: initial?.influx_token ?? '',
        influx_org: initial?.influx_org ?? '',
        influx_bucket: initial?.influx_bucket ?? '',
        influx_batch_size: initial?.influx_batch_size ?? '500',
        influx_flush_interval: initial?.influx_flush_interval ?? '10',
    });
    const [saving, setSaving] = useState(false);
    const [msg, setMsg] = useState<string | null>(null);

    useEffect(() => {
        if (initial) {
            setForm({
                influx_enabled: initial.influx_enabled ?? 'false',
                influx_url: initial.influx_url ?? '',
                influx_token: initial.influx_token ?? '',
                influx_org: initial.influx_org ?? '',
                influx_bucket: initial.influx_bucket ?? '',
                influx_batch_size: initial.influx_batch_size ?? '500',
                influx_flush_interval: initial.influx_flush_interval ?? '10',
            });
        }
    }, [initial]);

    const save = async () => {
        setSaving(true);
        try {
            await systemApi.updateSettings({
                integrations: {
                    influx_enabled: form.influx_enabled,
                    influx_url: form.influx_url,
                    influx_token: form.influx_token,
                    influx_org: form.influx_org,
                    influx_bucket: form.influx_bucket,
                    influx_batch_size: form.influx_batch_size,
                    influx_flush_interval: form.influx_flush_interval,
                },
            });
            setMsg(t('common.saved'));
            onSaved();
        } catch {
            setMsg(t('systemPage.influx_save_failed'));
        } finally {
            setSaving(false);
            setTimeout(() => setMsg(null), 3000);
        }
    };

    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    <Server className="h-5 w-5" /> {t('systemPage.influx_title')}
                </CardTitle>
                <CardDescription>
                    {t('systemPage.influx_desc')}
                </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
                <div className="flex items-center justify-between">
                    <Label>{t('systemPage.influx_enable')}</Label>
                    <Switch
                        checked={form.influx_enabled === 'true'}
                        onCheckedChange={(v) => setForm(f => ({ ...f, influx_enabled: v ? 'true' : 'false' }))}
                    />
                </div>

                <div className="grid gap-3 opacity-100">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <Label className="text-xs">{t('systemPage.influx_url')}</Label>
                            <Input
                                placeholder="https://influxdb.example.com:8086"
                                value={form.influx_url}
                                onChange={(e) => setForm(f => ({ ...f, influx_url: e.target.value }))}
                            />
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs">{t('systemPage.influx_token')}</Label>
                            <Input
                                type="password"
                                placeholder={t('systemPage.influx_token_ph')}
                                value={form.influx_token}
                                onChange={(e) => setForm(f => ({ ...f, influx_token: e.target.value }))}
                            />
                        </div>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <Label className="text-xs">{t('common.organization')}</Label>
                            <Input
                                placeholder="my-org"
                                value={form.influx_org}
                                onChange={(e) => setForm(f => ({ ...f, influx_org: e.target.value }))}
                            />
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs">{t('systemPage.influx_bucket')}</Label>
                            <Input
                                placeholder="openedge"
                                value={form.influx_bucket}
                                onChange={(e) => setForm(f => ({ ...f, influx_bucket: e.target.value }))}
                            />
                        </div>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <Label className="text-xs">{t('systemPage.influx_batch')}</Label>
                            <Input
                                type="number"
                                min={1}
                                placeholder="500"
                                value={form.influx_batch_size}
                                onChange={(e) => setForm(f => ({ ...f, influx_batch_size: e.target.value }))}
                            />
                        </div>
                        <div className="space-y-1">
                            <Label className="text-xs">{t('systemPage.influx_flush')}</Label>
                            <Input
                                type="number"
                                min={1}
                                placeholder="10"
                                value={form.influx_flush_interval}
                                onChange={(e) => setForm(f => ({ ...f, influx_flush_interval: e.target.value }))}
                            />
                        </div>
                    </div>
                </div>

                <div className="flex items-center gap-3 pt-2">
                    <Button onClick={save} disabled={saving}>
                        {saving ? t('systemPage.saving') : t('systemPage.influx_save')}
                    </Button>
                    {msg && <span className="text-sm text-muted-foreground">{msg}</span>}
                </div>

                <div className="rounded-md bg-muted/50 border p-3 text-xs text-muted-foreground space-y-1">
                    <p><span className="font-semibold">{t('systemPage.influx_measurement')}</span> <code>openedge_tag</code></p>
                    <p><span className="font-semibold">{t('systemPage.influx_tags')}</span> tag_id, org_id, alias</p>
                    <p><span className="font-semibold">{t('systemPage.influx_fields')}</span> value, quality</p>
                    <p><span className="font-semibold">{t('systemPage.influx_precision')}</span> {t('systemPage.influx_ns')}</p>
                </div>
            </CardContent>
        </Card>
    );
};

export default SystemPage;
