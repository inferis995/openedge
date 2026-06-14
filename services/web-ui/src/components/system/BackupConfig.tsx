import { useEffect, useMemo, useState } from 'react';
import { AlertCircle, CheckCircle2, ChevronDown, ChevronUp, Clock, HardDrive, Loader2, ShieldCheck, Upload } from 'lucide-react';

import { systemApi, GlobalSettings } from '@/api/system';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';

const SCHEDULES = [
    { value: '0 3 * * *',    label: 'Daily at 03:00 UTC (recommended)' },
    { value: '0 2 * * *',    label: 'Daily at 02:00 UTC' },
    { value: '0 4 * * *',    label: 'Daily at 04:00 UTC' },
    { value: '0 */6 * * *',  label: 'Every 6 hours' },
    { value: '0 */12 * * *', label: 'Every 12 hours' },
    { value: 'custom',       label: 'Custom (advanced)' },
];

interface Props {
    initial?: GlobalSettings | null;
    onSaved?: () => void;
}

type Toast = { kind: 'success' | 'error'; text: string } | null;

interface AuditEntry {
    id: number;
    action: string;
    filename: string;
    user_email: string;
    ip_addr: string;
    details: string;
    created_at: string;
}

const actionColor = (action: string) => {
    if (action.includes('failed'))  return 'bg-red-500/10 text-red-400';
    if (action.includes('restore')) return 'bg-amber-500/10 text-amber-400';
    if (action.includes('deleted')) return 'bg-slate-500/10 text-slate-300';
    return 'bg-emerald-500/10 text-emerald-400';
};

