import { useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import { RefreshCw, RotateCcw, Upload, Wifi, WifiOff, Loader2 } from 'lucide-react';
import { useAuthStore } from '@/stores/useAuthStore';

interface EdgeStatus {
    org_id: number;
    org_name: string;
    online: boolean;
    last_ping: string | null;
    gateway_count: number;
}

const fleetApi = {
    async getStatus(): Promise<EdgeStatus[]> {
        const token = useAuthStore.getState().token;
        const r = await fetch('/api/fleet/status', {
            headers: { Authorization: `Bearer ${token}` },
        });
        if (!r.ok) throw new Error('Failed to fetch fleet status');
        return r.json();
    },
    async restartEdge(orgId: number): Promise<void> {
        const token = useAuthStore.getState().token;
        const r = await fetch(`/api/organizations/${orgId}/edge-restart`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${token}` },
        });
        if (!r.ok) throw new Error('Failed to restart edge');
    },
    async updateEdge(orgId: number, version: string, image: string): Promise<void> {
        const token = useAuthStore.getState().token;
        const r = await fetch(`/api/organizations/${orgId}/edge-update`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
            body: JSON.stringify({ version, image }),
        });
        if (!r.ok) throw new Error('Failed to send OTA update');
    },
};

const FleetPage = () => {
    const [fleet, setFleet] = useState<EdgeStatus[]>([]);
    const [loading, setLoading] = useState(true);
    const [actionOrgId, setActionOrgId] = useState<number | null>(null);
    const [updateDialog, setUpdateDialog] = useState<{ orgId: number; orgName: string } | null>(null);
    const [updateVersion, setUpdateVersion] = useState('');
    const [updateImage, setUpdateImage] = useState('');
    const [actionLoading, setActionLoading] = useState(false);
    const [toast, setToast] = useState<string | null>(null);

    const load = async () => {
        setLoading(true);
        try {
            setFleet(await fleetApi.getStatus());
        } catch {
            setFleet([]);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => { load(); }, []);

    const showToast = (msg: string) => {
        setToast(msg);
        setTimeout(() => setToast(null), 3000);
    };

    const handleRestart = async (orgId: number) => {
        setActionOrgId(orgId);
        setActionLoading(true);
        try {
            await fleetApi.restartEdge(orgId);
            showToast('Restart command sent');
        } catch {
            showToast('Failed to send restart command');
        } finally {
            setActionLoading(false);
            setActionOrgId(null);
        }
    };

    const handleUpdate = async () => {
        if (!updateDialog || !updateVersion.trim()) return;
        setActionLoading(true);
        try {
            await fleetApi.updateEdge(updateDialog.orgId, updateVersion.trim(), updateImage.trim());
            showToast(`OTA update sent to ${updateDialog.orgName}`);
            setUpdateDialog(null);
        } catch {
            showToast('Failed to send OTA update');
        } finally {
            setActionLoading(false);
        }
    };

    return (
        <div className="space-y-6 p-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h1 className="text-2xl font-bold tracking-tight">Fleet Management</h1>
                    <p className="text-sm text-muted-foreground mt-1">
                        Monitor and manage all edge agents across organizations.
                    </p>
                </div>
                <Button variant="outline" size="sm" onClick={load} disabled={loading}>
                    <RefreshCw className={`h-4 w-4 mr-2 ${loading ? 'animate-spin' : ''}`} />
                    Refresh
                </Button>
            </div>

            {/* Summary cards */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                <div className="rounded-lg border bg-card p-4">
                    <div className="text-sm text-muted-foreground uppercase tracking-wider font-semibold mb-1">Total Orgs</div>
                    <div className="text-3xl font-bold">{fleet.length}</div>
                </div>
                <div className="rounded-lg border bg-card p-4">
                    <div className="text-sm text-muted-foreground uppercase tracking-wider font-semibold mb-1">Online</div>
                    <div className="text-3xl font-bold text-green-500">{fleet.filter(f => f.online).length}</div>
                </div>
                <div className="rounded-lg border bg-card p-4">
                    <div className="text-sm text-muted-foreground uppercase tracking-wider font-semibold mb-1">Offline</div>
                    <div className="text-3xl font-bold text-destructive">{fleet.filter(f => !f.online).length}</div>
                </div>
            </div>

            <div className="rounded-lg border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Status</TableHead>
                            <TableHead>Organization</TableHead>
                            <TableHead>Gateways</TableHead>
                            <TableHead>Last Ping</TableHead>
                            <TableHead className="text-right">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {loading ? (
                            <TableRow>
                                <TableCell colSpan={5} className="text-center py-8">
                                    <Loader2 className="h-6 w-6 animate-spin mx-auto text-muted-foreground" />
                                </TableCell>
                            </TableRow>
                        ) : fleet.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={5} className="text-center py-8 text-muted-foreground">
                                    No organizations found
                                </TableCell>
                            </TableRow>
                        ) : fleet.map((org) => (
                            <TableRow key={org.org_id}>
                                <TableCell>
                                    {org.online ? (
                                        <div className="flex items-center gap-2">
                                            <Wifi className="h-4 w-4 text-green-500" />
                                            <span className="text-xs font-semibold text-green-600 uppercase tracking-wider">Online</span>
                                        </div>
                                    ) : (
                                        <div className="flex items-center gap-2">
                                            <WifiOff className="h-4 w-4 text-destructive" />
                                            <span className="text-xs font-semibold text-destructive uppercase tracking-wider">Offline</span>
                                        </div>
                                    )}
                                </TableCell>
                                <TableCell>
                                    <div className="font-medium">{org.org_name}</div>
                                    <div className="text-xs text-muted-foreground">org #{org.org_id}</div>
                                </TableCell>
                                <TableCell>{org.gateway_count}</TableCell>
                                <TableCell className="text-sm text-muted-foreground">
                                    {org.last_ping
                                        ? new Date(org.last_ping).toLocaleString()
                                        : '—'}
                                </TableCell>
                                <TableCell className="text-right">
                                    <div className="flex gap-2 justify-end">
                                        <Button
                                            size="sm"
                                            variant="outline"
                                            onClick={() => handleRestart(org.org_id)}
                                            disabled={actionLoading && actionOrgId === org.org_id}
                                        >
                                            {actionLoading && actionOrgId === org.org_id ? (
                                                <Loader2 className="h-4 w-4 animate-spin" />
                                            ) : (
                                                <RotateCcw className="h-4 w-4" />
                                            )}
                                            <span className="ml-1">Restart</span>
                                        </Button>
                                        <Button
                                            size="sm"
                                            variant="outline"
                                            onClick={() => {
                                                setUpdateDialog({ orgId: org.org_id, orgName: org.org_name });
                                                setUpdateVersion('');
                                                setUpdateImage('');
                                            }}
                                        >
                                            <Upload className="h-4 w-4" />
                                            <span className="ml-1">Update</span>
                                        </Button>
                                    </div>
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>

            {/* OTA Update Dialog */}
            <Dialog open={!!updateDialog} onOpenChange={() => setUpdateDialog(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>OTA Update — {updateDialog?.orgName}</DialogTitle>
                    </DialogHeader>
                    <div className="space-y-4 pt-2">
                        <div className="space-y-2">
                            <Label>Version tag <span className="text-destructive">*</span></Label>
                            <Input
                                placeholder="e.g. v1.2.0"
                                value={updateVersion}
                                onChange={(e) => setUpdateVersion(e.target.value)}
                            />
                        </div>
                        <div className="space-y-2">
                            <Label>Custom image (optional)</Label>
                            <Input
                                placeholder="ghcr.io/inferis995/openedge-driver-manager:v1.2.0"
                                value={updateImage}
                                onChange={(e) => setUpdateImage(e.target.value)}
                            />
                            <p className="text-xs text-muted-foreground">
                                Leave blank to use the default image for the version tag.
                            </p>
                        </div>
                        <div className="flex justify-end gap-2">
                            <Button variant="outline" onClick={() => setUpdateDialog(null)}>Cancel</Button>
                            <Button
                                onClick={handleUpdate}
                                disabled={!updateVersion.trim() || actionLoading}
                            >
                                {actionLoading ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : null}
                                Send OTA Update
                            </Button>
                        </div>
                    </div>
                </DialogContent>
            </Dialog>

            {/* Toast */}
            {toast && (
                <div className="fixed bottom-6 right-6 z-50 rounded-lg border bg-card px-4 py-3 shadow-lg text-sm font-medium">
                    {toast}
                </div>
            )}
        </div>
    );
};

export default FleetPage;
