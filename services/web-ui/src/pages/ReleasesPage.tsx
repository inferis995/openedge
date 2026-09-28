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
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { startLoad } from '@/lib/startLoad';

function statusBadge(status: string) {
    switch (status) {
        case 'pending':    return <Badge variant="secondary"><Clock className="h-3 w-3 mr-1" />{i18n.t('releasesPage.st_pending')}</Badge>;
        case 'approved':   return <Badge className="bg-blue-600 text-white"><CheckCircle2 className="h-3 w-3 mr-1" />{i18n.t('releasesPage.st_approved')}</Badge>;
        case 'updating':   return <Badge className="bg-indigo-600 text-white"><RefreshCw className="h-3 w-3 mr-1 animate-spin" />{i18n.t('releasesPage.st_updating')}</Badge>;
        case 'success':    return <Badge className="bg-green-600 text-white"><CheckCircle2 className="h-3 w-3 mr-1" />{i18n.t('releasesPage.st_success')}</Badge>;
        case 'failed':     return <Badge variant="destructive"><XCircle className="h-3 w-3 mr-1" />{i18n.t('releasesPage.st_failed')}</Badge>;
        case 'rolled_back': return <Badge className="bg-orange-500 text-white"><AlertCircle className="h-3 w-3 mr-1" />{i18n.t('releasesPage.st_rolled_back')}</Badge>;
        default: return <Badge variant="outline">{status}</Badge>;
    }
}

function formatDate(dateStr: string) {
    return new Date(dateStr).toLocaleString(i18n.language);
}

const SHA256_RE = /^[0-9a-fA-F]{64}$/;

