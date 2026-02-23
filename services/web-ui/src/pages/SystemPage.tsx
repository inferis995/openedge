import { useState } from 'react';
import { systemApi } from '@/api/system';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

import { Download, AlertTriangle, CheckCircle } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';


const SystemPage = () => {
    const [loading, setLoading] = useState(false);
    const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);

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
                    Perform full system backups and restoration.
                </p>
            </div>

            {message && (
                <div className={`p-4 rounded-md flex items-center gap-3 ${message.type === 'success' ? 'bg-green-50 text-green-700 border border-green-200' : 'bg-red-50 text-red-700 border border-red-200'}`}>
                    {message.type === 'success' ? <CheckCircle size={20} /> : <AlertTriangle size={20} />}
                    {message.text}
                </div>
            )}

            <div className="max-w-2xl">
                {/* Full System Backup */}
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
