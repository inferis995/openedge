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
import {
    Download, Key, UserPlus, WifiOff, Copy, Trash2, RefreshCw, Eye, EyeOff,
} from 'lucide-react';
import { organizationsApi } from '@/api/organizations';
import { apiKeysApi, ApiKey } from '@/api/apiKeys';
import { invitesApi } from '@/api/invites';
import { showApiSuccess, showApiError } from '@/lib/api-error-handler';
import { toast } from 'sonner';

interface Props {
    org: { id: number; name: string };
    open: boolean;
    onOpenChange: (open: boolean) => void;
}

export default function OrgInfrastructureDialog({ org, open, onOpenChange }: Props) {
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
        onError: (e) => showApiError(e, 'Failed to create API key'),
    });

    const revokeKeyMutation = useMutation({
        mutationFn: (keyId: number) => apiKeysApi.revoke(org.id, keyId),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['api-keys', org.id] });
            showApiSuccess('API key revoked');
        },
        onError: (e) => showApiError(e, 'Failed to revoke key'),
    });

    const copyToClipboard = useCallback((text: string) => {
        navigator.clipboard.writeText(text);
        toast.success('Copied to clipboard');
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
            showApiSuccess('Invite created', `Invite link generated for ${data.email}`);
        },
        onError: (e) => showApiError(e, 'Failed to create invite'),
    });

    const inviteLink = createdInvite
        ? `${window.location.origin}/accept-invite?token=${createdInvite.token}`
        : null;

    // ── Download installer ────────────────────────────────────────────────────
    const [downloading, setDownloading] = useState(false);

    const handleDownload = async () => {
        setDownloading(true);
        try {
            await organizationsApi.downloadEdgeInstaller(org.id, org.name);
            showApiSuccess('Download started', 'Edge deployment package is downloading');
        } catch (e) {
            showApiError(e, 'Download failed');
        } finally {
            setDownloading(false);
        }
    };

    // Reset local state when dialog closes
    useEffect(() => {
        if (!open) {
            setShownKey(null);
            setCreatedInvite(null);
            setInviteEmail('');
            setNewKeyName('');
        }
    }, [open]);

    const activeKeys = apiKeys.filter((k: ApiKey) => !k.revoked_at);

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2 text-lg">
                        Infrastructure — {org.name}
                    </DialogTitle>
                    <DialogDescription>
                        Manage edge deployment, API keys, and team invites for this organization.
                    </DialogDescription>
                </DialogHeader>

                <Tabs defaultValue="edge" className="mt-2">
                    <TabsList className="grid w-full grid-cols-3">
                        <TabsTrigger value="edge">Edge</TabsTrigger>
                        <TabsTrigger value="apikeys">API Keys</TabsTrigger>
                        <TabsTrigger value="invites">Invite Users</TabsTrigger>
                    </TabsList>

                    {/* ── EDGE TAB ─────────────────────────────────────────── */}
                    <TabsContent value="edge" className="space-y-4 pt-4">
                        <div className="flex items-center justify-between rounded-lg border p-4">
                            <div className="space-y-1">
                                <p className="text-sm font-medium">Edge Manager Status</p>
                                {edgeStatus?.online ? (
                                    <div className="flex items-center gap-2">
                                        <span className="relative flex h-2.5 w-2.5">
                                            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-400 opacity-75" />
                                            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-green-500" />
                                        </span>
                                        <Badge variant="outline" className="border-green-500 text-green-600">
                                            Online
                                        </Badge>
                                        {edgeStatus.last_ping && (
                                            <span className="text-xs text-muted-foreground">
                                                last ping {formatRelativeTime(edgeStatus.last_ping)}
                                            </span>
                                        )}
                                    </div>
                                ) : (
                                    <div className="flex items-center gap-2">
                                        <WifiOff size={14} className="text-muted-foreground" />
                                        <Badge variant="outline" className="border-muted-foreground text-muted-foreground">
                                            Offline
                                        </Badge>
                                        <span className="text-xs text-muted-foreground">
                                            No heartbeat received
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
                                <p className="text-sm font-medium">Edge Deployment Package</p>
                                <p className="text-xs text-muted-foreground mt-0.5">
                                    Download a ready-to-run ZIP containing docker-compose, .env with pre-filled credentials, and install scripts for Linux and Windows.
                                </p>
                            </div>
                            <Button onClick={handleDownload} disabled={downloading} className="gap-2">
                                <Download size={15} />
                                {downloading ? 'Generating…' : 'Download Edge Package'}
                            </Button>
                        </div>
                    </TabsContent>

                    {/* ── API KEYS TAB ─────────────────────────────────────── */}
                    <TabsContent value="apikeys" className="space-y-4 pt-4">
                        {/* New key revealed after creation */}
                        {shownKey && (
                            <div className="rounded-lg border border-amber-500 bg-amber-50 dark:bg-amber-950/20 p-4 space-y-2">
                                <p className="text-sm font-semibold text-amber-700 dark:text-amber-400">
                                    Save this key — it will not be shown again
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
                            <p className="text-sm font-medium">Create API Key</p>
                            <div className="flex gap-2">
                                <Input
                                    placeholder="Key name (e.g. edge-manager)"
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
                                    {createKeyMutation.isPending ? 'Creating…' : 'Create'}
                                </Button>
                            </div>
                        </div>

                        {/* Active keys list */}
                        {activeKeys.length > 0 && (
                            <div className="rounded-md border">
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead>Name</TableHead>
                                            <TableHead>Prefix</TableHead>
                                            <TableHead>Created</TableHead>
                                            <TableHead>Last used</TableHead>
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
                                                    {new Date(k.created_at).toLocaleDateString()}
                                                </TableCell>
                                                <TableCell className="text-xs text-muted-foreground">
                                                    {k.last_used_at
                                                        ? formatRelativeTime(k.last_used_at)
                                                        : 'Never'}
                                                </TableCell>
                                                <TableCell>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-7 w-7 text-red-500 hover:text-red-600 hover:bg-red-500/10"
                                                        onClick={() => {
                                                            if (confirm('Revoke this API key? Edge managers using it will lose access.')) {
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
                                No active API keys. Create one to enable edge manager authentication.
                            </p>
                        )}
                    </TabsContent>

                    {/* ── INVITES TAB ──────────────────────────────────────── */}
                    <TabsContent value="invites" className="space-y-4 pt-4">
                        {/* Show invite link after creation */}
                        {inviteLink && (
                            <div className="rounded-lg border border-blue-500 bg-blue-50 dark:bg-blue-950/20 p-4 space-y-2">
                                <p className="text-sm font-semibold text-blue-700 dark:text-blue-400">
                                    Invite link for {createdInvite?.email}
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
                                    Link expires in 7 days. Share it with the recipient — they'll set their own password.
                                </p>
                            </div>
                        )}

                        {/* Create invite form */}
                        <div className="rounded-lg border p-4 space-y-3">
                            <p className="text-sm font-medium">Invite a new team member</p>
                            <div className="grid gap-3">
                                <div className="space-y-1.5">
                                    <Label htmlFor="invite-email" className="text-xs">Email address</Label>
                                    <Input
                                        id="invite-email"
                                        type="email"
                                        placeholder="colleague@company.com"
                                        value={inviteEmail}
                                        onChange={(e) => setInviteEmail(e.target.value)}
                                    />
                                </div>
                                <div className="space-y-1.5">
                                    <Label htmlFor="invite-role" className="text-xs">Role</Label>
                                    <Select value={inviteRole} onValueChange={(v) => setInviteRole(v as 'user' | 'admin')}>
                                        <SelectTrigger id="invite-role">
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="user">User — can view and operate</SelectItem>
                                            <SelectItem value="admin">Admin — can configure everything</SelectItem>
                                        </SelectContent>
                                    </Select>
                                </div>
                                <Button
                                    onClick={() => createInviteMutation.mutate()}
                                    disabled={createInviteMutation.isPending || !inviteEmail}
                                    className="gap-2"
                                >
                                    <UserPlus size={14} />
                                    {createInviteMutation.isPending ? 'Creating…' : 'Generate Invite Link'}
                                </Button>
                            </div>
                        </div>

                        <p className="text-xs text-muted-foreground px-1">
                            The invite link lets the recipient create their own account in this organization.
                            Each link is single-use and expires after 7 days.
                        </p>
                    </TabsContent>
                </Tabs>
            </DialogContent>
        </Dialog>
    );
}

function formatRelativeTime(isoString: string): string {
    const diff = Math.floor((Date.now() - new Date(isoString).getTime()) / 1000);
    if (diff < 60) return `${diff}s ago`;
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    return `${Math.floor(diff / 86400)}d ago`;
}
