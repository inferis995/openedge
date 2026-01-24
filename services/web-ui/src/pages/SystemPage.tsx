import { useState } from 'react';
import { systemApi } from '@/api/system';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { RefreshCw, Download, AlertTriangle, CheckCircle } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

const SystemPage = () => {
    const [loading, setLoading] = useState(false);
    const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);

    const handleReload = async () => {
        setLoading(true);
        setMessage(null);
        try {
            await systemApi.reload();
            setMessage({ type: 'success', text: 'System configuration reloaded and published to MQTT.' });
        } catch (error) {
            setMessage({ type: 'error', text: 'Failed to reload configuration.' });
        } finally {
            setLoading(false);
        }
    };

    const handleExport = async () => {
        try {
            const blob = await systemApi.exportConfig();
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `edge-config-${new Date().toISOString().split('T')[0]}.json`;
            document.body.appendChild(a);
            a.click();
            window.URL.revokeObjectURL(url);
            document.body.removeChild(a);
        } catch (error) {
            setMessage({ type: 'error', text: 'Failed to export configuration.' });
        }
    };

    const handleImport = async (e: React.ChangeEvent<HTMLInputElement>) => {
        if (!e.target.files || !e.target.files[0]) return;
        const file = e.target.files[0];

        if (confirm('Importing configuration will replace or update current settings. Continue?')) {
            setLoading(true);
            setMessage(null);
            try {
                await systemApi.importConfig(file);
                setMessage({ type: 'success', text: 'Configuration imported successfully.' });
                // Optionally reload page to reflect changes
            } catch (error) {
                setMessage({ type: 'error', text: 'Failed to import configuration.' });
            } finally {
                setLoading(false);
            }
        }
    };

    return (
        <div className="space-y-6">
            <div>
                <h2 className="text-2xl font-bold tracking-tight">System Manager</h2>
                <p className="text-muted-foreground">
                    Maintenance tasks, configuration backup, and system control.
                </p>
            </div>

            {message && (
                <div className={`p-4 rounded-md flex items-center gap-3 ${message.type === 'success' ? 'bg-green-50 text-green-700 border border-green-200' : 'bg-red-50 text-red-700 border border-red-200'}`}>
                    {message.type === 'success' ? <CheckCircle size={20} /> : <AlertTriangle size={20} />}
                    {message.text}
                </div>
            )}

            <div className="grid gap-6 md:grid-cols-2">
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <RefreshCw className="h-5 w-5 text-blue-500" />
                            Configuration Reload
                        </CardTitle>
                        <CardDescription>
                            Apply pending changes to the runtime engine. This will publish the new configuration via MQTT to all drivers.
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <Button onClick={handleReload} disabled={loading} className="w-full sm:w-auto">
                            {loading ? 'Reloading...' : 'Reload Configuration'}
                        </Button>
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <Download className="h-5 w-5 text-indigo-500" />
                            Backup & Restore
                        </CardTitle>
                        <CardDescription>
                            Export current configuration to JSON or restore from a backup file.
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <Button variant="outline" onClick={handleExport} className="w-full sm:w-auto gap-2">
                            <Download size={16} /> Export Configuration
                        </Button>

                        <div className="pt-4 border-t">
                            <Label htmlFor="import-file" className="block mb-2 text-sm text-slate-600">Import Configuration File</Label>
                            <div className="flex gap-2">
                                <Input
                                    id="import-file"
                                    type="file"
                                    accept=".json"
                                    onChange={handleImport}
                                    disabled={loading}
                                />
                            </div>
                        </div>
                    </CardContent>
                </Card>
            </div>
        </div>
    );
};

export default SystemPage;
