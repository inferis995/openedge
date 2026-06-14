import { useEffect, useMemo, useState } from 'react';
import { AlertCircle, Bell, BellRing, CheckCircle2, Eye, EyeOff, Loader2, Mail, MessageCircle, Send, AlertTriangle } from 'lucide-react';

import { systemApi, NotificationSettings as NS, NotificationTestResult } from '@/api/system';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';

// One panel per channel + one for the shared filters. The component
// owns its own state and save/test logic so SystemPage doesn't grow
// another 200 lines of fields.

type Toast = { kind: 'success' | 'error' | 'info'; text: string } | null;

const SEVERITIES = [
    { value: 'low',      label: 'Low (everything)' },
    { value: 'medium',   label: 'Medium and up' },
    { value: 'high',     label: 'High and up' },
    { value: 'critical', label: 'Critical only' },
];

const isTrue = (v: string | undefined): boolean => v === 'true';

// SecretInput renders a password-style field with a show/hide toggle and
// a clear "stored — leave blank to keep" placeholder so the operator
// knows their existing credential is still in the DB.
const SecretInput = ({
    id, value, onChange, placeholder, autoComplete,
}: { id: string; value: string; onChange: (v: string) => void; placeholder?: string; autoComplete?: string }) => {
    const [visible, setVisible] = useState(false);
    return (
        <div className="relative">
            <Input
                id={id}
                type={visible ? 'text' : 'password'}
                value={value}
                onChange={(e) => onChange(e.target.value)}
                placeholder={placeholder ?? '••••••••  (stored — leave blank to keep)'}
                autoComplete={autoComplete ?? 'new-password'}
                className="pr-10"
            />
            <button
                type="button"
                onClick={() => setVisible((v) => !v)}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                aria-label={visible ? 'Hide' : 'Show'}
            >
                {visible ? <EyeOff size={16} /> : <Eye size={16} />}
            </button>
        </div>
    );
};

interface Props {
    /** Initial values loaded by the parent (typically from getSettings).
     *  Keys not present are filled with sensible defaults below. */
    initial?: NS;
    /** Called when the operator saves successfully — used by the parent
     *  to refresh its own copy of the settings. */
    onSaved?: () => void;
}

