import { useState, useEffect } from 'react';
import { systemApi, GlobalSettings, UpdateSettingsRequest, BackupSettings, BackupFileInfo, ServiceStatus } from '@/api/system';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { Slider } from '@/components/ui/slider';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Switch } from '@/components/ui/switch';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Badge } from '@/components/ui/badge';

import {
    Download, AlertTriangle, CheckCircle, RefreshCw, Zap, ScrollText,
    ChevronDown, Settings2, Clock, Trash2, FileArchive,
    HardDrive, Server, Network, Eye, EyeOff, User, Key
} from 'lucide-react';
import { Input } from '@/components/ui/input';

const PUBLISH_MODES = [
    {
        value: 'dual',
        label: 'Dual (Legacy + Sparkplug B)',
        icon: RefreshCw,
        description: 'Pubblica in entrambi i formati per massima compatibilità',
        tooltip: 'Pubblica ogni aggiornamento in entrambi i formati. Ideale per ambienti misti con SCADA legacy e sistemi Sparkplug B.'
    },
    {
        value: 'sparkplug_only',
        label: 'Solo Sparkplug B',
        icon: Zap,
        description: 'Ottimizza la banda con Report by Exception (RBE)',
        tooltip: 'Standard industriale Eclipse Sparkplug B. Pubblica solo quando il valore cambia, riducendo drasticamente il traffico di rete.'
    },
    {
        value: 'legacy_only',
        label: 'Solo Legacy JSON',
        icon: ScrollText,
        description: 'Formato compatibile con sistemi esistenti',
        tooltip: 'Pubblica solo nel formato JSON interno. Per sistemi che non supportano Sparkplug B o per debug.'
    }
];

const INTERVAL_OPTIONS = [
    { value: '6h', label: 'Ogni 6 ore' },
    { value: '12h', label: 'Ogni 12 ore' },
    { value: '24h', label: 'Ogni 24 ore' },
    { value: '7d', label: 'Ogni settimana' }
];

const RETENTION_OPTIONS = [
    { value: 3, label: '3 giorni' },
    { value: 7, label: '7 giorni' },
    { value: 14, label: '14 giorni' },
    { value: 30, label: '30 giorni' }
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
    return date.toLocaleString('it-IT', {
        day: '2-digit',
        month: '2-digit',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit'
    });
};

