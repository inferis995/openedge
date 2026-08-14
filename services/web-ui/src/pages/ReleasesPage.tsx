import { useEffect, useState } from 'react';
import { updatesApi, EdgeRelease, FleetUpdateRow } from '@/api/updates';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Switch } from '@/components/ui/switch';
import { Label } from '@/components/ui/label';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { PackageOpen, Plus, Download, RefreshCw, CheckCircle2, Clock, AlertCircle, XCircle } from 'lucide-react';
import { toast } from 'sonner';

function statusBadge(status: string) {
    switch (status) {
        case 'pending':    return <Badge variant="secondary"><Clock className="h-3 w-3 mr-1" />Pending</Badge>;
        case 'approved':   return <Badge className="bg-blue-600 text-white"><CheckCircle2 className="h-3 w-3 mr-1" />Approved</Badge>;
        case 'updating':   return <Badge className="bg-indigo-600 text-white"><RefreshCw className="h-3 w-3 mr-1 animate-spin" />Updating</Badge>;
        case 'success':    return <Badge className="bg-green-600 text-white"><CheckCircle2 className="h-3 w-3 mr-1" />Success</Badge>;
        case 'failed':     return <Badge variant="destructive"><XCircle className="h-3 w-3 mr-1" />Failed</Badge>;
        case 'rolled_back': return <Badge className="bg-orange-500 text-white"><AlertCircle className="h-3 w-3 mr-1" />Rolled Back</Badge>;
        default: return <Badge variant="outline">{status}</Badge>;
    }
}

function formatDate(dateStr: string) {
    return new Date(dateStr).toLocaleString();
}

const SHA256_RE = /^[0-9a-fA-F]{64}$/;