const NotificationsSettings = ({ initial, onSaved }: Props) => {
    // ── Form state ──────────────────────────────────────────────────────
    const [emailEnabled, setEmailEnabled] = useState(false);
    const [emailHost, setEmailHost] = useState('');
    const [emailPort, setEmailPort] = useState('587');
    const [emailUseTLS, setEmailUseTLS] = useState(false);
    const [emailUser, setEmailUser] = useState('');
    const [emailPassword, setEmailPassword] = useState(''); // stays empty unless operator edits
    const [emailFrom, setEmailFrom] = useState('');
    const [emailTo, setEmailTo] = useState('');

    const [tgEnabled, setTgEnabled] = useState(false);
    const [tgToken, setTgToken] = useState('');
    const [tgChatID, setTgChatID] = useState('');

    // Slack
    const [slackEnabled, setSlackEnabled] = useState(false);
    const [slackWebhookUrl, setSlackWebhookUrl] = useState('');

    // Microsoft Teams
    const [teamsEnabled, setTeamsEnabled] = useState(false);
    const [teamsWebhookUrl, setTeamsWebhookUrl] = useState('');

    // PagerDuty
    const [pdEnabled, setPdEnabled] = useState(false);
    const [pdRoutingKey, setPdRoutingKey] = useState('');

    const [minSeverity, setMinSeverity] = useState('medium');
    const [onCleared, setOnCleared] = useState(false);
    const [rateLimit, setRateLimit] = useState('60');

    const [saving, setSaving] = useState(false);
    const [testing, setTesting] = useState(false);
    const [toast, setToast] = useState<Toast>(null);
    const [testResult, setTestResult] = useState<NotificationTestResult | null>(null);

    // Seed the form when initial settings arrive (or change after a save).
    useEffect(() => {
        if (!initial) return;
        setEmailEnabled(isTrue(initial.notif_email_enabled));
        setEmailHost(initial.notif_email_smtp_host ?? '');
        setEmailPort(initial.notif_email_smtp_port ?? '587');
        setEmailUseTLS(isTrue(initial.notif_email_use_tls));
        setEmailUser(initial.notif_email_username ?? '');
        // password stays blank — the server returned '' for masking
        setEmailFrom(initial.notif_email_from ?? '');
        setEmailTo(initial.notif_email_to ?? '');

        setTgEnabled(isTrue(initial.notif_telegram_enabled));
        setTgChatID(initial.notif_telegram_chat_id ?? '');

        setSlackEnabled(isTrue(initial.notif_slack_enabled));
        setSlackWebhookUrl(initial.notif_slack_webhook_url ?? '');

        setTeamsEnabled(isTrue(initial.notif_teams_enabled));
        setTeamsWebhookUrl(initial.notif_teams_webhook_url ?? '');

        setPdEnabled(isTrue(initial.notif_pagerduty_enabled));
        // routing key stays blank — masked on server
        setPdRoutingKey('');

        setMinSeverity(initial.notif_min_severity ?? 'medium');
        setOnCleared(isTrue(initial.notif_on_cleared));
        setRateLimit(initial.notif_rate_limit_per_min ?? '60');
    }, [initial]);

    // ── Validation ──────────────────────────────────────────────────────
    const emailErrors = useMemo(() => {
        if (!emailEnabled) return [] as string[];
        const errs: string[] = [];
        if (!emailHost.trim()) errs.push('SMTP host is required');
        const port = parseInt(emailPort, 10);
        if (isNaN(port) || port < 1 || port > 65535) errs.push('SMTP port must be 1–65535');
        if (!emailFrom.trim()) errs.push('From address is required');
        if (!emailTo.trim()) errs.push('At least one recipient is required');
        return errs;
    }, [emailEnabled, emailHost, emailPort, emailFrom, emailTo]);

    const tgErrors = useMemo(() => {
        if (!tgEnabled) return [] as string[];
        const errs: string[] = [];
        if (!tgChatID.trim()) errs.push('Chat ID is required');
        return errs;
    }, [tgEnabled, tgChatID]);

    const slackErrors = useMemo(() => {
        if (!slackEnabled) return [] as string[];
        if (!slackWebhookUrl.trim()) return ['Webhook URL is required'];
        return [];
    }, [slackEnabled, slackWebhookUrl]);

    const teamsErrors = useMemo(() => {
        if (!teamsEnabled) return [] as string[];
        if (!teamsWebhookUrl.trim()) return ['Webhook URL is required'];
        return [];
    }, [teamsEnabled, teamsWebhookUrl]);

    const canSave = emailErrors.length === 0 && tgErrors.length === 0 &&
        slackErrors.length === 0 && teamsErrors.length === 0 && !saving;

    // ── Save ────────────────────────────────────────────────────────────
    const handleSave = async () => {
        setSaving(true);
        setToast(null);
        try {
            const payload = {
                notif_email_enabled: emailEnabled ? 'true' : 'false',
                notif_email_smtp_host: emailHost.trim(),
                notif_email_smtp_port: emailPort.trim(),
                notif_email_use_tls: emailUseTLS ? 'true' : 'false',
                notif_email_username: emailUser,
                notif_email_password: emailPassword,
                notif_email_from: emailFrom.trim(),
                notif_email_to: emailTo.trim(),

                notif_telegram_enabled: tgEnabled ? 'true' : 'false',
                notif_telegram_bot_token: tgToken,
                notif_telegram_chat_id: tgChatID.trim(),

                notif_slack_enabled: slackEnabled ? 'true' : 'false',
                notif_slack_webhook_url: slackWebhookUrl.trim(),

                notif_teams_enabled: teamsEnabled ? 'true' : 'false',
                notif_teams_webhook_url: teamsWebhookUrl.trim(),

                notif_pagerduty_enabled: pdEnabled ? 'true' : 'false',
                notif_pagerduty_routing_key: pdRoutingKey,

                notif_min_severity: minSeverity,
                notif_on_cleared: onCleared ? 'true' : 'false',
                notif_rate_limit_per_min: rateLimit,
            } as NS;
            await systemApi.updateNotifications(payload);
            setToast({ kind: 'success', text: 'Notification settings saved.' });
            setEmailPassword('');
            setTgToken('');
            setPdRoutingKey('');
            onSaved?.();
        } catch (e: unknown) {
            setToast({ kind: 'error', text: `Save failed: ${(e as Error)?.message ?? 'unknown error'}` });
        } finally {
            setSaving(false);
        }
    };

    // ── Test ────────────────────────────────────────────────────────────
    const handleTest = async () => {
        setTesting(true);
        setTestResult(null);
        try {
            const r = await systemApi.testNotifications();
            setTestResult(r);
        } catch (e: unknown) {
            setTestResult({ ok: false, errors: [(e as Error)?.message ?? 'request failed'] });
        } finally {
            setTesting(false);
        }
    };

    // ── Render ──────────────────────────────────────────────────────────
    return (
        <Card className="border-border shadow-sm bg-card">
            <CardHeader className="pb-4 border-b border-border">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                        <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                            <BellRing className="h-4 w-4 text-primary" />
                        </div>
                        <div>
                            <CardTitle className="text-base text-foreground">Notifications</CardTitle>
                            <CardDescription className="text-xs mt-0.5">
                                Send alarm events to email and Telegram. Configure once; every
                                alarm above the chosen severity is dispatched out-of-band.
                            </CardDescription>
                        </div>
                    </div>
                    <div className="flex flex-wrap gap-2">
                        {emailEnabled && <Badge className="bg-blue-500/10 text-blue-500 border-none">Email</Badge>}
                        {tgEnabled && <Badge className="bg-emerald-500/10 text-emerald-500 border-none">Telegram</Badge>}
                        {slackEnabled && <Badge className="bg-purple-500/10 text-purple-500 border-none">Slack</Badge>}
                        {teamsEnabled && <Badge className="bg-indigo-500/10 text-indigo-500 border-none">Teams</Badge>}
                        {pdEnabled && <Badge className="bg-green-500/10 text-green-500 border-none">PagerDuty</Badge>}
                        {!emailEnabled && !tgEnabled && !slackEnabled && !teamsEnabled && !pdEnabled && (
                            <Badge className="bg-slate-500/10 text-slate-300 border-none">No channel enabled</Badge>
                        )}
                    </div>
                </div>
            </CardHeader>
            <CardContent className="pt-5 space-y-6">

                {/* ── Filters (apply to both channels) ───────────────── */}
                <section className="space-y-3">
                    <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                        <Bell size={14} /> Delivery rules
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                        <div className="grid gap-1">
                            <Label htmlFor="ns-severity">Minimum severity</Label>
                            <select
                                id="ns-severity"
                                value={minSeverity}
                                onChange={(e) => setMinSeverity(e.target.value)}
                                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                            >
                                {SEVERITIES.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
                            </select>
                            <p className="text-xs text-muted-foreground">Events below this level are silently dropped.</p>
                        </div>
                        <div className="grid gap-1">
                            <Label htmlFor="ns-rate">Rate limit (events/min)</Label>
                            <Input id="ns-rate" type="number" min={1} max={1000}
                                value={rateLimit} onChange={(e) => setRateLimit(e.target.value)} />
                            <p className="text-xs text-muted-foreground">Cap across all channels — survives flapping sensors.</p>
                        </div>
                        <div className="flex flex-col gap-1">
                            <Label className="pt-0.5">On CLEARED events</Label>
                            <div className="flex items-center gap-2 h-10">
                                <Switch checked={onCleared} onCheckedChange={setOnCleared} />
                                <span className="text-xs text-muted-foreground">
                                    {onCleared ? 'Notify when alarms clear' : 'Only on ACTIVE'}
                                </span>
                            </div>
                        </div>
                    </div>
                </section>

                {/* ── Email channel ─────────────────────────────────── */}
                <section className="space-y-3 rounded-md border p-4">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2 text-sm font-semibold">
                            <Mail size={14} /> Email
                        </div>
                        <Switch checked={emailEnabled} onCheckedChange={setEmailEnabled} />
                    </div>
                    {emailEnabled && (
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                            <div className="grid gap-1">
                                <Label htmlFor="em-host">SMTP host</Label>
                                <Input id="em-host" value={emailHost} onChange={(e) => setEmailHost(e.target.value)}
                                    placeholder="smtp.gmail.com" />
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="em-port">Port</Label>
                                <Input id="em-port" type="number" value={emailPort} onChange={(e) => setEmailPort(e.target.value)}
                                    placeholder="587" />
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="em-user">Username</Label>
                                <Input id="em-user" value={emailUser} onChange={(e) => setEmailUser(e.target.value)}
                                    placeholder="alerts@yourcompany.com" autoComplete="username" />
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="em-pass">Password</Label>
                                <SecretInput id="em-pass" value={emailPassword} onChange={setEmailPassword}
                                    autoComplete="new-password" />
                                <p className="text-xs text-muted-foreground">
                                    Gmail: use an{' '}
                                    <a className="underline" href="https://myaccount.google.com/apppasswords"
                                        target="_blank" rel="noreferrer">app password</a> — not your account password.
                                </p>
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="em-from">From address</Label>
                                <Input id="em-from" value={emailFrom} onChange={(e) => setEmailFrom(e.target.value)}
                                    placeholder="alerts@yourcompany.com" />
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="em-to">To (comma-separated)</Label>
                                <Input id="em-to" value={emailTo} onChange={(e) => setEmailTo(e.target.value)}
                                    placeholder="op1@company.com, op2@company.com" />
                            </div>
                            <div className="md:col-span-2 flex items-center gap-3">
                                <Switch checked={emailUseTLS} onCheckedChange={setEmailUseTLS} />
                                <div>
                                    <p className="text-sm">Implicit TLS</p>
                                    <p className="text-xs text-muted-foreground">
                                        On = port 465 (full TLS). Off = port 587 (STARTTLS, recommended).
                                    </p>
                                </div>
                            </div>
                            {emailErrors.length > 0 && (
                                <div className="md:col-span-2 text-xs text-red-500 flex items-start gap-1">
                                    <AlertCircle size={14} className="mt-0.5" />
                                    <ul className="list-disc list-inside">{emailErrors.map((e) => <li key={e}>{e}</li>)}</ul>
                                </div>
                            )}
                        </div>
                    )}
                </section>

                {/* ── Telegram channel ──────────────────────────────── */}
                <section className="space-y-3 rounded-md border p-4">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2 text-sm font-semibold">
                            <MessageCircle size={14} /> Telegram
                        </div>
                        <Switch checked={tgEnabled} onCheckedChange={setTgEnabled} />
                    </div>
                    {tgEnabled && (
                        <div className="space-y-3">
                            <div className="text-xs text-muted-foreground bg-muted/40 rounded p-2 leading-relaxed">
                                <strong>One-time setup:</strong> talk to{' '}
                                <a className="underline" href="https://t.me/BotFather" target="_blank" rel="noreferrer">@BotFather</a>{' '}
                                → <code>/newbot</code> → paste the token below. Add the bot to a group
                                (or DM it), then open{' '}
                                <code>https://api.telegram.org/bot&lt;TOKEN&gt;/getUpdates</code> to read
                                the <code>chat.id</code> (negative for groups, positive for DMs).
                            </div>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                                <div className="grid gap-1 md:col-span-2">
                                    <Label htmlFor="tg-token">Bot token</Label>
                                    <SecretInput id="tg-token" value={tgToken} onChange={setTgToken}
                                        placeholder="••••••••  (stored — leave blank to keep)" />
                                </div>
                                <div className="grid gap-1">
                                    <Label htmlFor="tg-chat">Chat ID</Label>
                                    <Input id="tg-chat" value={tgChatID} onChange={(e) => setTgChatID(e.target.value)}
                                        placeholder="-1001234567890 or 123456789" />
                                </div>
                            </div>
                            {tgErrors.length > 0 && (
                                <div className="text-xs text-red-500 flex items-start gap-1">
                                    <AlertCircle size={14} className="mt-0.5" />
                                    <ul className="list-disc list-inside">{tgErrors.map((e) => <li key={e}>{e}</li>)}</ul>
                                </div>
                            )}
                        </div>
                    )}
                </section>

                {/* ── Slack channel ─────────────────────────────────── */}
                <section className="space-y-3 rounded-md border p-4">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2 text-sm font-semibold">
                            {/* Slack logo */}
                            <svg className="h-4 w-4" viewBox="0 0 122.8 122.8" fill="none">
                                <path d="M25.8 77.6c0 7.1-5.8 12.9-12.9 12.9S0 84.7 0 77.6s5.8-12.9 12.9-12.9h12.9v12.9z" fill="#E01E5A"/>
                                <path d="M32.3 77.6c0-7.1 5.8-12.9 12.9-12.9s12.9 5.8 12.9 12.9v32.3c0 7.1-5.8 12.9-12.9 12.9s-12.9-5.8-12.9-12.9V77.6z" fill="#E01E5A"/>
                                <path d="M45.2 25.8c-7.1 0-12.9-5.8-12.9-12.9S38.1 0 45.2 0s12.9 5.8 12.9 12.9v12.9H45.2z" fill="#36C5F0"/>
                                <path d="M45.2 32.3c7.1 0 12.9 5.8 12.9 12.9s-5.8 12.9-12.9 12.9H12.9C5.8 58.1 0 52.3 0 45.2s5.8-12.9 12.9-12.9H45.2z" fill="#36C5F0"/>
                                <path d="M97 45.2c0-7.1 5.8-12.9 12.9-12.9s12.9 5.8 12.9 12.9-5.8 12.9-12.9 12.9H97V45.2z" fill="#2EB67D"/>
                                <path d="M90.5 45.2c0 7.1-5.8 12.9-12.9 12.9s-12.9-5.8-12.9-12.9V12.9C64.7 5.8 70.5 0 77.6 0s12.9 5.8 12.9 12.9v32.3z" fill="#2EB67D"/>
                                <path d="M77.6 97c7.1 0 12.9 5.8 12.9 12.9s-5.8 12.9-12.9 12.9-12.9-5.8-12.9-12.9V97h12.9z" fill="#ECB22E"/>
                                <path d="M77.6 90.5c-7.1 0-12.9-5.8-12.9-12.9s5.8-12.9 12.9-12.9h32.3c7.1 0 12.9 5.8 12.9 12.9s-5.8 12.9-12.9 12.9H77.6z" fill="#ECB22E"/>
                            </svg>
                            Slack
                        </div>
                        <Switch checked={slackEnabled} onCheckedChange={setSlackEnabled} />
                    </div>
                    {slackEnabled && (
                        <div className="space-y-3">
                            <div className="text-xs text-muted-foreground bg-muted/40 rounded p-2 leading-relaxed">
                                <strong>Setup:</strong> In Slack, create an App → Incoming Webhooks → Activate → Add to channel → copy the URL below.
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="slack-url">Incoming Webhook URL</Label>
                                <Input id="slack-url" value={slackWebhookUrl}
                                    onChange={(e) => setSlackWebhookUrl(e.target.value)}
                                    placeholder="https://hooks.slack.com/services/T.../B.../..." />
                            </div>
                            {slackErrors.length > 0 && (
                                <div className="text-xs text-red-500 flex items-start gap-1">
                                    <AlertCircle size={14} className="mt-0.5" />
                                    <ul className="list-disc list-inside">{slackErrors.map((e) => <li key={e}>{e}</li>)}</ul>
                                </div>
                            )}
                        </div>
                    )}
                </section>

                {/* ── Microsoft Teams channel ────────────────────────── */}
                <section className="space-y-3 rounded-md border p-4">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2 text-sm font-semibold">
                            {/* Teams icon */}
                            <svg className="h-4 w-4" viewBox="0 0 23 23">
                                <path fill="#f3f3f3" d="M0 0h23v23H0z"/>
                                <path fill="#5059C9" d="M13 7.5h5v5.5a4 4 0 0 1-4 4h-1v-9.5z"/>
                                <path fill="#7B83EB" d="M15 7.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z"/>
                                <path fill="#5059C9" d="M15.5 7.5H11a.5.5 0 0 0-.5.5V13a4.5 4.5 0 0 0 9 0V8a.5.5 0 0 0-.5-.5h-3.5z"/>
                                <path fill="#4B53BC" d="M15.5 7.5h-5v5.57A4.5 4.5 0 0 0 15 17.5h.5V7.5z"/>
                                <path fill="#7B83EB" d="M11 12.5V8a.5.5 0 0 1 .5-.5h4.5v6a4.5 4.5 0 0 1-4.5 4.5 4.5 4.5 0 0 1-4.5-4.5V8a.5.5 0 0 1 .5-.5h3.5z"/>
                                <path fill="#3D5A98" opacity=".1" d="M11.5 7.5h-1a.5.5 0 0 0-.5.5v5.5a4 4 0 0 0 2 3.46V7.5z"/>
                                <path fill="#3D5A98" opacity=".2" d="M11 7.5h-.5a.5.5 0 0 0-.5.5v5.5a4 4 0 0 0 2 3.46V8a.5.5 0 0 1 .5-.5H11z"/>
                                <path fill="#4B53BC" opacity=".2" d="M11.5 7.5v10.5a4 4 0 0 1-2-3.5V8a.5.5 0 0 1 .5-.5h1.5z"/>
                                <rect fill="#5059C9" x="3" y="7.5" width="9" height="9" rx="4.5"/>
                            </svg>
                            Microsoft Teams
                        </div>
                        <Switch checked={teamsEnabled} onCheckedChange={setTeamsEnabled} />
                    </div>
                    {teamsEnabled && (
                        <div className="space-y-3">
                            <div className="text-xs text-muted-foreground bg-muted/40 rounded p-2 leading-relaxed">
                                <strong>Setup:</strong> In Teams, open a channel → ··· → Connectors → Incoming Webhook → Configure → copy URL below.
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="teams-url">Incoming Webhook URL</Label>
                                <Input id="teams-url" value={teamsWebhookUrl}
                                    onChange={(e) => setTeamsWebhookUrl(e.target.value)}
                                    placeholder="https://yourorg.webhook.office.com/webhookb2/..." />
                            </div>
                            {teamsErrors.length > 0 && (
                                <div className="text-xs text-red-500 flex items-start gap-1">
                                    <AlertCircle size={14} className="mt-0.5" />
                                    <ul className="list-disc list-inside">{teamsErrors.map((e) => <li key={e}>{e}</li>)}</ul>
                                </div>
                            )}
                        </div>
                    )}
                </section>

                {/* ── PagerDuty channel ────────────────────────────── */}
                <section className="space-y-3 rounded-md border p-4">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2 text-sm font-semibold">
                            <AlertTriangle size={14} className="text-green-600" />
                            PagerDuty
                        </div>
                        <Switch checked={pdEnabled} onCheckedChange={setPdEnabled} />
                    </div>
                    {pdEnabled && (
                        <div className="space-y-3">
                            <div className="text-xs text-muted-foreground bg-muted/40 rounded p-2 leading-relaxed">
                                <strong>Setup:</strong> PagerDuty → Services → Integrations → Add Integration → Events API v2 → copy Routing Key below.
                                Alarms trigger incidents; CLEARED events auto-resolve them.
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="pd-key">Routing Key (32 hex chars)</Label>
                                <SecretInput id="pd-key" value={pdRoutingKey} onChange={setPdRoutingKey}
                                    placeholder="••••••••  (stored — leave blank to keep)" />
                            </div>
                        </div>
                    )}
                </section>

                {/* ── Actions ──────────────────────────────────────── */}
                <div className="flex items-center gap-3 pt-2 border-t">
                    <Button onClick={handleSave} disabled={!canSave}>
                        {saving && <Loader2 size={16} className="mr-2 animate-spin" />}
                        {saving ? 'Saving...' : 'Save'}
                    </Button>
                    <Button variant="outline" onClick={handleTest}
                        disabled={testing || (!emailEnabled && !tgEnabled && !slackEnabled && !teamsEnabled && !pdEnabled)}>
                        {testing
                            ? <><Loader2 size={16} className="mr-2 animate-spin" /> Sending test...</>
                            : <><Send size={16} className="mr-2" /> Send test</>}
                    </Button>
                    {toast && (
                        <span className={`text-sm flex items-center gap-1 ${
                            toast.kind === 'success' ? 'text-emerald-500' :
                            toast.kind === 'error' ? 'text-red-500' : 'text-muted-foreground'
                        }`}>
                            {toast.kind === 'success'
                                ? <CheckCircle2 size={14} />
                                : toast.kind === 'error'
                                ? <AlertCircle size={14} />
                                : null}
                            {toast.text}
                        </span>
                    )}
                </div>

                {/* ── Test result ──────────────────────────────────── */}
                {testResult && (
                    <div className={`rounded-md p-3 border text-sm ${
                        testResult.ok
                            ? 'border-emerald-500/30 bg-emerald-500/5 text-emerald-600 dark:text-emerald-400'
                            : 'border-red-500/30 bg-red-500/5'
                    }`}>
                        {testResult.ok ? (
                            <div className="flex items-center gap-2">
                                <CheckCircle2 size={16} /> Test event delivered to every enabled channel.
                            </div>
                        ) : (
                            <div className="space-y-1">
                                <div className="flex items-center gap-2 text-red-500 font-medium">
                                    <AlertCircle size={16} /> One or more channels failed:
                                </div>
                                <ul className="list-disc list-inside text-xs">
                                    {testResult.errors.map((e, i) => <li key={i}>{e}</li>)}
                                </ul>
                            </div>
                        )}
                    </div>
                )}
            </CardContent>
        </Card>
    );
};

export default NotificationsSettings;
