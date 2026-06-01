import { useEffect, useMemo, useState } from 'react';
import { AlertCircle, CheckCircle2, Clock, HardDrive, Loader2, ShieldCheck } from 'lucide-react';

import { systemApi, GlobalSettings } from '@/api/system';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';

// Cron presets the operator picks 95% of the time. Picking "custom"
// surfaces the raw expression for the remaining 5%.
const SCHEDULES = [
    { value: '0 3 * * *',  label: 'Daily at 03:00 UTC (recommended)' },
    { value: '0 2 * * *',  label: 'Daily at 02:00 UTC' },
    { value: '0 4 * * *',  label: 'Daily at 04:00 UTC' },
    { value: '0 */6 * * *', label: 'Every 6 hours' },
    { value: '0 */12 * * *', label: 'Every 12 hours' },
    { value: 'custom',      label: 'Custom (advanced)' },
];

interface Props {
    initial?: GlobalSettings | null;
    onSaved?: () => void;
}

type Toast = { kind: 'success' | 'error'; text: string } | null;

const BackupConfig = ({ initial, onSaved }: Props) => {
    const [enabled, setEnabled] = useState(true);
    const [preset, setPreset] = useState<string>('0 3 * * *');
    const [customCron, setCustomCron] = useState<string>('0 3 * * *');
    const [retention, setRetention] = useState<string>('30');
    // age recipient is a PUBLIC key (the encryption target). Safe to
    // display + safe to allow explicit clearing by saving an empty
    // string. The corresponding private key never touches OpenEdge.
    const [ageRecipient, setAgeRecipient] = useState<string>('');

    const [saving, setSaving] = useState(false);
    const [toast, setToast] = useState<Toast>(null);

    // Hydrate from server.
    useEffect(() => {
        if (!initial) return;
        const ena = initial.backup_enabled ?? 'true';
        setEnabled(ena !== 'false');

        const sched = initial.backup_schedule ?? '0 3 * * *';
        const knownPreset = SCHEDULES.find((s) => s.value === sched);
        if (knownPreset) {
            setPreset(sched);
        } else {
            setPreset('custom');
            setCustomCron(sched);
        }
        setRetention(initial.backup_retention_days ?? '30');
        setAgeRecipient(initial.backup_age_recipient ?? '');
    }, [initial]);

    // ── Validation ──────────────────────────────────────────────────────
    const errors = useMemo(() => {
        const errs: string[] = [];
        if (!enabled) return errs;
        const r = parseInt(retention, 10);
        if (isNaN(r) || r < 1 || r > 3650) errs.push('Retention must be between 1 and 3650 days');
        const cron = preset === 'custom' ? customCron : preset;
        // Minimal cron syntax check: 5 space-separated fields. Real cron
        // grammar (ranges, steps, names) is too complex to validate
        // inline — alpine cron will reject malformed entries at runtime
        // and the operator sees the error in the container log.
        if (cron.trim().split(/\s+/).length !== 5) errs.push('Cron expression must have 5 fields');
        return errs;
    }, [enabled, retention, preset, customCron]);

    const canSave = errors.length === 0 && !saving;

    const handleSave = async () => {
        setSaving(true);
        setToast(null);
        try {
            const schedule = preset === 'custom' ? customCron.trim() : preset;
            await systemApi.updateSettings({
                backup: {
                    backup_enabled: enabled ? 'true' : 'false',
                    backup_schedule: schedule,
                    backup_retention_days: retention.trim(),
                    backup_age_recipient: ageRecipient.trim(), // public key — empty disables encryption
                },
            } as Parameters<typeof systemApi.updateSettings>[0]);
            setToast({ kind: 'success', text: 'Backup settings saved. Restart the backup container to apply.' });
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
                                Nightly PostgreSQL dumps with optional age encryption + auto-prune.
                            </CardDescription>
                        </div>
                    </div>
                    {enabled
                        ? <Badge className="bg-emerald-500/10 text-emerald-500 border-none">Enabled</Badge>
                        : <Badge className="bg-slate-500/10 text-slate-300 border-none">Disabled</Badge>}
                </div>
            </CardHeader>
            <CardContent className="pt-5 space-y-5">

                <div className="flex items-center gap-3">
                    <Switch checked={enabled} onCheckedChange={setEnabled} />
                    <div className="text-sm">
                        <p>Automatic backups</p>
                        <p className="text-xs text-muted-foreground">
                            When off, only manual `make backup-now` runs are possible.
                        </p>
                    </div>
                </div>

                {enabled && (
                    <>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="grid gap-1">
                                <Label htmlFor="bk-preset" className="flex items-center gap-1">
                                    <Clock size={12} /> Schedule
                                </Label>
                                <select
                                    id="bk-preset"
                                    value={preset}
                                    onChange={(e) => setPreset(e.target.value)}
                                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                                >
                                    {SCHEDULES.map((s) => (
                                        <option key={s.value} value={s.value}>{s.label}</option>
                                    ))}
                                </select>
                                {preset === 'custom' ? (
                                    <Input
                                        value={customCron}
                                        onChange={(e) => setCustomCron(e.target.value)}
                                        placeholder="0 3 * * *"
                                        className="font-mono text-xs"
                                    />
                                ) : (
                                    <p className="text-xs text-muted-foreground">
                                        Cron expression: <code>{preset}</code>
                                    </p>
                                )}
                            </div>

                            <div className="grid gap-1">
                                <Label htmlFor="bk-retention">Retention (days)</Label>
                                <Input id="bk-retention" type="number" min={1} max={3650}
                                    value={retention} onChange={(e) => setRetention(e.target.value)} />
                                <p className="text-xs text-muted-foreground">
                                    Backup files older than this are auto-pruned. 30 days covers most
                                    "we noticed last Tuesday" scenarios.
                                </p>
                            </div>
                        </div>

                        <div className="space-y-2 rounded-md border p-3 bg-muted/20">
                            <div className="flex items-center gap-2 text-sm font-semibold">
                                <ShieldCheck size={14} /> Encryption (age)
                            </div>
                            <p className="text-xs text-muted-foreground">
                                Set an <code>age</code> public key here to encrypt every dump.
                                Required when backups will leave the host (USB, NAS, off-site).
                                Generate the key pair OFF the server with{' '}
                                <code>age-keygen -o age-key.txt</code> and keep the private key
                                somewhere safe.
                            </p>
                            <Input
                                value={ageRecipient}
                                onChange={(e) => setAgeRecipient(e.target.value)}
                                placeholder="age1..."
                                className="font-mono text-xs"
                            />
                            <p className="text-xs text-muted-foreground">
                                Empty = plaintext dumps (acceptable when the backup directory is
                                on an encrypted disk and never copied off the host). The age
                                recipient is the PUBLIC key — safe to display and version.
                            </p>
                        </div>
                    </>
                )}

                {errors.length > 0 && (
                    <div className="text-xs text-red-500 flex items-start gap-1">
                        <AlertCircle size={14} className="mt-0.5" />
                        <ul className="list-disc list-inside">{errors.map((e) => <li key={e}>{e}</li>)}</ul>
                    </div>
                )}

                <div className="flex items-center gap-3 pt-2 border-t">
                    <Button onClick={handleSave} disabled={!canSave}>
                        {saving && <Loader2 size={16} className="mr-2 animate-spin" />}
                        {saving ? 'Saving...' : 'Save'}
                    </Button>
                    {toast && (
                        <span className={`text-sm flex items-center gap-1 ${
                            toast.kind === 'success' ? 'text-emerald-500' : 'text-red-500'
                        }`}>
                            {toast.kind === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                            {toast.text}
                        </span>
                    )}
                </div>

                <p className="text-xs text-muted-foreground">
                    💡 Settings persist immediately. The cron schedule is read at container
                    start, so restart the backup service to apply changes:{' '}
                    <code>docker compose restart backup</code>.
                </p>
            </CardContent>
        </Card>
    );
};

export default BackupConfig;