const ReleasesPage = () => {
    const [releases, setReleases] = useState<EdgeRelease[]>([]);
    const [fleet, setFleet] = useState<FleetUpdateRow[]>([]);
    const [loading, setLoading] = useState(true);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [publishing, setPublishing] = useState(false);
    const [form, setForm] = useState({
        version: '',
        release_notes: '',
        artifact_url: '',
        sha256_checksum: '',
        is_stable: true,
    });
    const [formErrors, setFormErrors] = useState<Record<string, string>>({});

    const loadData = async () => {
        setLoading(true);
        try {
            const [r, f] = await Promise.all([
                updatesApi.listReleases(),
                updatesApi.getFleetStatus(),
            ]);
            setReleases(r);
            setFleet(f);
        } catch (err) {
            console.error('Failed to load release data', err);
            toast.error('Failed to load release data');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => { loadData(); }, []);

    const validate = () => {
        const errs: Record<string, string> = {};
        if (!form.version.trim()) errs.version = 'Version is required';
        if (!form.artifact_url.trim()) errs.artifact_url = 'Artifact URL is required';
        if (!SHA256_RE.test(form.sha256_checksum)) errs.sha256_checksum = 'Must be exactly 64 hex characters';
        return errs;
    };

    const handlePublish = async () => {
        const errs = validate();
        if (Object.keys(errs).length > 0) { setFormErrors(errs); return; }
        setFormErrors({});
        setPublishing(true);
        try {
            await updatesApi.createRelease(form);
            toast.success(`Release ${form.version} published`);
            setDialogOpen(false);
            setForm({ version: '', release_notes: '', artifact_url: '', sha256_checksum: '', is_stable: true });
            await loadData();
        } catch (err) {
            toast.error('Failed to publish release');
            console.error(err);
        } finally {
            setPublishing(false);
        }
    };

    const exportCSV = () => {
        const headers = ['org_id', 'org_name', 'version', 'status', 'approved_at', 'completed_at', 'error_msg'];
        const rows = fleet.map(r => [
            r.org_id, r.org_name, r.version, r.status,
            r.approved_at || '', r.completed_at || '', r.error_msg || '',
        ]);
        const csv = [headers, ...rows].map(r => r.map(v => `"${String(v).replace(/"/g, '""')}"`).join(',')).join('\n');
        const blob = new Blob([csv], { type: 'text/csv' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `fleet-update-status-${new Date().toISOString().slice(0, 10)}.csv`;
        a.click();
        URL.revokeObjectURL(url);
    };

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex items-center gap-3">
                    <PackageOpen className="h-9 sm:h-7 w-9 sm:w-7 text-primary" />
                    <div>
                        <h1 className="text-2xl font-bold">Edge Releases</h1>
                        <p className="text-sm text-muted-foreground">Manage OTA edge software releases</p>
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    <Button variant="outline" size="sm" onClick={loadData} disabled={loading}>
                        <RefreshCw className={`h-4 w-4 mr-2 ${loading ? 'animate-spin' : ''}`} />
                        Refresh
                    </Button>
                    <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
                        <DialogTrigger asChild>
                            <Button size="sm">
                                <Plus className="h-4 w-4 mr-2" />
                                Publish Release
                            </Button>
                        </DialogTrigger>
                        <DialogContent className="max-w-lg">
                            <DialogHeader>
                                <DialogTitle>Publish New Edge Release</DialogTitle>
                            </DialogHeader>
                            <div className="space-y-4 py-2">
                                <div className="space-y-1">
                                    <Label htmlFor="version">Version <span className="text-red-500">*</span></Label>
                                    <Input
                                        id="version"
                                        placeholder="e.g. v1.3.0"
                                        value={form.version}
                                        onChange={e => setForm(f => ({ ...f, version: e.target.value }))}
                                    />
                                    {formErrors.version && <p className="text-xs text-red-500">{formErrors.version}</p>}
                                </div>
                                <div className="space-y-1">
                                    <Label htmlFor="artifact_url">Artifact URL <span className="text-red-500">*</span></Label>
                                    <Input
                                        id="artifact_url"
                                        placeholder="https://releases.example.com/edge-v1.3.0.tar.gz"
                                        value={form.artifact_url}
                                        onChange={e => setForm(f => ({ ...f, artifact_url: e.target.value }))}
                                    />
                                    {formErrors.artifact_url && <p className="text-xs text-red-500">{formErrors.artifact_url}</p>}
                                </div>
                                <div className="space-y-1">
                                    <Label htmlFor="sha256">SHA256 Checksum <span className="text-red-500">*</span></Label>
                                    <Input
                                        id="sha256"
                                        placeholder="64 hex characters"
                                        value={form.sha256_checksum}
                                        onChange={e => setForm(f => ({ ...f, sha256_checksum: e.target.value }))}
                                        className="font-mono text-xs"
                                    />
                                    {formErrors.sha256_checksum && <p className="text-xs text-red-500">{formErrors.sha256_checksum}</p>}
                                </div>
                                <div className="space-y-1">
                                    <Label htmlFor="release_notes">Release Notes</Label>
                                    <Textarea
                                        id="release_notes"
                                        placeholder="What changed in this release..."
                                        value={form.release_notes}
                                        onChange={e => setForm(f => ({ ...f, release_notes: e.target.value }))}
                                        rows={4}
                                    />
                                </div>
                                <div className="flex items-center gap-3">
                                    <Switch
                                        id="is_stable"
                                        checked={form.is_stable}
                                        onCheckedChange={checked => setForm(f => ({ ...f, is_stable: checked }))}
                                    />
                                    <Label htmlFor="is_stable">Mark as stable</Label>
                                </div>
                                <div className="flex justify-end gap-2 pt-2">
                                    <Button variant="outline" onClick={() => setDialogOpen(false)}>Cancel</Button>
                                    <Button onClick={handlePublish} disabled={publishing}>
                                        {publishing ? <RefreshCw className="h-4 w-4 mr-2 animate-spin" /> : <Plus className="h-4 w-4 mr-2" />}
                                        Publish
                                    </Button>
                                </div>
                            </div>
                        </DialogContent>
                    </Dialog>
                </div>
            </div>

            {/* Releases Table */}
            <Card>
                <CardHeader className="pb-3">
                    <CardTitle className="text-base">Published Releases</CardTitle>
                </CardHeader>
                <CardContent className="p-0">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Version</TableHead>
                                <TableHead>Published</TableHead>
                                <TableHead>Stable</TableHead>
                                <TableHead>Pending</TableHead>
                                <TableHead>Approved</TableHead>
                                <TableHead>Success</TableHead>
                                <TableHead>Failed</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {loading ? (
                                <TableRow><TableCell colSpan={7} className="text-center py-8 text-muted-foreground">Loading...</TableCell></TableRow>
                            ) : releases.length === 0 ? (
                                <TableRow><TableCell colSpan={7} className="text-center py-8 text-muted-foreground">No releases published yet</TableCell></TableRow>
                            ) : releases.map(r => (
                                <TableRow key={r.id}>
                                    <TableCell className="font-mono font-semibold">{r.version}</TableCell>
                                    <TableCell className="text-sm text-muted-foreground">{formatDate(r.published_at)}</TableCell>
                                    <TableCell>
                                        {r.is_stable
                                            ? <Badge className="bg-green-600 text-white">Stable</Badge>
                                            : <Badge variant="outline">Pre-release</Badge>}
                                    </TableCell>
                                    <TableCell><Badge variant="secondary">{r.org_status_counts?.pending ?? 0}</Badge></TableCell>
                                    <TableCell><Badge className="bg-blue-600 text-white">{r.org_status_counts?.approved ?? 0}</Badge></TableCell>
                                    <TableCell><Badge className="bg-green-600 text-white">{r.org_status_counts?.success ?? 0}</Badge></TableCell>
                                    <TableCell><Badge variant="destructive">{r.org_status_counts?.failed ?? 0}</Badge></TableCell>
                                </TableRow>
                            ))}
                        </TableBody>
                    </Table>
                </CardContent>
            </Card>

            {/* Fleet Status */}
            <Card>
                <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                        <CardTitle className="text-base">Fleet Update Status</CardTitle>
                        <Button variant="outline" size="sm" onClick={exportCSV}>
                            <Download className="h-4 w-4 mr-2" />
                            Export CSV
                        </Button>
                    </div>
                </CardHeader>
                <CardContent className="p-0">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Organization</TableHead>
                                <TableHead>Version</TableHead>
                                <TableHead>Status</TableHead>
                                <TableHead>Approved At</TableHead>
                                <TableHead>Completed At</TableHead>
                                <TableHead>Error</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {loading ? (
                                <TableRow><TableCell colSpan={6} className="text-center py-8 text-muted-foreground">Loading...</TableCell></TableRow>
                            ) : fleet.length === 0 ? (
                                <TableRow><TableCell colSpan={6} className="text-center py-8 text-muted-foreground">No fleet data yet</TableCell></TableRow>
                            ) : fleet.map(r => (
                                <TableRow key={r.org_id}>
                                    <TableCell className="font-medium">{r.org_name}</TableCell>
                                    <TableCell className="font-mono text-sm">{r.version}</TableCell>
                                    <TableCell>{statusBadge(r.status)}</TableCell>
                                    <TableCell className="text-sm text-muted-foreground">{r.approved_at ? formatDate(r.approved_at) : '—'}</TableCell>
                                    <TableCell className="text-sm text-muted-foreground">{r.completed_at ? formatDate(r.completed_at) : '—'}</TableCell>
                                    <TableCell className="text-sm text-red-500 max-w-[200px] truncate">{r.error_msg || '—'}</TableCell>
                                </TableRow>
                            ))}
                        </TableBody>
                    </Table>
                </CardContent>
            </Card>
        </div>
    );
};

export default ReleasesPage;
