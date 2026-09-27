import { useState, useEffect, useCallback } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from '@/components/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import { Switch } from '@/components/ui/switch';
import {
    Download, Key, UserPlus, WifiOff, Copy, Trash2, RefreshCw, Eye, EyeOff,
    Webhook as WebhookIcon, Shield, Plus,
} from 'lucide-react';
import { organizationsApi, SSOProvider, SSOProviderInput } from '@/api/organizations';
import EdgeBoxesPanel from '@/components/EdgeBoxesPanel';
import { apiKeysApi, ApiKey } from '@/api/apiKeys';
import { invitesApi } from '@/api/invites';
import { webhooksApi, Webhook, WEBHOOK_EVENTS, WebhookEvent } from '@/api/webhooks';
import { showApiSuccess, showApiError } from '@/lib/api-error-handler';
import { toast } from 'sonner';
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';
import { useTranslation } from 'react-i18next';
import MqttClientsPanel from '@/components/MqttClientsPanel';

interface Props {
    org: { id: number; name: string };
    open: boolean;
    onOpenChange: (open: boolean) => void;
}

export default function OrgInfrastructureDialog({ org, open, onOpenChange }: Props) {
    const { t } = useTranslation();
    const qc = useQueryClient();

    // ── Edge Status ───────────────────────────────────────────────────────────
    const { data: edgeStatus, refetch: refetchEdge, isFetching: edgeFetching } = useQuery({
        queryKey: ['edge-status', org.id],
        queryFn: () => organizationsApi.getEdgeStatus(org.id),
        enabled: open,
        refetchInterval: open ? 30_000 : false,
    });

    // ── API Keys ──────────────────────────────────────────────────────────────
    const { data: apiKeys = [], isFetching: keysFetching } = useQuery({
        queryKey: ['api-keys', org.id],
        queryFn: () => apiKeysApi.list(org.id),
        enabled: open,
    });

    const [newKeyName, setNewKeyName] = useState('');
    const [shownKey, setShownKey] = useState<string | null>(null);
    const [revealedKey, setRevealedKey] = useState(false);

    const createKeyMutation = useMutation({
        mutationFn: () => apiKeysApi.create(org.id, newKeyName || 'default'),
        onSuccess: (data) => {
            qc.invalidateQueries({ queryKey: ['api-keys', org.id] });
            setShownKey(data.full_key);
            setNewKeyName('');
            setRevealedKey(false);
        },
        onError: (e) => showApiError(e, t('orgInfra.key_create_failed')),
    });

    const revokeKeyMutation = useMutation({
        mutationFn: (keyId: number) => apiKeysApi.revoke(org.id, keyId),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['api-keys', org.id] });
            showApiSuccess(t('orgInfra.key_revoked'));
        },
        onError: (e) => showApiError(e, t('orgInfra.key_revoke_failed')),
    });

    const copyToClipboard = useCallback((text: string) => {
        navigator.clipboard.writeText(text);
        toast.success(i18n.t('orgInfra.copied'));
    }, []);

    // ── Invites ───────────────────────────────────────────────────────────────
    const [inviteEmail, setInviteEmail] = useState('');
    const [inviteRole, setInviteRole] = useState<'user' | 'admin'>('user');
    const [createdInvite, setCreatedInvite] = useState<{ token: string; email: string } | null>(null);

    const createInviteMutation = useMutation({
        mutationFn: () => invitesApi.create(org.id, { email: inviteEmail, role: inviteRole }),
        onSuccess: (data) => {
            setCreatedInvite({ token: data.token, email: data.email });
            setInviteEmail('');
            showApiSuccess(t('orgInfra.invite_created'), t('orgInfra.invite_created_desc', { email: data.email }));
        },
        onError: (e) => showApiError(e, t('orgInfra.invite_failed')),
    });

    const inviteLink = createdInvite
        ? `${window.location.origin}/accept-invite?token=${createdInvite.token}`
        : null;

    // ── Download installer ────────────────────────────────────────────────────
    const [downloading, setDownloading] = useState(false);
    const [newBoxName, setNewBoxName] = useState('');

    const handleDownload = async () => {
        setDownloading(true);
        try {
            await organizationsApi.downloadEdgeInstaller(org.id, org.name, newBoxName.trim() || undefined);
            setNewBoxName('');
            qc.invalidateQueries({ queryKey: ['edge-agents', org.id] });
            showApiSuccess(t('orgInfra.download_started'), t('orgInfra.download_started_desc'));
        } catch (e) {
            showApiError(e, t('orgInfra.download_failed'));
        } finally {
            setDownloading(false);
        }
    };

    // ── Webhooks ──────────────────────────────────────────────────────────────
    const [whURL, setWhURL] = useState('');
    const [whEvents, setWhEvents] = useState<WebhookEvent[]>([]);
    const [shownWhSecret, setShownWhSecret] = useState<string | null>(null);

    const { data: webhooks = [], refetch: refetchWebhooks } = useQuery<Webhook[]>({
        queryKey: ['webhooks', org.id],
        queryFn: () => webhooksApi.list(org.id),
        enabled: open,
    });

    const createWebhookMutation = useMutation({
        mutationFn: () => webhooksApi.create(org.id, { url: whURL, events: whEvents }),
        onSuccess: (wh) => {
            setShownWhSecret(wh.secret ?? null);
            setWhURL(''); setWhEvents([]);
            refetchWebhooks();
            toast.success(t('orgInfra.wh_created'));
        },
        onError: (e) => showApiError(e, t('orgInfra.wh_create_failed')),
    });

    const deleteWebhookMutation = useMutation({
        mutationFn: (id: number) => webhooksApi.delete(org.id, id),
        onSuccess: () => { refetchWebhooks(); toast.success(t('orgInfra.wh_deleted')); },
        onError: (e) => showApiError(e, t('orgInfra.wh_delete_failed')),
    });

    const toggleWhEvent = (ev: WebhookEvent) => {
        setWhEvents(prev => prev.includes(ev) ? prev.filter(e => e !== ev) : [...prev, ev]);
    };

    // ── SSO Providers ─────────────────────────────────────────────────────────
    const emptySSO: SSOProviderInput = {
        provider: 'google', client_id: '', client_secret: '',
        tenant_id: '', domain_hint: '', enabled: true,
    };
    const [ssoForm, setSSOForm] = useState<SSOProviderInput>(emptySSO);
    const [ssoEditing, setSSOEditing] = useState(false);

    const { data: ssoProviders = [], refetch: refetchSSO } = useQuery<SSOProvider[]>({
        queryKey: ['sso-providers', org.id],
        queryFn: () => organizationsApi.listSSOProviders(org.id),
        enabled: open,
    });

    const upsertSSOMutation = useMutation({
        mutationFn: (data: SSOProviderInput) => organizationsApi.upsertSSOProvider(org.id, data),
        onSuccess: () => {
            refetchSSO();
            setSSOForm(emptySSO);
            setSSOEditing(false);
            toast.success(t('orgInfra.sso_saved'));
        },
        onError: (e) => showApiError(e, t('orgInfra.sso_save_failed')),
    });

    const deleteSSOMutation = useMutation({
        mutationFn: (provider: string) => organizationsApi.deleteSSOProvider(org.id, provider),
        onSuccess: () => { refetchSSO(); toast.success(t('orgInfra.sso_removed')); },
        onError: (e) => showApiError(e, t('orgInfra.sso_remove_failed')),
    });

    const startEditSSO = (p: SSOProvider) => {
        setSSOForm({
            provider: p.provider, client_id: p.client_id, client_secret: '',
            tenant_id: p.tenant_id ?? '', domain_hint: p.domain_hint ?? '', enabled: p.enabled,
        });
        setSSOEditing(true);
    };

    // Reset local state when dialog closes
    useEffect(() => {
        if (!open) {
            setShownKey(null);
            setCreatedInvite(null);
            setInviteEmail('');
            setNewKeyName('');
            setShownWhSecret(null);
            setSSOForm(emptySSO);
            setSSOEditing(false);
        }
    }, [open]);

    const activeKeys = apiKeys.filter((k: ApiKey) => !k.revoked_at);

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2 text-lg">
                        {t('orgInfra.title', { name: org.name })}
                    </DialogTitle>
                    <DialogDescription>
                        {t('orgInfra.subtitle')}
                    </DialogDescription>
                </DialogHeader>

                <Tabs defaultValue="edge" className="mt-2">
                    <TabsList className="grid w-full grid-cols-2 sm:grid-cols-3 lg:grid-cols-6">
                        <TabsTrigger value="edge">{t('orgInfra.tab_edge')}</TabsTrigger>
                        <TabsTrigger value="apikeys">{t('orgInfra.tab_keys')}</TabsTrigger>
                        <TabsTrigger value="invites">{t('orgInfra.tab_invites')}</TabsTrigger>
                        <TabsTrigger value="webhooks">{t('orgInfra.tab_webhooks')}</TabsTrigger>
                        <TabsTrigger value="mqtt">{t('mqttClients.tab')}</TabsTrigger>
                        <TabsTrigger value="sso">SSO</TabsTrigger>
                    </TabsList>

                    {/* ── EDGE TAB ─────────────────────────────────────────── */}
                    <TabsContent value="edge" className="space-y-4 pt-4">
                        <div className="flex items-center justify-between rounded-lg border p-4">
                            <div className="space-y-1">
                                <p className="text-sm font-medium">{t('orgInfra.edge_status')}</p>
                                {edgeStatus?.online ? (
                                    <div className="flex items-center gap-2">
                                        <span className="relative flex h-2.5 w-2.5">
                                            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-400 opacity-75" />
                                            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-green-500" />
                                        </span>
                                        <Badge variant="outline" className="border-green-500 text-green-600">
                                            {t('orgInfra.online')}
                                        </Badge>
                                        {edgeStatus.last_ping && (
                                            <span className="text-xs text-muted-foreground">
                                                {t('orgInfra.last_ping', { when: formatRelativeTime(edgeStatus.last_ping) })}
                                            </span>
                                        )}
                                    </div>
                                ) : (
                                    <div className="flex items-center gap-2">
                                        <WifiOff size={14} className="text-muted-foreground" />
                                        <Badge variant="outline" className="border-muted-foreground text-muted-foreground">
                                            {t('orgInfra.offline')}
                                        </Badge>
                                        <span className="text-xs text-muted-foreground">
                                            {t('orgInfra.no_heartbeat')}
                                        </span>
                                    </div>
                                )}
                            </div>
                            <Button variant="ghost" size="icon" onClick={() => refetchEdge()} disabled={edgeFetching}>
                                <RefreshCw size={15} className={edgeFetching ? 'animate-spin' : ''} />
                            </Button>
                        </div>

                        <div className="rounded-lg border p-4 space-y-3">
                            <div>
                                <p className="text-sm font-medium">{t('orgInfra.who_polls')}</p>
                                <p className="text-xs text-muted-foreground mt-0.5">
                                    {t('orgInfra.who_polls_desc')}
                                </p>
                            </div>
                            <EdgeBoxesPanel orgId={org.id} />
                        </div>

                        <div className="rounded-lg border p-4 space-y-3">
                            <div>
                                <p className="text-sm font-medium">{t('orgInfra.install_box')}</p>
                                <p className="text-xs text-muted-foreground mt-0.5">
                                    {t('orgInfra.install_box_desc')}
                                </p>
                            </div>
                            <div className="flex flex-wrap items-center gap-2">
                                <Input
                                    value={newBoxName}
                                    onChange={(e) => setNewBoxName(e.target.value)}
                                    placeholder={t('orgInfra.box_name_placeholder')}
                                    className="max-w-xs"
                                    aria-label={t('orgInfra.box_name_aria')}
                                />
                                <Button onClick={handleDownload} disabled={downloading} className="gap-2">
                                    <Download size={15} />
                                    {downloading ? t('orgInfra.generating') : t('orgInfra.download')}
                                </Button>
                            </div>
                        </div>
                    </TabsContent>

                    {/* ── API KEYS TAB ─────────────────────────────────────── */}
                    <TabsContent value="apikeys" className="space-y-4 pt-4">
                        {/* New key revealed after creation */}
                        {shownKey && (
                            <div className="rounded-lg border border-amber-500 bg-amber-50 dark:bg-amber-950/20 p-4 space-y-2">
                                <p className="text-sm font-semibold text-amber-700 dark:text-amber-400">
                                    {t('orgInfra.key_save_now')}
                                </p>
                                <div className="flex items-center gap-2">
                                    <code className="flex-1 rounded bg-background px-2 py-1 text-xs font-mono border">
                                        {revealedKey ? shownKey : shownKey.replace(/./g, '•')}
                                    </code>
                                    <Button variant="ghost" size="icon" onClick={() => setRevealedKey(v => !v)}>
                                        {revealedKey ? <EyeOff size={14} /> : <Eye size={14} />}
                                    </Button>
                                    <Button variant="ghost" size="icon" onClick={() => copyToClipboard(shownKey)}>
                                        <Copy size={14} />
                                    </Button>
                                </div>
                            </div>
                        )}

                        {/* Create new key */}
                        <div className="rounded-lg border p-4 space-y-3">
                            <p className="text-sm font-medium">{t('orgInfra.key_create_title')}</p>
                            <div className="flex gap-2">
                                <Input
                                    placeholder={t('orgInfra.key_name_placeholder')}
                                    value={newKeyName}
                                    onChange={(e) => setNewKeyName(e.target.value)}
                                    className="flex-1"
                                />
                                <Button
                                    onClick={() => createKeyMutation.mutate()}
                                    disabled={createKeyMutation.isPending}
                                    className="gap-2 shrink-0"
                                >
                                    <Key size={14} />
                                    {createKeyMutation.isPending ? t('orgInfra.creating') : t('common.create')}
                                </Button>
                            </div>
                        </div>

                        {/* Active keys list */}
                        {activeKeys.length > 0 && (
                            <div className="rounded-md border">
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead>{t('common.name')}</TableHead>
                                            <TableHead>{t('orgInfra.col_prefix')}</TableHead>
                                            <TableHead>{t('common.created_at')}</TableHead>
                                            <TableHead>{t('orgInfra.col_last_used')}</TableHead>
                                            <TableHead className="w-[60px]" />
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {activeKeys.map((k: ApiKey) => (
                                            <TableRow key={k.id}>
                                                <TableCell className="font-medium text-sm">{k.name}</TableCell>
                                                <TableCell>
                                                    <code className="text-xs bg-muted px-1.5 py-0.5 rounded">
                                                        {k.key_prefix}…
                                                    </code>
                                                </TableCell>
                                                <TableCell className="text-xs text-muted-foreground">
                                                    {new Date(k.created_at).toLocaleDateString(i18n.language)}
                                                </TableCell>
                                                <TableCell className="text-xs text-muted-foreground">
                                                    {k.last_used_at
                                                        ? formatRelativeTime(k.last_used_at)
                                                        : t('orgInfra.never')}
                                                </TableCell>
                                                <TableCell>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-9 sm:h-7 w-9 sm:w-7 text-red-500 hover:text-red-600 hover:bg-red-500/10"
                                                        onClick={async () => {
                                                            if (await confirmAction({ title: i18n.t('ask.revoke_key'), description: i18n.t('ask.revoke_key_desc'), confirmLabel: i18n.t('ask.revoke'), destructive: true })) {
                                                                revokeKeyMutation.mutate(k.id);
                                                            }
                                                        }}
                                                    >
                                                        <Trash2 size={13} />
                                                    </Button>
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </div>
                        )}

                        {!keysFetching && activeKeys.length === 0 && !shownKey && (
                            <p className="text-center text-sm text-muted-foreground py-6">
                                {t('orgInfra.keys_empty')}
                            </p>
                        )}
                    </TabsContent>

                    {/* ── INVITES TAB ──────────────────────────────────────── */}
                    <TabsContent value="invites" className="space-y-4 pt-4">
                        {/* Show invite link after creation */}
                        {inviteLink && (
                            <div className="rounded-lg border border-blue-500 bg-blue-50 dark:bg-blue-950/20 p-4 space-y-2">
                                <p className="text-sm font-semibold text-blue-700 dark:text-blue-400">
                                    {t('orgInfra.invite_link_for', { email: createdInvite?.email })}
                                </p>
                                <div className="flex items-center gap-2">
                                    <code className="flex-1 rounded bg-background px-2 py-1 text-xs font-mono border break-all">
                                        {inviteLink}
                                    </code>
                                    <Button variant="ghost" size="icon" onClick={() => copyToClipboard(inviteLink)}>
                                        <Copy size={14} />
                                    </Button>
                                </div>
                                <p className="text-xs text-muted-foreground">
                                    {t('orgInfra.invite_link_hint')}
                                </p>
                            </div>
                        )}

                        {/* Create invite form */}
                        <div className="rounded-lg border p-4 space-y-3">
                            <p className="text-sm font-medium">{t('orgInfra.invite_title')}</p>
                            <div className="grid gap-3">
                                <div className="space-y-1.5">
                                    <Label htmlFor="invite-email" className="text-xs">{t('orgInfra.email')}</Label>
                                    <Input
                                        id="invite-email"
                                        type="email"
                                        placeholder={t('orgInfra.email_placeholder')}
                                        value={inviteEmail}
                                        onChange={(e) => setInviteEmail(e.target.value)}
                                    />
                                </div>
                                <div className="space-y-1.5">
                                    <Label htmlFor="invite-role" className="text-xs">{t('orgInfra.role')}</Label>
                                    <Select value={inviteRole} onValueChange={(v) => setInviteRole(v as 'user' | 'admin')}>
                                        <SelectTrigger id="invite-role">
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="user">{t('orgInfra.role_user')}</SelectItem>
                                            <SelectItem value="admin">{t('orgInfra.role_admin')}</SelectItem>
                                        </SelectContent>
                                    </Select>
                                </div>
                                <Button
                                    onClick={() => createInviteMutation.mutate()}
                                    disabled={createInviteMutation.isPending || !inviteEmail}
                                    className="gap-2"
                                >
                                    <UserPlus size={14} />
                                    {createInviteMutation.isPending ? t('orgInfra.creating') : t('orgInfra.invite_generate')}
                                </Button>
                            </div>
                        </div>

                        <p className="text-xs text-muted-foreground px-1">
                            {t('orgInfra.invite_footer')}
                        </p>
                    </TabsContent>

                    {/* ── WEBHOOKS TAB ─────────────────────────────────────── */}
                    <TabsContent value="webhooks" className="space-y-4 pt-4">
                        {shownWhSecret && (
                            <div className="rounded-lg border border-amber-500 bg-amber-50 dark:bg-amber-950/20 p-4 space-y-2">
                                <p className="text-sm font-semibold text-amber-700 dark:text-amber-400 flex items-center gap-2">
                                    <WebhookIcon size={14} /> {t('orgInfra.wh_secret_title')}
                                </p>
                                <div className="flex items-center gap-2">
                                    <code className="flex-1 rounded bg-background px-2 py-1 text-xs font-mono border break-all">
                                        {shownWhSecret}
                                    </code>
                                    <Button variant="ghost" size="icon" onClick={() => {
                                        navigator.clipboard.writeText(shownWhSecret);
                                        toast.success(t('orgInfra.wh_secret_copied'));
                                    }}>
                                        <Copy size={14} />
                                    </Button>
                                </div>
                                <p className="text-xs text-muted-foreground">
                                    {t('orgInfra.wh_secret_hint_before')}<code>X-OpenEdge-Signature</code>{t('orgInfra.wh_secret_hint_after')}
                                </p>
                            </div>
                        )}

                        {/* Create webhook */}
                        <div className="rounded-lg border p-4 space-y-3">
                            <p className="text-sm font-medium">{t('orgInfra.wh_add_title')}</p>
                            <div className="space-y-1.5">
                                <Label className="text-xs">URL</Label>
                                <Input
                                    type="url"
                                    placeholder={t('orgInfra.wh_url_placeholder')}
                                    value={whURL}
                                    onChange={e => setWhURL(e.target.value)}
                                />
                            </div>
                            <div className="space-y-1.5">
                                <Label className="text-xs">{t('orgInfra.wh_events')}</Label>
                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5">
                                    {WEBHOOK_EVENTS.map(ev => (
                                        <label key={ev.value} className="flex items-center gap-2 text-sm cursor-pointer">
                                            <input
                                                type="checkbox"
                                                checked={whEvents.includes(ev.value)}
                                                onChange={() => toggleWhEvent(ev.value)}
                                                className="rounded"
                                            />
                                            {t(`orgInfra.wh_event.${ev.value.replace('.', '_')}`, { defaultValue: ev.label })}
                                        </label>
                                    ))}
                                </div>
                            </div>
                            <Button
                                onClick={() => createWebhookMutation.mutate()}
                                disabled={createWebhookMutation.isPending || !whURL || whEvents.length === 0}
                                size="sm"
                                className="gap-2"
                            >
                                <WebhookIcon size={14} />
                                {createWebhookMutation.isPending ? t('orgInfra.creating') : t('orgInfra.wh_add')}
                            </Button>
                        </div>

                        {/* List webhooks */}
                        {webhooks.length > 0 && (
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>URL</TableHead>
                                        <TableHead>{t('orgInfra.col_events')}</TableHead>
                                        <TableHead>{t('orgInfra.col_last_call')}</TableHead>
                                        <TableHead className="w-[50px]" />
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {webhooks.map(wh => (
                                        <TableRow key={wh.id}>
                                            <TableCell className="font-mono text-xs max-w-[200px] truncate" title={wh.url}>
                                                {wh.url}
                                            </TableCell>
                                            <TableCell>
                                                <div className="flex flex-wrap gap-1">
                                                    {wh.events.map(ev => (
                                                        <Badge key={ev} variant="secondary" className="text-[10px] px-1 py-0">
                                                            {ev}
                                                        </Badge>
                                                    ))}
                                                </div>
                                            </TableCell>
                                            <TableCell className="text-xs text-muted-foreground">
                                                {wh.last_triggered_at ? (
                                                    <span className={wh.last_status_code && wh.last_status_code >= 200 && wh.last_status_code < 300
                                                        ? 'text-green-600' : 'text-red-500'}>
                                                        {wh.last_status_code} · {formatRelativeTime(wh.last_triggered_at)}
                                                    </span>
                                                ) : '—'}
                                            </TableCell>
                                            <TableCell>
                                                <Button variant="ghost" size="icon" className="h-9 sm:h-7 w-9 sm:w-7 text-destructive hover:text-destructive"
                                                    aria-label={i18n.t('common.delete')}
                                                    onClick={async () => {
                                                        if (await confirmAction({ title: i18n.t('ask.delete_webhook'), description: wh.url, destructive: true })) {
                                                            deleteWebhookMutation.mutate(wh.id);
                                                        }
                                                    }}>
                                                    <Trash2 size={13} />
                                                </Button>
                                            </TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        )}
                        {webhooks.length === 0 && (
                            <p className="text-center text-sm text-muted-foreground py-6">
                                {t('orgInfra.wh_empty')}
                            </p>
                        )}
                    </TabsContent>
                    {/* ── SSO TAB ──────────────────────────────────────────── */}
                    <TabsContent value="mqtt" className="space-y-4 pt-4">
                        <MqttClientsPanel orgId={org.id} />
                    </TabsContent>

                    <TabsContent value="sso" className="space-y-4 pt-4">
                        <p className="text-sm text-muted-foreground">
                            {t('orgInfra.sso_intro')}
                        </p>

                        {/* Provider list */}
                        {ssoProviders.length > 0 && (
                            <div className="rounded-md border">
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead>Provider</TableHead>
                                            <TableHead>Client ID</TableHead>
                                            <TableHead>{t('orgInfra.col_domain_hint')}</TableHead>
                                            <TableHead>{t('orgInfra.col_enabled')}</TableHead>
                                            <TableHead className="w-[80px]" />
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {ssoProviders.map(p => (
                                            <TableRow key={p.provider}>
                                                <TableCell className="font-medium text-sm capitalize">{p.provider}</TableCell>
                                                <TableCell>
                                                    <code className="text-xs bg-muted px-1.5 py-0.5 rounded">
                                                        {p.client_id.length > 20 ? p.client_id.slice(0, 20) + '…' : p.client_id}
                                                    </code>
                                                </TableCell>
                                                <TableCell className="text-xs text-muted-foreground">
                                                    {p.domain_hint || '—'}
                                                </TableCell>
                                                <TableCell>
                                                    <Badge variant={p.enabled ? 'default' : 'secondary'} className="text-[10px]">
                                                        {p.enabled ? t('orgInfra.sso_active') : t('orgInfra.sso_disabled')}
                                                    </Badge>
                                                </TableCell>
                                                <TableCell>
                                                    <div className="flex gap-1">
                                                        <Button variant="ghost" size="icon" className="h-9 sm:h-7 w-9 sm:w-7"
                                                            onClick={() => startEditSSO(p)}>
                                                            <Key size={13} />
                                                        </Button>
                                                        <Button variant="ghost" size="icon"
                                                            className="h-9 sm:h-7 w-9 sm:w-7 text-destructive hover:text-destructive"
                                                            onClick={async () => {
                                                                if (await confirmAction({ title: i18n.t('ask.remove_sso', { provider: p.provider }), description: i18n.t('ask.remove_sso_desc'), confirmLabel: i18n.t('ask.remove'), destructive: true })) {
                                                                    deleteSSOMutation.mutate(p.provider);
                                                                }
                                                            }}>
                                                            <Trash2 size={13} />
                                                        </Button>
                                                    </div>
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </div>
                        )}

                        {/* Add / Edit form */}
                        {ssoEditing || ssoProviders.length === 0 ? (
                            <div className="rounded-lg border p-4 space-y-3">
                                <p className="text-sm font-medium flex items-center gap-2">
                                    <Shield size={14} />
                                    {ssoEditing ? t('orgInfra.sso_edit_title') : t('orgInfra.sso_add_title')}
                                </p>

                                <div className="grid gap-3">
                                    <div className="space-y-1.5">
                                        <Label className="text-xs">Provider</Label>
                                        <Select
                                            value={ssoForm.provider}
                                            onValueChange={v => setSSOForm(f => ({ ...f, provider: v as 'google' | 'azure' }))}
                                        >
                                            <SelectTrigger>
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="google">Google Workspace</SelectItem>
                                                <SelectItem value="azure">Microsoft Azure AD</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>

                                    <div className="space-y-1.5">
                                        <Label className="text-xs">Client ID</Label>
                                        <Input
                                            placeholder="OAuth 2.0 Client ID"
                                            value={ssoForm.client_id}
                                            onChange={e => setSSOForm(f => ({ ...f, client_id: e.target.value }))}
                                        />
                                    </div>

                                    <div className="space-y-1.5">
                                        <Label className="text-xs">
                                            Client Secret {ssoEditing && <span className="text-muted-foreground">{t('orgInfra.sso_secret_keep')}</span>}
                                        </Label>
                                        <Input
                                            type="password"
                                            placeholder={ssoEditing ? '••••••••' : 'OAuth 2.0 Client Secret'}
                                            value={ssoForm.client_secret}
                                            onChange={e => setSSOForm(f => ({ ...f, client_secret: e.target.value }))}
                                        />
                                    </div>

                                    {ssoForm.provider === 'azure' && (
                                        <div className="space-y-1.5">
                                            <Label className="text-xs">Tenant ID (Azure)</Label>
                                            <Input
                                                placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
                                                value={ssoForm.tenant_id}
                                                onChange={e => setSSOForm(f => ({ ...f, tenant_id: e.target.value }))}
                                            />
                                        </div>
                                    )}

                                    <div className="space-y-1.5">
                                        <Label className="text-xs">{t('orgInfra.sso_domain_hint')}</Label>
                                        <Input
                                            placeholder={t('orgInfra.sso_domain_placeholder')}
                                            value={ssoForm.domain_hint}
                                            onChange={e => setSSOForm(f => ({ ...f, domain_hint: e.target.value }))}
                                        />
                                    </div>

                                    <div className="flex items-center gap-2">
                                        <Switch
                                            id="sso-enabled"
                                            checked={ssoForm.enabled}
                                            onCheckedChange={v => setSSOForm(f => ({ ...f, enabled: v }))}
                                        />
                                        <Label htmlFor="sso-enabled" className="text-xs cursor-pointer">
                                            {t('orgInfra.sso_enable')}
                                        </Label>
                                    </div>

                                    <div className="flex gap-2 pt-1">
                                        <Button
                                            onClick={() => upsertSSOMutation.mutate(ssoForm)}
                                            disabled={upsertSSOMutation.isPending || !ssoForm.client_id || (!ssoEditing && !ssoForm.client_secret)}
                                            size="sm"
                                            className="gap-2"
                                        >
                                            <Shield size={13} />
                                            {upsertSSOMutation.isPending ? t('orgInfra.saving') : t('orgInfra.sso_save')}
                                        </Button>
                                        {ssoEditing && (
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                onClick={() => { setSSOEditing(false); setSSOForm(emptySSO); }}
                                            >
                                                {t('common.cancel')}
                                            </Button>
                                        )}
                                    </div>
                                </div>
                            </div>
                        ) : (
                            <Button variant="outline" size="sm" className="gap-2"
                                onClick={() => setSSOEditing(true)}>
                                <Plus size={14} /> {t('orgInfra.sso_add_another')}
                            </Button>
                        )}
                    </TabsContent>
                </Tabs>
            </DialogContent>
        </Dialog>
    );
}

function formatRelativeTime(isoString: string): string {
    const diff = Math.floor((Date.now() - new Date(isoString).getTime()) / 1000);
    if (diff < 60) return i18n.t('orgInfra.ago_s', { n: diff });
    if (diff < 3600) return i18n.t('orgInfra.ago_m', { n: Math.floor(diff / 60) });
    if (diff < 86400) return i18n.t('orgInfra.ago_h', { n: Math.floor(diff / 3600) });
    return i18n.t('orgInfra.ago_d', { n: Math.floor(diff / 86400) });
}