const ReleasesPage = () => {
    const { t } = useTranslation();
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

    // Fetch without flipping the loading flag first: on mount it is already true.
    const fetchData = async () => {
        try {
            const [r, f] = await Promise.all([
                updatesApi.listReleases(),
                updatesApi.getFleetStatus(),
            ]);
            setReleases(r);
            setFleet(f);
        } catch (err) {
            console.error('Failed to load release data', err);
            toast.error(t('releasesPage.load_failed'));
        } finally {
            setLoading(false);
        }
    };

    const loadData = () => {
        setLoading(true);
        return fetchData();
    };

    useEffect(() => { startLoad(fetchData); }, []);

    const validate = () => {
        const errs: Record<string, string> = {};
        if (!form.version.trim()) errs.version = t('releasesPage.err_version');
        if (!form.artifact_url.trim()) errs.artifact_url = t('releasesPage.err_url');
        if (!SHA256_RE.test(form.sha256_checksum)) errs.sha256_checksum = t('releasesPage.err_sha');
        return errs;
    };

    const handlePublish = async () => {
        const errs = validate();
        if (Object.keys(errs).length > 0) { setFormErrors(errs); return; }
        setFormErrors({});
        setPublishing(true);
        try {
            await updatesApi.createRelease(form);
            toast.success(t('releasesPage.published_ok', { version: form.version }));
            setDialogOpen(false);
            setForm({ version: '', release_notes: '', artifact_url: '', sha256_checksum: '', is_stable: true });
            await loadData();
        } catch (err) {
            toast.error(t('releasesPage.publish_failed'));
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
                        <h1 className="text-2xl font-bold">{t('releasesPage.title')}</h1>
                        <p className="text-sm text-muted-foreground">{t('releasesPage.subtitle')}</p>
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    <Button variant="outline" size="sm" onClick={loadData} disabled={loading}>
                        <RefreshCw className={`h-4 w-4 mr-2 ${loading ? 'animate-spin' : ''}`} />
                        {t('releasesPage.refresh')}
                    </Button>
                    <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
                        <DialogTrigger asChild>
                            <Button size="sm">
                                <Plus className="h-4 w-4 mr-2" />
                                {t('releasesPage.publish_release')}
                            </Button>
                        </DialogTrigger>
                        <DialogContent className="max-w-lg">
                            <DialogHeader>
                                <DialogTitle>{t('releasesPage.dialog_title')}</DialogTitle>
                            </DialogHeader>
                            <div className="space-y-4 py-2">
                                <div className="space-y-1">
                                    <Label htmlFor="version">{t('releasesPage.version')} <span className="text-red-500">*</span></Label>
                                    <Input
                                        id="version"
                                        placeholder={t('releasesPage.version_ph')}
                                        value={form.version}
                                        onChange={e => setForm(f => ({ ...f, version: e.target.value }))}
                                    />
                                    {formErrors.version && <p className="text-xs text-red-500">{formErrors.version}</p>}
                                </div>
                                <div className="space-y-1">
                                    <Label htmlFor="artifact_url">{t('releasesPage.artifact_url')} <span className="text-red-500">*</span></Label>
                                    <Input
                                        id="artifact_url"
                                        placeholder="https://releases.example.com/edge-v1.3.0.tar.gz"
                                        value={form.artifact_url}
                                        onChange={e => setForm(f => ({ ...f, artifact_url: e.target.value }))}
                                    />
                                    {formErrors.artifact_url && <p className="text-xs text-red-500">{formErrors.artifact_url}</p>}
                                </div>
                                <div className="space-y-1">
                                    <Label htmlFor="sha256">{t('releasesPage.sha')} <span className="text-red-500">*</span></Label>
                                    <Input
                                        id="sha256"
                                        placeholder={t('releasesPage.sha_ph')}
                                        value={form.sha256_checksum}
                                        onChange={e => setForm(f => ({ ...f, sha256_checksum: e.target.value }))}
                                        className="font-mono text-xs"
                                    />
                                    {formErrors.sha256_checksum && <p className="text-xs text-red-500">{formErrors.sha256_checksum}</p>}
                                </div>
                                <div className="space-y-1">
                                    <Label htmlFor="release_notes">{t('releasesPage.notes')}</Label>
                                    <Textarea
                                        id="release_notes"
                                        placeholder={t('releasesPage.notes_ph')}
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
                                    <Label htmlFor="is_stable">{t('releasesPage.mark_stable')}</Label>
                                </div>
                                <div className="flex justify-end gap-2 pt-2">
                                    <Button variant="outline" onClick={() => setDialogOpen(false)}>{t('common.cancel')}</Button>
                                    <Button onClick={handlePublish} disabled={publishing}>
                                        {publishing ? <RefreshCw className="h-4 w-4 mr-2 animate-spin" /> : <Plus className="h-4 w-4 mr-2" />}
                                        {t('releasesPage.publish')}
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
                    <CardTitle className="text-base">{t('releasesPage.published')}</CardTitle>
                </CardHeader>
                <CardContent className="p-0">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>{t('releasesPage.version')}</TableHead>
                                <TableHead>{t('releasesPage.col_published')}</TableHead>
                                <TableHead>{t('releasesPage.col_stable')}</TableHead>
                                <TableHead>{t('releasesPage.st_pending')}</TableHead>
                                <TableHead>{t('releasesPage.col_approved')}</TableHead>
                                <TableHead>{t('releasesPage.col_success')}</TableHead>
                                <TableHead>{t('releasesPage.col_failed')}</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {loading ? (
                                <TableRow><TableCell colSpan={7} className="text-center py-8 text-muted-foreground">{t('common.loading')}</TableCell></TableRow>
                            ) : releases.length === 0 ? (
                                <TableRow><TableCell colSpan={7} className="text-center py-8 text-muted-foreground">{t('releasesPage.no_releases')}</TableCell></TableRow>
                            ) : releases.map(r => (
                                <TableRow key={r.id}>
                                    <TableCell className="font-mono font-semibold">{r.version}</TableCell>
                                    <TableCell className="text-sm text-muted-foreground">{formatDate(r.published_at)}</TableCell>
                                    <TableCell>
                                        {r.is_stable
                                            ? <Badge className="bg-green-600 text-white">{t('releasesPage.col_stable')}</Badge>
                                            : <Badge variant="outline">{t('releasesPage.prerelease')}</Badge>}
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
                        <CardTitle className="text-base">{t('releasesPage.fleet_status')}</CardTitle>
                        <Button variant="outline" size="sm" onClick={exportCSV}>
                            <Download className="h-4 w-4 mr-2" />
                            {t('releasesPage.export_csv')}
                        </Button>
                    </div>
                </CardHeader>
                <CardContent className="p-0">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>{t('common.organization')}</TableHead>
                                <TableHead>{t('releasesPage.version')}</TableHead>
                                <TableHead>{t('releasesPage.col_status')}</TableHead>
                                <TableHead>{t('releasesPage.col_approved_at')}</TableHead>
                                <TableHead>{t('releasesPage.col_completed_at')}</TableHead>
                                <TableHead>{t('releasesPage.col_error')}</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {loading ? (
                                <TableRow><TableCell colSpan={6} className="text-center py-8 text-muted-foreground">{t('common.loading')}</TableCell></TableRow>
                            ) : fleet.length === 0 ? (
                                <TableRow><TableCell colSpan={6} className="text-center py-8 text-muted-foreground">{t('releasesPage.no_fleet')}</TableCell></TableRow>
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
