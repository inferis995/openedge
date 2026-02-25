import { useState, useEffect } from 'react';
import { systemApi, GlobalSettings, UpdateSettingsRequest, PublishMetrics } from '@/api/system';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { Slider } from '@/components/ui/slider';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';

import { Download, AlertTriangle, CheckCircle, RefreshCw, Zap, ScrollText, ChevronDown, Settings2, BarChart3 } from 'lucide-react';
import { Input } from '@/components/ui/input';

const PUBLISH_MODES = [
    {
        value: 'dual',
        label: 'Dual (Legacy + Sparkplug B)',
        icon: RefreshCw,
        description: 'Pubblica in entrambi i formati per massima compatibilità'
    },
    {
        value: 'sparkplug_only',
        label: 'Solo Sparkplug B',
        icon: Zap,
        description: 'Ottimizza la banda, pubblica solo i cambiamenti (RBE)'
    },
    {
        value: 'legacy_only',
        label: 'Solo Legacy',
        icon: ScrollText,
        description: 'Formato compatibile con sistemi esistenti'
    }
];

const TOOLTIPS = {
    dual: 'Pubblica ogni aggiornamento in entrambi i formati. Ideale per ambienti misti con SCADA legacy e sistemi Sparkplug B. Maggior traffico di rete ma massima compatibilità.',
    sparkplug_only: 'Usa lo standard industriale Eclipse Sparkplug B con Report by Exception. Pubblica solo quando il valore cambia, riducendo drasticamente il traffico di rete. Richiede client compatibili Sparkplug B.',
    legacy_only: 'Pubblica solo nel formato JSON legacy interno. Per sistemi che non supportano Sparkplug B o per debug.'
};

