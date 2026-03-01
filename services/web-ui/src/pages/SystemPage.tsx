import { useState, useEffect } from 'react';
import { systemApi, GlobalSettings, UpdateSettingsRequest, BackupSettings, BackupFileInfo } from '@/api/system';

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

import {
    Download, AlertTriangle, CheckCircle, RefreshCw, Zap, ScrollText,
    ChevronDown, Settings2, Shield, Clock, Trash2, FileArchive,
    HardDrive
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

    // Backup Settings
    const [backupSettings, setBackupSettings] = useState<BackupSettings>({
        enabled: false,
        interval: '24h',
        backup_type: 'config',
        retention: 7,
        next_run: '',
        last_run: '',
        last_status: ''
    });
    const [backupList, setBackupList] = useState<BackupFileInfo[]>([]);

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
            setHeartbeat(parseInt(data.rbe_heartbeat_seconds) || 60);
            setDeadband(parseFloat(data.rbe_deadband_percent) || 0.5);
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
            };
            if (publishMode === 'sparkplug_only') {
                update.rbe_heartbeat_seconds = heartbeat;
                update.rbe_deadband_percent = deadband;
            }
            await systemApi.updateSettings(update);
            setMessage({ type: 'success', text: 'Configurazione salvata e applicata automaticamente ai driver.' });
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
        <div className="min-h-full bg-gray-50">
            {/* Page header */}
            <div className="bg-white border-b border-gray-200 px-6 py-5">
                <div className="max-w-5xl mx-auto flex items-center justify-between">
                    <div>
                        <h1 className="text-xl font-semibold text-gray-900">System Manager</h1>
                        <p className="text-sm text-gray-500 mt-0.5">
                            Configurazione e gestione della piattaforma industriale
                        </p>
                    </div>
                    <div className="flex items-center gap-2 px-3 py-1.5 bg-blue-50 border border-blue-200 rounded-full">
                        <div className="w-2 h-2 bg-blue-500 rounded-full animate-pulse" />
                        <span className="text-xs font-medium text-blue-700">Sistema operativo</span>
                    </div>
                </div>
            </div>

            <div className="max-w-5xl mx-auto px-6 py-8 space-y-6">

                {/* Alert message */}
                {message && (
                    <div className={`flex items-center gap-3 px-4 py-3 rounded-lg border text-sm font-medium ${
                        message.type === 'success'
                            ? 'bg-green-50 border-green-200 text-green-800'
                            : 'bg-red-50 border-red-200 text-red-800'
                    }`}>
                        {message.type === 'success'
                            ? <CheckCircle className="h-4 w-4 flex-shrink-0" />
                            : <AlertTriangle className="h-4 w-4 flex-shrink-0" />
                        }
                        {message.text}
                    </div>
                )}

                {/* Main grid */}
                <div className="grid grid-cols-1 lg:grid-cols-5 gap-6">

                    {/* MQTT Configuration — larger column */}
                    <div className="lg:col-span-3">
                        <Card className="border-gray-200 shadow-sm bg-white h-full">
                            <CardHeader className="pb-4 border-b border-gray-100">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 rounded-lg bg-blue-50 border border-blue-100 flex items-center justify-center flex-shrink-0">
                                        <RefreshCw className="h-4 w-4 text-blue-600" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-gray-900">Configurazione MQTT</CardTitle>
                                        <CardDescription className="text-xs mt-0.5">
                                            Modalità di pubblicazione per i driver industriali
                                        </CardDescription>
                                    </div>
                                </div>
                            </CardHeader>
                            <CardContent className="pt-5 space-y-5">
                                {settingsLoading ? (
                                    <div className="text-sm text-gray-400 py-4 text-center">Caricamento...</div>
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
                                                        className={`flex items-start gap-3 p-3.5 rounded-lg border cursor-pointer transition-all ${
                                                            isSelected
                                                                ? 'border-blue-400 bg-blue-50/60'
                                                                : 'border-gray-200 bg-white hover:border-gray-300 hover:bg-gray-50'
                                                        }`}
                                                    >
                                                        <RadioGroupItem value={mode.value} id={mode.value} className="mt-0.5 flex-shrink-0" />
                                                        <div className="flex-1 min-w-0">
                                                            <div className="flex items-center gap-2">
                                                                <Icon className={`h-3.5 w-3.5 flex-shrink-0 ${isSelected ? 'text-blue-600' : 'text-gray-400'}`} />
                                                                <span className={`text-sm font-medium ${isSelected ? 'text-blue-900' : 'text-gray-700'}`}>
                                                                    {mode.label}
                                                                </span>
                                                            </div>
                                                            <p className="text-xs text-gray-500 mt-1">{mode.description}</p>
                                                            {isSelected && (
                                                                <p className="text-xs text-blue-600/80 mt-1.5 italic">{mode.tooltip}</p>
                                                            )}
                                                        </div>
                                                    </label>
                                                );
                                            })}
                                        </RadioGroup>

                                        {/* Advanced RBE options */}
                                        {publishMode === 'sparkplug_only' && (
                                            <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
                                                <CollapsibleTrigger className="flex items-center gap-2 text-xs font-medium text-gray-600 hover:text-gray-900 w-full py-2 border-t border-gray-100 mt-1">
                                                    <Settings2 className="h-3.5 w-3.5" />
                                                    Parametri RBE avanzati
                                                    <ChevronDown className={`h-3.5 w-3.5 ml-auto transition-transform ${advancedOpen ? 'rotate-180' : ''}`} />
                                                </CollapsibleTrigger>
                                                <CollapsibleContent className="space-y-5 pt-4">
                                                    <div className="space-y-3">
                                                        <div className="flex justify-between items-center">
                                                            <Label className="text-sm">Heartbeat</Label>
                                                            <span className="text-sm font-mono text-blue-700 bg-blue-50 px-2 py-0.5 rounded">
                                                                {heartbeat === 0 ? 'Solo cambio' : (heartbeat < 60 ? `${heartbeat}s` : `${heartbeat / 60}m`)}
                                                            </span>
                                                        </div>
                                                        <div className="flex gap-1.5 flex-wrap">
                                                            <Button
                                                                type="button"
                                                                variant={heartbeat === 0 ? 'default' : 'outline'}
                                                                size="sm"
                                                                className={`h-7 min-w-[44px] text-xs ${heartbeat === 0 ? 'bg-green-600 hover:bg-green-700' : ''}`}
                                                                onClick={() => setHeartbeat(0)}
                                                            >
                                                                Solo cambio
                                                            </Button>
                                                            {[10, 30, 60, 120, 300].map((val) => (
                                                                <Button
                                                                    key={val}
                                                                    type="button"
                                                                    variant={heartbeat === val ? 'default' : 'outline'}
                                                                    size="sm"
                                                                    className={`h-7 min-w-[44px] text-xs ${heartbeat === val ? 'bg-blue-600 hover:bg-blue-700' : ''}`}
                                                                    onClick={() => setHeartbeat(val)}
                                                                >
                                                                    {val < 60 ? `${val}s` : `${val / 60}m`}
                                                                </Button>
                                                            ))}
                                                        </div>
                                                        <p className="text-xs text-gray-400">
                                                            {heartbeat === 0
                                                                ? 'Pubblica SOLO quando il valore cambia (massima ottimizzazione banda).'
                                                                : 'Intervallo di pubblicazione periodico anche se il valore non cambia.'}
                                                        </p>
                                                    </div>

                                                    <div className="space-y-3">
                                                        <div className="flex justify-between items-center">
                                                            <Label className="text-sm">Deadband</Label>
                                                            <span className="text-sm font-mono text-blue-700 bg-blue-50 px-2 py-0.5 rounded">{deadband.toFixed(1)}%</span>
                                                        </div>
                                                        <Slider
                                                            value={[deadband * 10]}
                                                            onValueChange={(v) => setDeadband(v[0] / 10)}
                                                            min={0}
                                                            max={50}
                                                            step={1}
                                                            className="w-full"
                                                        />
                                                        <p className="text-xs text-gray-400">Soglia minima di variazione per pubblicare valori analogici.</p>
                                                    </div>
                                                </CollapsibleContent>
                                            </Collapsible>
                                        )}

                                        <div className="flex items-center gap-3 pt-2 border-t border-gray-100">
                                            <Button
                                                onClick={handleSaveSettings}
                                                disabled={loading}
                                                className="gap-2 bg-blue-600 hover:bg-blue-700 text-white h-9 px-5"
                                            >
                                                <CheckCircle className="h-4 w-4" />
                                                Salva configurazione
                                            </Button>
                                            {settings && settings.publish_mode === publishMode && !loading && (
                                                <span className="text-xs text-green-600 flex items-center gap-1">
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

                    {/* Right column — Backup */}
                    <div className="lg:col-span-2 space-y-6">
                        {/* Manual Backup */}
                        <Card className="border-gray-200 shadow-sm bg-white">
                            <CardHeader className="pb-4 border-b border-gray-100">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 rounded-lg bg-emerald-50 border border-emerald-100 flex items-center justify-center flex-shrink-0">
                                        <Download className="h-4 w-4 text-emerald-600" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-gray-900">Backup Manuale</CardTitle>
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
                                    className="w-full gap-2 border-blue-300 text-blue-700 hover:bg-blue-50 h-9"
                                >
                                    <Download className="h-4 w-4" />
                                    Scarica Backup
                                </Button>
                            </CardContent>
                        </Card>

                        {/* Restore */}
                        <Card className="border-gray-200 shadow-sm bg-white">
                            <CardHeader className="pb-4 border-b border-gray-100">
                                <div className="flex items-center gap-3">
                                    <div className="w-9 h-9 rounded-lg bg-amber-50 border border-amber-100 flex items-center justify-center flex-shrink-0">
                                        <Shield className="h-4 w-4 text-amber-600" />
                                    </div>
                                    <div>
                                        <CardTitle className="text-base text-gray-900">Ripristino</CardTitle>
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
                                    className="text-sm file:bg-gray-100 file:text-gray-700 hover:file:bg-gray-200 cursor-pointer h-9"
                                />
                                <p className="text-xs text-amber-700 flex items-center gap-1.5">
                                    <AlertTriangle className="h-3 w-3 flex-shrink-0" />
                                    Il ripristino sovrascrive la configurazione corrente.
                                </p>
                            </CardContent>
                        </Card>
                    </div>
                </div>

                {/* Automatic Backup Section */}
                <Card className="border-gray-200 shadow-sm bg-white">
                    <CardHeader className="pb-4 border-b border-gray-100">
                        <div className="flex items-center justify-between">
                            <div className="flex items-center gap-3">
                                <div className="w-9 h-9 rounded-lg bg-violet-50 border border-violet-100 flex items-center justify-center flex-shrink-0">
                                    <Clock className="h-4 w-4 text-violet-600" />
                                </div>
                                <div>
                                    <CardTitle className="text-base text-gray-900">Backup Automatico</CardTitle>
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
                                <span className="text-xs text-gray-600">{backupSettings.enabled ? 'Attivo' : 'Disattivo'}</span>
                            </div>
                        </div>
                    </CardHeader>
                    <CardContent className="pt-5">
                        <div className={`space-y-4 ${!backupSettings.enabled && 'opacity-50 pointer-events-none'}`}>
                            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                                <div className="space-y-2">
                                    <Label className="text-xs text-gray-500">Frequenza</Label>
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
                                    <Label className="text-xs text-gray-500">Retention</Label>
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
                            <div className="flex items-center gap-4 text-xs text-gray-500 pt-2 border-t border-gray-100">
                                {backupSettings.last_run && (
                                    <span>Ultimo: <span className={backupSettings.last_status === 'success' ? 'text-green-600' : 'text-red-600'}>{formatDate(backupSettings.last_run)}</span></span>
                                )}
                                {backupSettings.next_run && backupSettings.enabled && (
                                    <span>Prossimo: <span className="text-blue-600">{formatDate(backupSettings.next_run)}</span></span>
                                )}
                            </div>

                            <div className="flex items-center gap-3 pt-2">
                                <Button
                                    onClick={handleSaveBackupSettings}
                                    disabled={loading}
                                    size="sm"
                                    className="gap-2 bg-violet-600 hover:bg-violet-700 text-white h-8"
                                >
                                    <CheckCircle className="h-3.5 w-3.5" />
                                    Salva impostazioni
                                </Button>
                                <span className="text-xs text-gray-400 flex items-center gap-1">
                                    <HardDrive className="h-3 w-3" />
                                    Salvataggio in ./backups/
                                </span>
                            </div>
                        </div>
                    </CardContent>
                </Card>

                {/* Backup Files List */}
                {backupList.length > 0 && (
                    <Card className="border-gray-200 shadow-sm bg-white">
                        <CardHeader className="pb-4 border-b border-gray-100">
                            <div className="flex items-center gap-3">
                                <div className="w-9 h-9 rounded-lg bg-gray-50 border border-gray-200 flex items-center justify-center flex-shrink-0">
                                    <FileArchive className="h-4 w-4 text-gray-600" />
                                </div>
                                <div>
                                    <CardTitle className="text-base text-gray-900">Backup Disponibili</CardTitle>
                                    <CardDescription className="text-xs mt-0.5">
                                        {backupList.length} file salvati su disco
                                    </CardDescription>
                                </div>
                            </div>
                        </CardHeader>
                        <CardContent className="pt-4">
                            <div className="space-y-2">
                                {backupList.map((backup) => (
                                    <div key={backup.filename} className="flex items-center justify-between p-3 bg-gray-50 rounded-lg border border-gray-100">
                                        <div className="flex items-center gap-3">
                                            <FileArchive className="h-4 w-4 text-gray-400" />
                                            <div>
                                                <p className="text-sm font-medium text-gray-700">{backup.filename}</p>
                                                <p className="text-xs text-gray-400">
                                                    {formatBytes(backup.size)} • {formatDate(backup.created_at)} •
                                                    <span className={`ml-1 ${backup.type === 'full' ? 'text-blue-500' : 'text-emerald-500'}`}>
                                                        {backup.type === 'full' ? 'Completo' : 'Config'}
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
                                                <Download className="h-4 w-4 text-gray-500" />
                                            </Button>
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                className="h-8 w-8 p-0"
                                                onClick={() => handleDeleteBackup(backup.filename)}
                                            >
                                                <Trash2 className="h-4 w-4 text-red-400" />
                                            </Button>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </CardContent>
                    </Card>
                )}

                {/* Footer */}
                <div className="pt-6 border-t border-gray-200">
                    <p className="text-center text-xs text-gray-400">
                        Sviluppato da{' '}
                        <span className="font-semibold text-gray-500">Giovanni Addeo</span>
                        {' '}— soluzioni IIoT per il monitoraggio e la storicizzazione di impianti industriali in tempo reale.
                    </p>
                </div>

            </div>
        </div>
    );
};

export default SystemPage;