const BackupConfig = ({ initial, onSaved }: Props) => {
    const [enabled, setEnabled]           = useState(true);
    const [preset, setPreset]             = useState('0 3 * * *');
    const [customCron, setCustomCron]     = useState('0 3 * * *');
    const [retention, setRetention]       = useState('30');
    const [ageRecipient, setAgeRecipient] = useState('');

    // S3 / MinIO
    const [s3Enabled, setS3Enabled]       = useState(false);
    const [s3Bucket, setS3Bucket]         = useState('');
    const [s3Endpoint, setS3Endpoint]     = useState('');
    const [s3Region, setS3Region]         = useState('us-east-1');
    const [s3AccessKey, setS3AccessKey]   = useState('');
    const [s3SecretKey, setS3SecretKey]   = useState('');

    const [saving, setSaving]             = useState(false);
    const [toast, setToast]               = useState<Toast>(null);
    const [showAudit, setShowAudit]       = useState(false);
    const [audit, setAudit]               = useState<AuditEntry[]>([]);
    const [auditLoading, setAuditLoading] = useState(false);

    useEffect(() => {
        if (!initial) return;
        setEnabled((initial.backup_enabled ?? 'true') !== 'false');

        const sched = initial.backup_schedule ?? '0 3 * * *';
        const known = SCHEDULES.find(s => s.value === sched);
        setPreset(known ? sched : 'custom');
        if (!known) setCustomCron(sched);

        setRetention(initial.backup_retention_days ?? '30');
        setAgeRecipient(initial.backup_age_recipient ?? '');


        setS3Enabled(initial.backup_s3_enabled === 'true');
        setS3Bucket(initial.backup_s3_bucket ?? '');
        setS3Endpoint(initial.backup_s3_endpoint ?? '');
        setS3Region(initial.backup_s3_region ?? 'us-east-1');
        setS3AccessKey(initial.backup_s3_access_key ?? '');
        setS3SecretKey(initial.backup_s3_secret_key ?? '');
    }, [initial]);

    const loadAudit = async () => {
        setAuditLoading(true);
        try { setAudit(await systemApi.getBackupAudit()); } catch { /* best-effort */ }
        setAuditLoading(false);
    };

    const toggleAudit = () => {
        if (!showAudit) loadAudit();
        setShowAudit(v => !v);
    };

    const errors = useMemo(() => {
        const errs: string[] = [];
        if (!enabled) return errs;
        const r = parseInt(retention, 10);
        if (isNaN(r) || r < 1 || r > 3650) errs.push('Retention must be between 1 and 3650 days');
        const cron = preset === 'custom' ? customCron : preset;
        if (cron.trim().split(/\s+/).length !== 5) errs.push('Cron expression must have 5 fields');
        if (s3Enabled && !s3Bucket) errs.push('S3 bucket name is required when S3 is enabled');
        return errs;
    }, [enabled, retention, preset, customCron, s3Enabled, s3Bucket]);

    const handleSave = async () => {
        setSaving(true);
        setToast(null);
        try {
            const schedule = preset === 'custom' ? customCron.trim() : preset;
            await systemApi.updateSettings({
                backup: {
                    backup_enabled:        enabled ? 'true' : 'false',
                    backup_schedule:       schedule,
                    backup_retention_days: retention.trim(),
                    backup_age_recipient:  ageRecipient.trim(),
                    backup_s3_enabled:     s3Enabled ? 'true' : 'false',
                    backup_s3_bucket:      s3Bucket.trim(),
                    backup_s3_endpoint:    s3Endpoint.trim(),
                    backup_s3_region:      s3Region.trim(),
                    backup_s3_access_key:  s3AccessKey.trim(),
                    backup_s3_secret_key:  s3SecretKey.trim(),
                },
            } as Parameters<typeof systemApi.updateSettings>[0]);
            setToast({ kind: 'success', text: 'Saved. Restart the backup container to apply schedule / S3 changes.' });
            onSaved?.();
        } catch (e: unknown) {
            setToast({ kind: 'error', text: `Save failed: ${(e as Error)?.message ?? 'unknown error'}` });
        } finally {
            setSaving(false);
        }
    };

    return (
        <Card className="border-border shadow-sm bg-card">
            <CardHeader className="pb-4 border-b border-border">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                        <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                            <HardDrive className="h-4 w-4 text-primary" />
                        </div>
                        <div>
                            <CardTitle className="text-base text-foreground">Backup</CardTitle>
                            <CardDescription className="text-xs mt-0.5">
                                Scheduled PostgreSQL dumps · age encryption · SHA-256 integrity · optional S3/MinIO offsite
                            </CardDescription>
                        </div>
                    </div>
                    {enabled
                        ? <Badge className="bg-emerald-500/10 text-emerald-500 border-none">Enabled</Badge>
                        : <Badge className="bg-slate-500/10 text-slate-300 border-none">Disabled</Badge>}
                </div>
            </CardHeader>

            <CardContent className="pt-5 space-y-5">
                {/* Enable toggle */}
                <div className="flex items-center gap-3">
                    <Switch checked={enabled} onCheckedChange={setEnabled} />
                    <div className="text-sm">
                        <p>Automatic backups</p>
                        <p className="text-xs text-muted-foreground">When off, only manual exports are possible.</p>
                    </div>
                </div>

                {enabled && (
                    <>
                        {/* Schedule + retention */}
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="grid gap-1">
                                <Label htmlFor="bk-preset" className="flex items-center gap-1">
                                    <Clock size={12} /> Schedule
                                </Label>
                                <select id="bk-preset" value={preset}
                                    onChange={e => setPreset(e.target.value)}
                                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
                                    {SCHEDULES.map(s => <option key={s.value} value={s.value}>{s.label}</option>)}
                                </select>
                                {preset === 'custom'
                                    ? <Input value={customCron} onChange={e => setCustomCron(e.target.value)}
                                        placeholder="0 3 * * *" className="font-mono text-xs" />
                                    : <p className="text-xs text-muted-foreground">Cron: <code>{preset}</code></p>}
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="bk-retention">Retention (days)</Label>
                                <Input id="bk-retention" type="number" min={1} max={3650}
                                    value={retention} onChange={e => setRetention(e.target.value)} />
                                <p className="text-xs text-muted-foreground">
                                    Backups older than this are auto-pruned. 30 days recommended.
                                </p>
                            </div>
                        </div>

                        {/* Age encryption */}
                        <div className="space-y-2 rounded-md border p-3 bg-muted/20">
                            <div className="flex items-center gap-2 text-sm font-semibold">
                                <ShieldCheck size={14} /> Encryption (age)
                            </div>
                            <p className="text-xs text-muted-foreground">
                                Set an <code>age</code> public key to encrypt every dump.
                                Generate offline: <code>age-keygen -o age-key.txt</code>.
                                Each backup automatically gets a <code>.sha256</code> sidecar for integrity verification.
                            </p>
                            <Input value={ageRecipient} onChange={e => setAgeRecipient(e.target.value)}
                                placeholder="age1..." className="font-mono text-xs" />
                            <p className="text-xs text-muted-foreground">
                                Empty = plaintext dumps (acceptable when the backup directory is on an encrypted disk).
                            </p>
                        </div>

                        {/* S3 / MinIO offsite */}
                        <div className="space-y-3 rounded-md border p-3 bg-muted/20">
                            <div className="flex items-center justify-between">
                                <div className="flex items-center gap-2 text-sm font-semibold">
                                    <Upload size={14} /> Offsite Storage (S3 / MinIO / R2)
                                </div>
                                <Switch checked={s3Enabled} onCheckedChange={setS3Enabled} />
                            </div>
                            {s3Enabled && (
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 pt-1">
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Bucket *</Label>
                                        <Input value={s3Bucket} onChange={e => setS3Bucket(e.target.value)}
                                            placeholder="my-openedge-backups" className="text-xs" />
                                    </div>
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Region</Label>
                                        <Input value={s3Region} onChange={e => setS3Region(e.target.value)}
                                            placeholder="us-east-1" className="text-xs" />
                                    </div>
                                    <div className="grid gap-1 md:col-span-2">
                                        <Label className="text-xs">Custom Endpoint (MinIO / Cloudflare R2 — leave empty for AWS)</Label>
                                        <Input value={s3Endpoint} onChange={e => setS3Endpoint(e.target.value)}
                                            placeholder="http://minio:9000" className="text-xs" />
                                    </div>
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Access Key ID</Label>
                                        <Input value={s3AccessKey} onChange={e => setS3AccessKey(e.target.value)}
                                            placeholder="AKIA..." className="font-mono text-xs" />
                                    </div>
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Secret Access Key</Label>
                                        <Input type="password" value={s3SecretKey}
                                            onChange={e => setS3SecretKey(e.target.value)}
                                            placeholder="••••••••" className="font-mono text-xs" />
                                    </div>
                                    <p className="md:col-span-2 text-xs text-muted-foreground">
                                        After each scheduled backup, the dump and its <code>.sha256</code> sidecar are uploaded to S3.
                                        Restart the backup container after saving to apply changes.
                                    </p>
                                </div>
                            )}
                        </div>
                    </>
                )}

                {errors.length > 0 && (
                    <div className="text-xs text-red-500 flex items-start gap-1">
                        <AlertCircle size={14} className="mt-0.5 shrink-0" />
                        <ul className="list-disc list-inside">{errors.map(e => <li key={e}>{e}</li>)}</ul>
                    </div>
                )}

                {/* Save */}
                <div className="flex items-center gap-3 pt-2 border-t">
                    <Button onClick={handleSave} disabled={errors.length > 0 || saving}>
                        {saving && <Loader2 size={16} className="mr-2 animate-spin" />}
                        {saving ? 'Saving…' : 'Save'}
                    </Button>
                    {toast && (
                        <span className={`text-sm flex items-center gap-1 ${
                            toast.kind === 'success' ? 'text-emerald-500' : 'text-red-500'}`}>
                            {toast.kind === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                            {toast.text}
                        </span>
                    )}
                </div>

                {/* Audit log */}
                <div className="border rounded-md overflow-hidden">
                    <button onClick={toggleAudit}
                        className="w-full flex items-center justify-between px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors bg-muted/10">
                        <span>Audit Log</span>
                        {showAudit ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
                    </button>
                    {showAudit && (
                        <div className="border-t max-h-64 overflow-y-auto">
                            {auditLoading && (
                                <div className="flex justify-center py-4">
                                    <Loader2 size={16} className="animate-spin text-muted-foreground" />
                                </div>
                            )}
                            {!auditLoading && audit.length === 0 && (
                                <p className="text-xs text-muted-foreground text-center py-4">No audit events yet.</p>
                            )}
                            {!auditLoading && audit.map(e => (
                                <div key={e.id} className="flex items-start gap-2 px-3 py-2 border-b last:border-0 text-xs">
                                    <Badge className={`mt-0.5 shrink-0 text-[10px] border-none ${actionColor(e.action)}`}>
                                        {e.action.replace(/_/g, ' ')}
                                    </Badge>
                                    <div className="min-w-0 flex-1">
                                        <p className="font-mono truncate text-foreground">{e.filename || '—'}</p>
                                        <p className="text-muted-foreground truncate">{e.details}</p>
                                    </div>
                                    <div className="text-right shrink-0 text-muted-foreground">
                                        <p>{e.user_email || 'system'}</p>
                                        <p>{new Date(e.created_at).toLocaleString()}</p>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>

                <p className="text-xs text-muted-foreground">
                    💡 Schedule and S3 changes require a container restart:{' '}
                    <code>docker compose restart backup</code>
                </p>
            </CardContent>
        </Card>
    );
};

export default BackupConfig;