const SystemPage = () => {
    const [loading, setLoading] = useState(false);
    const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);

    // Settings state
    const [settings, setSettings] = useState<GlobalSettings | null>(null);
    const [settingsLoading, setSettingsLoading] = useState(true);
    const [publishMode, setPublishMode] = useState<string>('dual');
    const [heartbeat, setHeartbeat] = useState<number>(60);
    const [deadband, setDeadband] = useState<number>(0.5);
    const [advancedOpen, setAdvancedOpen] = useState(false);
    const [metrics, setMetrics] = useState<PublishMetrics | null>(null);

    // Load settings on mount
    useEffect(() => {
        loadSettings();
        loadMetrics();
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

    const loadMetrics = async () => {
        try {
            const data = await systemApi.getMetrics();
            setMetrics(data);
        } catch (error) {
            console.error('Failed to load metrics:', error);
        }
    };

    const handleSaveSettings = async () => {
        setLoading(true);
        setMessage(null);
        try {
            const update: UpdateSettingsRequest = {
                publish_mode: publishMode
            };

            if (publishMode === 'sparkplug_only') {
                update.rbe_heartbeat_seconds = heartbeat;
                update.rbe_deadband_percent = deadband;
            }

            await systemApi.updateSettings(update);
            setMessage({ type: 'success', text: 'Configurazione salvata. Applicata automaticamente ai driver.' });
            loadMetrics();
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: 'Errore nel salvataggio della configurazione.' });
        } finally {
            setLoading(false);
        }
    };

    const handleFullBackup = async () => {
        setLoading(true);
        setMessage({ type: 'success', text: 'Generating backup... please wait.' });
        try {
            const blob = await systemApi.exportBackup();
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `system-full-backup-${new Date().toISOString().replace(/[:.]/g, '-')}.zip`;
            document.body.appendChild(a);
            a.click();
            window.URL.revokeObjectURL(url);
            document.body.removeChild(a);
            setMessage({ type: 'success', text: 'System backup created and downloaded.' });
        } catch (error) {
            console.error(error);
            setMessage({ type: 'error', text: 'Failed to create system backup.' });
        } finally {
            setLoading(false);
        }
    };

    const handleFullRestore = async (e: React.ChangeEvent<HTMLInputElement>) => {
        if (!e.target.files || !e.target.files[0]) return;
        const file = e.target.files[0];

        if (confirm('CRITICAL WARNING: Restoring a full backup will OVERWRITE the current configuration and merge historical data. This cannot be undone. Are you sure?')) {
            setLoading(true);
            setMessage({ type: 'success', text: 'Restoring backup... this may take a moment.' });
            try {
                await systemApi.restoreBackup(file);
                setMessage({ type: 'success', text: 'System restored successfully. Reloading...' });
                setTimeout(() => window.location.reload(), 2000);
            } catch (error) {
                console.error(error);
                setMessage({ type: 'error', text: 'Failed to restore system backup.' });
            } finally {
                setLoading(false);
            }
        } else {
            e.target.value = ''; // Reset input
        }
    };

    return (
        <div className="space-y-6">
            <div>
                <h2 className="text-2xl font-bold tracking-tight">System Manager</h2>
                <p className="text-muted-foreground">
                    Configure system settings and perform backups.
                </p>
            </div>

            {message && (
                <div className={`p-4 rounded-md flex items-center gap-3 ${message.type === 'success' ? 'bg-green-50 text-green-700 border border-green-200' : 'bg-red-50 text-red-700 border border-red-200'}`}>
                    {message.type === 'success' ? <CheckCircle size={20} /> : <AlertTriangle size={20} />}
                    {message.text}
                </div>
            )}

            {/* MQTT Configuration Section */}
            <Card className="max-w-2xl border-blue-200 shadow-sm">
                <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                        <RefreshCw className="h-5 w-5 text-blue-600" />
                        Configurazione MQTT
                    </CardTitle>
                    <CardDescription>
                        Seleziona la modalita di pubblicazione MQTT per i driver industriali.
                    </CardDescription>
                </CardHeader>
                <CardContent className="space-y-6">
                    {settingsLoading ? (
                        <div className="text-muted-foreground">Caricamento...</div>
                    ) : (
                        <>
                            <div className="space-y-4">
                                <Label className="text-base font-medium">Modalita di pubblicazione</Label>
                                <RadioGroup
                                    value={publishMode}
                                    onValueChange={setPublishMode}
                                    className="space-y-3"
                                >
                                    {PUBLISH_MODES.map((mode) => {
                                        const Icon = mode.icon;
                                        return (
                                            <div
                                                key={mode.value}
                                                className={`flex items-start space-x-3 p-3 rounded-lg border transition-colors ${
                                                    publishMode === mode.value
                                                        ? 'border-blue-500 bg-blue-50'
                                                        : 'border-gray-200 hover:border-gray-300'
                                                }`}
                                            >
                                                <RadioGroupItem value={mode.value} id={mode.value} className="mt-1" />
                                                <div className="flex-1">
                                                    <Label
                                                        htmlFor={mode.value}
                                                        className="flex items-center gap-2 cursor-pointer font-medium"
                                                    >
                                                        <Icon className={`h-4 w-4 ${publishMode === mode.value ? 'text-blue-600' : 'text-gray-500'}`} />
                                                        {mode.label}
                                                    </Label>
                                                    <p className="text-sm text-muted-foreground mt-1">
                                                        {mode.description}
                                                    </p>
                                                    <p className="text-xs text-blue-600 mt-2 italic">
                                                        {TOOLTIPS[mode.value as keyof typeof TOOLTIPS]}
                                                    </p>
                                                </div>
                                            </div>
                                        );
                                    })}
                                </RadioGroup>
                            </div>

                            {/* Advanced options - only visible for sparkplug_only */}
                            {publishMode === 'sparkplug_only' && (
                                <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
                                    <CollapsibleTrigger className="flex items-center gap-2 text-sm font-medium text-gray-700 hover:text-gray-900 w-full py-2">
                                        <Settings2 className="h-4 w-4" />
                                        Avanzate
                                        <ChevronDown className={`h-4 w-4 transition-transform ${advancedOpen ? 'rotate-180' : ''}`} />
                                    </CollapsibleTrigger>
                                    <CollapsibleContent className="space-y-4 pt-4">
                                        <div className="space-y-3">
                                            <div className="flex justify-between">
                                                <Label>Heartbeat (secondi)</Label>
                                                <span className="text-sm text-muted-foreground">{heartbeat} sec</span>
                                            </div>
                                            <div className="flex gap-2 flex-wrap">
                                                {[10, 30, 60, 120, 300].map((val) => (
                                                    <Button
                                                        key={val}
                                                        type="button"
                                                        variant={heartbeat === val ? "default" : "outline"}
                                                        size="sm"
                                                        className="h-8 min-w-[50px]"
                                                        onClick={() => setHeartbeat(val)}
                                                    >
                                                        {val < 60 ? `${val}s` : `${val / 60}m`}
                                                    </Button>
                                                ))}
                                            </div>
                                            <p className="text-xs text-muted-foreground">
                                                Intervallo di pubblicazione periodico anche se il valore non cambia.
                                            </p>
                                        </div>

                                        <div className="space-y-3">
                                            <div className="flex justify-between">
                                                <Label>Deadband (%)</Label>
                                                <span className="text-sm text-muted-foreground">{deadband.toFixed(1)}%</span>
                                            </div>
                                            <Slider
                                                value={[deadband * 10]}
                                                onValueChange={(v) => setDeadband(v[0] / 10)}
                                                min={0}
                                                max={50}
                                                step={1}
                                                className="w-full"
                                            />
                                            <p className="text-xs text-muted-foreground">
                                                Soglia minima di variazione per pubblicare valori analogici.
                                            </p>
                                        </div>
                                    </CollapsibleContent>
                                </Collapsible>
                            )}

                            {/* Metrics display */}
                            {metrics && (
                                <div className="flex items-center gap-4 p-3 bg-gray-50 rounded-lg border">
                                    <BarChart3 className="h-5 w-5 text-gray-500" />
                                    <div className="text-sm">
                                        <span className="font-medium">Statistiche (ultimi 5 min):</span>
                                        {' '}
                                        <span className="text-green-600">Pubblicati: {metrics.published}</span>
                                        {' | '}
                                        <span className="text-blue-600">Risparmiati: {metrics.saved_percent.toFixed(1)}%</span>
                                    </div>
                                </div>
                            )}

                            <div className="flex items-center gap-3">
                                <Button
                                    onClick={handleSaveSettings}
                                    disabled={loading}
                                    className="gap-2 bg-blue-600 hover:bg-blue-700 text-white"
                                >
                                    <CheckCircle size={16} />
                                    Salva configurazione
                                </Button>
                                {settings && settings.publish_mode === publishMode && (
                                    <span className="text-xs text-green-600 flex items-center gap-1">
                                        <CheckCircle size={12} />
                                        Applicato automaticamente
                                    </span>
                                )}
                            </div>
                        </>
                    )}
                </CardContent>
            </Card>

            {/* Full System Backup */}
            <div className="max-w-2xl">
                <Card className="border-purple-200 shadow-sm">
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <Download className="h-5 w-5 text-purple-600" />
                            Full System Backup
                        </CardTitle>
                        <CardDescription>
                            Download a complete snapshot including SQL Configuration and InfluxDB History. System remains online.
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-6">
                        <Button onClick={handleFullBackup} disabled={loading} className="w-full sm:w-auto gap-2 bg-purple-600 hover:bg-purple-700 text-white">
                            <Download size={16} /> Download Full Backup (.zip)
                        </Button>

                        <div className="pt-6 border-t border-purple-100">
                            <Label htmlFor="restore-file" className="block mb-3 text-lg font-medium text-purple-900">Restore Full System</Label>
                            <CardDescription className="mb-4">
                                Upload a previously downloaded .zip backup file to restore configuration and history.
                            </CardDescription>
                            <div className="space-y-3">
                                <Input
                                    id="restore-file"
                                    type="file"
                                    accept=".zip"
                                    onChange={handleFullRestore}
                                    disabled={loading}
                                    className="file:bg-purple-50 file:text-purple-700 hover:file:bg-purple-100 cursor-pointer"
                                />
                                <p className="text-xs text-red-500 font-medium flex items-center gap-1">
                                    <AlertTriangle size={12} />
                                    Warning: Restore overwrites configuration and merges history.
                                </p>
                            </div>
                        </div>
                    </CardContent>
                </Card>
            </div>
        </div>
    );
};

export default SystemPage;