const SystemPage = () => {
    const [loading, setLoading] = useState(false);
    const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);

    // MQTT Settings
    const [settings, setSettings] = useState<GlobalSettings | null>(null);
    const [settingsLoading, setSettingsLoading] = useState(true);
    const [publishMode, setPublishMode] = useState<string>('dual');
    const [heartbeat, setHeartbeat] = useState<number>(60);
    const [deadband, setDeadband] = useState<number>(0.5);
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

    // Backup Settings
    const [backupSettings, setBackupSettings] = useState<BackupSettings>({
        enabled: false,
        interval: '24h',
        backup_type: 'full',
        retention: 7,
        next_run: '',
        last_run: '',
        last_status: ''
    });
    const [backupList, setBackupList] = useState<BackupFileInfo[]>([]);

    // Post-restore state
    const [postRestoreLoading, setPostRestoreLoading] = useState<boolean>(false);
    const [postRestoreResults, setPostRestoreResults] = useState<ServiceStatus[] | null>(null);

    useEffect(() => {
        loadSettings();
        loadBackupSettings();
        loadBackupList();
    }, []);

    const loadSettings = async () => {
        try {
            const data = await systemApi.getSettings();
            setSettings(data);
            setPublishMode(data.publish_mode || 'dual');
            const parsedHeartbeat = parseInt(data.rbe_heartbeat_seconds);
            setHeartbeat(isNaN(parsedHeartbeat) ? 60 : parsedHeartbeat);
            const parsedDeadband = parseFloat(data.rbe_deadband_percent);
            setDeadband(isNaN(parsedDeadband) ? 0.5 : parsedDeadband);
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

        } catch (error) {
            console.error('Failed to load settings:', error);
        } finally {
            setSettingsLoading(false);
        }
    };

    const loadBackupSettings = async () => {
        try {
            const data = await systemApi.getBackupSettings();
            setBackupSettings(data || backupSettings);
        } catch (error) {
            console.error('Failed to load backup settings:', error);
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
            setMessage({ type: 'success', text: 'Configurazione salvata. Riavviare i servizi per applicare le modifiche al broker MQTT.' });
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: 'Errore nel salvataggio della configurazione.' });
        } finally {
            setLoading(false);
        }
    };

    const handleBackup = async () => {
        setLoading(true);
        setMessage({ type: 'success', text: 'Generazione backup in corso...' });
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
            setMessage({ type: 'success', text: 'Backup completato e scaricato.' });
            loadBackupList();
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: 'Errore nella creazione del backup.' });
        } finally {
            setLoading(false);
        }
    };

    const handleRestore = async (e: React.ChangeEvent<HTMLInputElement>) => {
        if (!e.target.files || !e.target.files[0]) return;
        const file = e.target.files[0];
        if (confirm('ATTENZIONE: Il ripristino sovrascriverà la configurazione attuale. Questa operazione non può essere annullata. Continuare?')) {
            setLoading(true);
            setMessage({ type: 'success', text: 'Ripristino in corso...' });
            try {
                await systemApi.restoreBackup(file);
                setMessage({ type: 'success', text: 'Sistema ripristinato. Ricaricamento...' });
                setTimeout(() => window.location.reload(), 2000);
            } catch (error) {
                console.error(error);
                setMessage({ type: 'error', text: 'Errore nel ripristino del backup.' });
            } finally {
                setLoading(false);
            }
        } else {
            e.target.value = '';
        }
    };

    const handlePostRestore = async () => {
        if (!confirm('Questo riavvierà tutti i servizi nell\'ordine corretto. Continuare?')) return;

        setPostRestoreLoading(true);
        setPostRestoreResults(null);
        setMessage({ type: 'success', text: 'Riavvio servizi in corso...' });

        try {
            const response = await systemApi.postRestoreRestart();
            setPostRestoreResults(response.steps);
            setMessage({ type: 'success', text: response.message });
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: 'Errore durante il riavvio dei servizi.' });
        } finally {
            setPostRestoreLoading(false);
        }
    };

    const handleSaveBackupSettings = async () => {
        setLoading(true);
        setMessage(null);
        try {
            await systemApi.updateBackupSettings(backupSettings);
            setMessage({ type: 'success', text: 'Impostazioni backup automatico salvate.' });
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: 'Errore nel salvataggio delle impostazioni backup.' });
        } finally {
            setLoading(false);
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
            setMessage({ type: 'error', text: 'Errore nel download del backup.' });
        }
    };

    const handleDeleteBackup = async (filename: string) => {
        if (confirm(`Eliminare il backup ${filename}?`)) {
            try {
                await systemApi.deleteBackup(filename);
                setBackupList(backupList.filter(b => b.filename !== filename));
                setMessage({ type: 'success', text: 'Backup eliminato.' });
            } catch (error) {
                console.error(error);
                setMessage({ type: 'error', text: 'Errore nell\'eliminazione del backup.' });
            }
        }
    };

    return (
        <div className="min-h-full bg-background">
            {/* Page header */}
            <div className="bg-card border-b border-border px-6 py-5">
                <div className="max-w-5xl mx-auto flex items-center justify-between">
                    <div>
                        <h1 className="text-xl font-semibold text-foreground">System Manager</h1>
                        <p className="text-sm text-muted-foreground mt-0.5">
                            Configurazione e gestione della piattaforma industriale
                        </p>
                    </div>
                    <div className="flex items-center gap-2 px-3 py-1.5 bg-primary/10 border border-primary/20 clip-chamfer-sm">
                        <div className="w-2 h-2 bg-primary clip-hex animate-pulse" />
                        <span className="text-[10px] tracking-widest uppercase font-bold text-primary">Sistema operativo</span>
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

                {/* Main grid */}
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 items-start">

                    {/* Left Column */}
                    <div className="space-y-6">
                        {/* MQTT Broker Configuration */}
                        <Card className="border-border shadow-sm bg-card">
                            <CardHeader className="pb-4 border-b border-border">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                        <Server className="h-4 w-4 text-primary" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-foreground">Broker MQTT</CardTitle>
                                        <CardDescription className="text-xs mt-0.5">
                                            Seleziona il broker MQTT per la pubblicazione
                                        </CardDescription>
                                    </div>
                                </div>
                            </CardHeader>
                            <CardContent className="pt-5 space-y-5">
                                {settingsLoading ? (
                                    <div className="text-sm text-muted-foreground py-4 text-center">Caricamento...</div>
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
                                                            Broker Interno (Mosquitto)
                                                        </span>
                                                    </div>
                                                    <p className="text-xs text-muted-foreground mt-1">
                                                        Broker embedded accessibile su porta 1883
                                                    </p>
                                                    {mqttBrokerMode === 'internal' && (
                                                        <p className="text-xs text-primary mt-1.5 italic">
                                                            Ascolta su 0.0.0.0:1883 — accessibile dalla rete locale
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
                                                            Broker Esterno
                                                        </span>
                                                    </div>
                                                    <p className="text-xs text-muted-foreground mt-1">
                                                        Utilizza un broker MQTT esistente
                                                    </p>
                                                </div>
                                            </label>
                                        </RadioGroup>

                                        {/* External Broker Settings */}
                                        {mqttBrokerMode === 'external' && (
                                            <div className="space-y-4 pt-3 border-t border-border">
                                                {/* Connection Settings */}
                                                <div className="grid grid-cols-2 gap-3">
                                                    <div className="space-y-2">
                                                        <Label className="text-xs text-muted-foreground flex items-center gap-1">
                                                            <Network className="h-3 w-3" />
                                                            Host
                                                        </Label>
                                                        <Input
                                                            value={mqttExternalHost}
                                                            onChange={(e) => setMqttExternalHost(e.target.value)}
                                                            placeholder="192.168.1.100"
                                                            className="h-9"
                                                        />
                                                    </div>
                                                    <div className="space-y-2">
                                                        <Label className="text-xs text-muted-foreground">Porta</Label>
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
                                                        Autenticazione (opzionale)
                                                    </div>
                                                    <div className="grid grid-cols-2 gap-3">
                                                        <div className="space-y-2">
                                                            <Label className="text-xs text-muted-foreground flex items-center gap-1">
                                                                <User className="h-3 w-3" />
                                                                Username
                                                            </Label>
                                                            <Input
                                                                value={mqttUsername}
                                                                onChange={(e) => setMqttUsername(e.target.value)}
                                                                placeholder="utente"
                                                                className="h-9"
                                                                autoComplete="off"
                                                            />
                                                        </div>
                                                        <div className="space-y-2">
                                                            <Label className="text-xs text-muted-foreground">Password</Label>
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
                                                        <Label className="text-xs text-muted-foreground">Client ID</Label>
                                                        <Input
                                                            value={mqttClientId}
                                                            onChange={(e) => setMqttClientId(e.target.value)}
                                                            placeholder="industrial-edge"
                                                            className="h-9"
                                                        />
                                                        <p className="text-xs text-muted-foreground">Identificativo univoco per la connessione MQTT</p>
                                                    </div>
                                                </div>

                                                <p className="text-xs text-destructive flex items-center gap-1.5">
                                                    <AlertTriangle className="h-3 w-3 flex-shrink-0" />
                                                    Richiede riavvio dei servizi dopo il salvataggio.
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
                                                Cloud Sync (MQTT Forwarder) <Badge variant="secondary" className="text-[10px] uppercase font-mono tracking-wider bg-blue-500/10 text-blue-500 border-none px-1.5 py-0 h-4">Beta</Badge>
                                            </CardTitle>
                                            <CardDescription className="text-xs mt-0.5">
                                                Inoltra automaticamente i dati Sparkplug B a un Cloud remoto (AWS, Azure, ecc.)
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
                                            {cloudSyncEnabled ? 'Attivo' : 'Disattivo'}
                                        </Label>
                                    </div>
                                </div>
                            </CardHeader>
                            {cloudSyncEnabled && (
                                <CardContent className="pt-5 space-y-4">
                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                        <div className="space-y-2">
                                            <Label className="text-xs text-muted-foreground flex items-center gap-1">
                                                <Network className="h-3 w-3" />
                                                Host o Dominio Cloud
                                            </Label>
                                            <Input
                                                value={cloudMqttHost}
                                                onChange={(e) => setCloudMqttHost(e.target.value)}
                                                placeholder="es. a1b2c3d4.iot.eu-central-1.amazonaws.com"
                                                className="h-9 font-mono text-sm"
                                            />
                                        </div>
                                        <div className="space-y-2">
                                            <Label className="text-xs text-muted-foreground">Porta TLS / TCP</Label>
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
                                            <Label className="text-xs text-muted-foreground">Username (Opzionale)</Label>
                                            <Input
                                                value={cloudMqttUsername}
                                                onChange={(e) => setCloudMqttUsername(e.target.value)}
                                                placeholder="username-cloud"
                                                className="h-9"
                                                autoComplete="off"
                                            />
                                        </div>
                                        <div className="space-y-2">
                                            <Label className="text-xs text-muted-foreground">Password (Opzionale)</Label>
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
                                            <Label className="text-xs text-muted-foreground">Topic di Destinazione (Prefisso)</Label>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="sm"
                                                onClick={() => setCloudMqttTopic('')}
                                                className="h-6 px-2 text-[10px] text-muted-foreground hover:text-foreground"
                                            >
                                                <Trash2 className="h-2.5 w-2.5 mr-1" />
                                                Nessun Prefisso
                                            </Button>
                                        </div>
                                        <Input
                                            value={cloudMqttTopic}
                                            onChange={(e) => setCloudMqttTopic(e.target.value)}
                                            placeholder="es. sorical/data/"
                                            className="h-9 font-mono text-sm"
                                        />
                                        <p className="text-[10px] text-muted-foreground mt-1">
                                            Questo prefisso verrà aggiunto prima di ogni messaggio MQTT inoltrato al Cloud.
                                            {cloudMqttTopic ? (
                                                <>Esempio: <code>{cloudMqttTopic}spBv1.0/DDATA/...</code></>
                                            ) : (
                                                <>Senza prefisso: i messaggi verranno pubblicati con il formato originale (es. <code>spBv1.0/DDATA/...</code>)</>
                                            )}
                                        </p>
                                    </div>
                                </CardContent>
                            )}
                        </Card>
                    </div>

                    {/* Right Column */}
                    <div className="space-y-6">
                        {/* MQTT Publish Mode Configuration */}
                        <Card className="border-border shadow-sm bg-card">
                            <CardHeader className="pb-4 border-b border-border">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                        <RefreshCw className="h-4 w-4 text-primary" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-foreground">Configurazione MQTT</CardTitle>
                                        <CardDescription className="text-xs mt-0.5">
                                            Modalità di pubblicazione per i driver industriali
                                        </CardDescription>
                                    </div>
                                </div>
                            </CardHeader>
                            <CardContent className="pt-5 space-y-5">
                                {settingsLoading ? (
                                    <div className="text-sm text-muted-foreground py-4 text-center">Caricamento...</div>
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
                                                                    {mode.label}
                                                                </span>
                                                            </div>
                                                            <p className="text-xs text-muted-foreground mt-1">{mode.description}</p>
                                                            {isSelected && (
                                                                <p className="text-xs text-primary mt-1.5 italic">{mode.tooltip}</p>
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
                                                Parametri RBE avanzati
                                                <ChevronDown className={`h-3.5 w-3.5 ml-auto transition-transform ${advancedOpen ? 'rotate-180' : ''}`} />
                                            </CollapsibleTrigger>
                                            <CollapsibleContent className="space-y-5 pt-4">
                                                <div className="space-y-3">
                                                    <div className="flex justify-between items-center">
                                                        <Label className="text-sm">Heartbeat</Label>
                                                        <span className="text-sm font-mono text-primary bg-primary/10 px-2 py-0.5 rounded">
                                                            {heartbeat === -1 ? 'Solo cambio' : heartbeat === 0 ? 'Real-Time' : (heartbeat < 60 ? `${heartbeat}s` : `${heartbeat / 60}m`)}
                                                        </span>
                                                    </div>
                                                    <div className="flex gap-1.5 flex-wrap">
                                                        <Button
                                                            type="button"
                                                            variant={heartbeat === 0 ? 'default' : 'outline'}
                                                            size="sm"
                                                            className="h-7 min-w-[44px] text-xs"
                                                            onClick={() => setHeartbeat(0)}
                                                        >
                                                            Real-Time
                                                        </Button>
                                                        <Button
                                                            type="button"
                                                            variant={heartbeat === -1 ? 'default' : 'outline'}
                                                            size="sm"
                                                            className="h-7 min-w-[44px] text-xs"
                                                            onClick={() => setHeartbeat(-1)}
                                                        >
                                                            Solo cambio
                                                        </Button>
                                                        {[10, 30, 60, 120, 300].map((val) => (
                                                            <Button
                                                                key={val}
                                                                type="button"
                                                                variant={heartbeat === val ? 'default' : 'outline'}
                                                                size="sm"
                                                                className="h-7 min-w-[44px] text-xs"
                                                                onClick={() => setHeartbeat(val)}
                                                            >
                                                                {val < 60 ? `${val}s` : `${val / 60}m`}
                                                            </Button>
                                                        ))}
                                                    </div>
                                                    <p className="text-xs text-muted-foreground">
                                                        {heartbeat === -1
                                                            ? 'Pubblica SOLO quando il valore supera la deadband.'
                                                            : heartbeat === 0
                                                                ? 'Pubblica ad ogni singola lettura (Real-Time) disabilitando RBE.'
                                                                : 'Intervallo di pubblicazione forzato anche se il valore non cambia.'}
                                                    </p>
                                                </div>

                                                <div className="space-y-3">
                                                    <div className="flex justify-between items-center">
                                                        <Label className="text-sm">Deadband</Label>
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
                                                    <p className="text-xs text-muted-foreground">Soglia minima di variazione per pubblicare valori analogici.</p>
                                                </div>
                                            </CollapsibleContent>
                                        </Collapsible>

                                        {/* DB Retention Section */}
                                        <div className="pt-4 mt-2 border-t flex flex-col gap-3">
                                            <div className="flex justify-between items-center">
                                                <Label className="text-sm font-semibold text-foreground flex items-center gap-2">
                                                    Ritenzione Storico (TimescaleDB)
                                                </Label>
                                                <span className="text-xs font-mono text-primary bg-primary/10 px-2 py-0.5 rounded">
                                                    {dbRetention === 0 ? 'Infinito' : `${dbRetention} giorni`}
                                                </span>
                                            </div>
                                            <p className="text-xs text-muted-foreground whitespace-pre-wrap">
                                                Giorni di conservazione dei dati storici nel database PostgreSQL. I dati più vecchi verranno eliminati automaticamente per liberare spazio su disco.{'\n'}
                                                <span className="text-destructive font-medium">Attenzione: Valore 0 (Infinito) disabilita la pulizia automatica. Il disco potrebbe riempirsi!</span>
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
                                                Salva configurazione
                                            </Button>
                                            {settings && settings.publish_mode === publishMode && !loading && (
                                                <span className="text-xs text-primary flex items-center gap-1">
                                                    <CheckCircle className="h-3 w-3" />
                                                    Configurazione attiva
                                                </span>
                                            )}
                                        </div>
                                    </>
                                )}
                            </CardContent>
                        </Card>
                    </div>
                </div>

                {/* Second row — Backup section */}
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                    {/* Manual Backup */}
                    <Card className="border-border shadow-sm bg-card">
                        <CardHeader className="pb-4 border-b border-border">
                            <div className="flex items-center gap-3">
                                <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                    <Download className="h-4 w-4 text-primary" />
                                </div>
                                <div>
                                    <CardTitle className="text-base text-foreground">Backup Manuale</CardTitle>
                                    <CardDescription className="text-xs mt-0.5">
                                        Scarica immediatamente un backup
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
                                Scarica Backup
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
                                    <CardTitle className="text-base text-foreground">Ripristino</CardTitle>
                                    <CardDescription className="text-xs mt-0.5">
                                        Carica un backup .zip
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
                                    {postRestoreLoading ? 'Riavvio in corso...' : 'Riavvia Servizi (Post-Restore)'}
                                </Button>
                                {postRestoreResults && (
                                    <div className="mt-3 p-3 bg-muted rounded text-xs space-y-1">
                                        <div className="font-medium mb-2">Stato servizi:</div>
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
                                Il ripristino sovrascrive la configurazione corrente.
                            </p>
                        </CardContent>
                    </Card>
                </div>

                {/* Automatic Backup Section */}
                <Card className="border-border shadow-sm bg-card">
                    <CardHeader className="pb-4 border-b border-border">
                        <div className="flex items-center justify-between">
                            <div className="flex items-center gap-3">
                                <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                                    <Clock className="h-4 w-4 text-primary" />
                                </div>
                                <div>
                                    <CardTitle className="text-base text-foreground">Backup Automatico</CardTitle>
                                    <CardDescription className="text-xs mt-0.5">
                                        Salvataggio programmato su disco locale
                                    </CardDescription>
                                </div>
                            </div>
                            <div className="flex items-center gap-2">
                                <Switch
                                    checked={backupSettings.enabled}
                                    onCheckedChange={(checked) => setBackupSettings({ ...backupSettings, enabled: checked })}
                                />
                                <span className="text-xs text-muted-foreground">{backupSettings.enabled ? 'Attivo' : 'Disattivo'}</span>
                            </div>
                        </div>
                    </CardHeader>
                    <CardContent className="pt-5">
                        <div className={`space-y-4 ${!backupSettings.enabled && 'opacity-50 pointer-events-none'}`}>
                            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                                <div className="space-y-2">
                                    <Label className="text-xs text-muted-foreground">Frequenza</Label>
                                    <Select
                                        value={backupSettings.interval}
                                        onValueChange={(value) => setBackupSettings({ ...backupSettings, interval: value })}
                                    >
                                        <SelectTrigger className="h-9">
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            {INTERVAL_OPTIONS.map(opt => (
                                                <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </div>
                                <div className="space-y-2">
                                    <Label className="text-xs text-muted-foreground">Retention</Label>
                                    <Select
                                        value={backupSettings.retention.toString()}
                                        onValueChange={(value) => setBackupSettings({ ...backupSettings, retention: parseInt(value) })}
                                    >
                                        <SelectTrigger className="h-9">
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            {RETENTION_OPTIONS.map(opt => (
                                                <SelectItem key={opt.value} value={opt.value.toString()}>{opt.label}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </div>
                            </div>

                            {/* Status info */}
                            <div className="flex items-center gap-4 text-xs text-muted-foreground pt-2 border-t border-border">
                                {backupSettings.last_run && (
                                    <span>Ultimo: <span className={backupSettings.last_status === 'success' ? 'text-primary' : 'text-destructive'}>{formatDate(backupSettings.last_run)}</span></span>
                                )}
                                {backupSettings.next_run && backupSettings.enabled && (
                                    <span>Prossimo: <span className="text-primary">{formatDate(backupSettings.next_run)}</span></span>
                                )}
                            </div>

                            <div className="flex items-center gap-3 pt-2">
                                <Button
                                    onClick={handleSaveBackupSettings}
                                    disabled={loading}
                                    size="sm"
                                    className="gap-2 h-8"
                                >
                                    <CheckCircle className="h-3.5 w-3.5" />
                                    Salva impostazioni
                                </Button>
                                <span className="text-xs text-muted-foreground flex items-center gap-1">
                                    <HardDrive className="h-3 w-3" />
                                    Salvataggio in ./backups/
                                </span>
                            </div>
                        </div>
                    </CardContent>
                </Card>

                {/* Backup Files List */}
                {backupList.length > 0 && (
                    <Card className="border-border shadow-sm bg-card">
                        <CardHeader className="pb-4 border-b border-border">
                            <div className="flex items-center gap-3">
                                <div className="w-9 h-9 clip-hex bg-muted border border-border flex items-center justify-center flex-shrink-0">
                                    <FileArchive className="h-4 w-4 text-muted-foreground" />
                                </div>
                                <div>
                                    <CardTitle className="text-base text-foreground">Backup Disponibili</CardTitle>
                                    <CardDescription className="text-xs mt-0.5">
                                        {backupList.length} file salvati su disco
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
                                                        Solo Completo
                                                    </span>
                                                </p>
                                            </div>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                className="h-8 w-8 p-0"
                                                onClick={() => handleDownloadBackup(backup.filename)}
                                            >
                                                <Download className="h-4 w-4 text-muted-foreground" />
                                            </Button>
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                className="h-8 w-8 p-0"
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

                {/* Footer */}
                <div className="pt-6 border-t border-border flex flex-col items-center gap-3">
                    <img src="/logo-dark.png" alt="OpenEdge" className="h-12 w-auto object-contain hidden dark:block" />
                    <img src="/logo-light.png" alt="OpenEdge" className="h-12 w-auto object-contain dark:hidden" />
                    <p className="text-center text-xs text-muted-foreground">
                        Sviluppato da{' '}
                        <span className="font-semibold text-foreground">Giovanni Addeo</span>
                        {' '}— soluzioni IIoT per il monitoraggio e la storicizzazione di impianti industriali in tempo reale.
                    </p>
                </div>

            </div>
        </div>
    );
};

export default SystemPage;
