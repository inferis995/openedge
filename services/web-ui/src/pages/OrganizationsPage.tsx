import { useState } from 'react';
import { useOrganizations } from '@/hooks/useOrganizations';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useAuthStore } from '@/stores/useAuthStore';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogFooter,
    DialogTrigger,
} from '@/components/ui/dialog';
import {
    Building2, Plus, Trash2, ChevronRight, Server, Pencil, WifiOff,
} from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { organizationsApi } from '@/api/organizations';
import OrgInfrastructureDialog from '@/components/OrgInfrastructureDialog';
import { Organization } from '@/types';

// Small hook: fetch edge status for a single org (used inline in the row)
function useEdgeStatus(orgId: number, enabled: boolean) {
    return useQuery({
        queryKey: ['edge-status', orgId],
        queryFn: () => organizationsApi.getEdgeStatus(orgId),
        enabled,
        refetchInterval: enabled ? 30_000 : false,
        staleTime: 20_000,
    });
}

function EdgeBadge({ orgId }: { orgId: number }) {
    const { data } = useEdgeStatus(orgId, true);
    if (!data) return null;
    return data.online ? (
        <Badge variant="outline" className="gap-1 border-green-500 text-green-600 text-[10px] h-5 px-1.5">
            <span className="relative flex h-1.5 w-1.5">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-400 opacity-75" />
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-green-500" />
            </span>
            Edge online
        </Badge>
    ) : (
        <Badge variant="outline" className="gap-1 border-muted-foreground/40 text-muted-foreground text-[10px] h-5 px-1.5">
            <WifiOff size={9} />
            Edge offline
        </Badge>
    );
}

const OrganizationsPage = () => {
    const navigate = useNavigate();
    const { organizations, isLoading, create, update, remove } = useOrganizations();
    const { setSelectedOrgId } = useNavigationStore();
    const { isAdmin } = useAuthStore();

    // Create dialog
    const [isOpen, setIsOpen] = useState(false);
    const [newOrgName, setNewOrgName] = useState('');

    // Edit dialog
    const [isEditOpen, setIsEditOpen] = useState(false);
    const [editingOrg, setEditingOrg] = useState<{ id: number; name: string } | null>(null);

    // Infrastructure dialog
    const [infraOrg, setInfraOrg] = useState<Organization | null>(null);

    const handleCreate = async () => {
        if (!newOrgName.trim()) return;
        await create({ name: newOrgName.trim() });
        setIsOpen(false);
        setNewOrgName('');
    };

    const handleUpdate = async () => {
        if (!editingOrg) return;
        await update({ id: editingOrg.id, data: { name: editingOrg.name } });
        setIsEditOpen(false);
        setEditingOrg(null);
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (confirm('Delete this organization? This action cannot be undone.')) {
            await remove(id);
        }
    };

    const handleSelect = (id: number) => {
        setSelectedOrgId(id);
        navigate('/sites');
    };

    if (isLoading) {
        return <div className="p-8 text-center text-muted-foreground">Loading organizations…</div>;
    }

    return (
        <div className="space-y-6">
            {/* Tenant stats banner */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                <div className="rounded-lg border bg-card p-4 text-center">
                    <div className="text-3xl font-bold text-primary">{organizations.length}</div>
                    <div className="text-sm text-muted-foreground mt-1">Tenant totali</div>
                </div>
                <div className="rounded-lg border bg-card p-4 text-center">
                    <div className="text-3xl font-bold text-green-600">{organizations.length}</div>
                    <div className="text-sm text-muted-foreground mt-1">Attivi</div>
                </div>
                <div className="rounded-lg border bg-card p-4 text-center flex flex-col items-center justify-center">
                    <a
                        href="/fleet"
                        className="text-sm font-medium text-primary hover:underline"
                    >
                        Vedi Fleet →
                    </a>
                    <div className="text-xs text-muted-foreground mt-1">Stato edge per org</div>
                </div>
            </div>

            {/* Header */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Organizations</h2>
                    <p className="text-muted-foreground text-sm">
                        Manage tenants, deploy edge packages, and invite team members.
                    </p>
                </div>
                {isAdmin() && (
                    <Dialog open={isOpen} onOpenChange={setIsOpen}>
                        <DialogTrigger asChild>
                            <Button className="gap-2">
                                <Plus size={16} /> New Organization
                            </Button>
                        </DialogTrigger>
                        <DialogContent>
                            <DialogHeader>
                                <DialogTitle>Create Organization</DialogTitle>
                                <DialogDescription>
                                    Each organization is an isolated tenant with its own edge, users, and data.
                                </DialogDescription>
                            </DialogHeader>
                            <div className="grid gap-4 py-4">
                                <div className="space-y-1.5">
                                    <Label htmlFor="name">Organization name</Label>
                                    <Input
                                        id="name"
                                        value={newOrgName}
                                        onChange={(e) => setNewOrgName(e.target.value)}
                                        placeholder="e.g. Acme Industries"
                                        onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
                                        autoFocus
                                    />
                                </div>
                            </div>
                            <DialogFooter>
                                <Button variant="outline" onClick={() => setIsOpen(false)}>Cancel</Button>
                                <Button onClick={handleCreate} disabled={!newOrgName.trim()}>Create</Button>
                            </DialogFooter>
                        </DialogContent>
                    </Dialog>
                )}
            </div>

            {/* Org cards */}
            {organizations.length === 0 ? (
                <div className="rounded-lg border border-dashed p-12 text-center space-y-3">
                    <Building2 size={40} className="mx-auto text-muted-foreground/40" />
                    <p className="text-muted-foreground">No organizations yet.</p>
                    {isAdmin() && (
                        <Button variant="outline" onClick={() => setIsOpen(true)} className="gap-2">
                            <Plus size={14} /> Create your first organization
                        </Button>
                    )}
                </div>
            ) : (
                <div className="space-y-3">
                    {organizations.map((org) => (
                        <div
                            key={org.id}
                            className="group rounded-lg border bg-card hover:border-primary/40 transition-colors"
                        >
                            {/* Main row */}
                            <div className="flex items-center gap-4 p-4">
                                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10">
                                    <Building2 size={20} className="text-primary" />
                                </div>

                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2 flex-wrap">
                                        <span className="font-semibold text-base">{org.name}</span>
                                        <EdgeBadge orgId={org.id} />
                                    </div>
                                    <p className="text-xs text-muted-foreground mt-0.5">
                                        Created {new Date(org.created_at).toLocaleDateString('en-US', {
                                            year: 'numeric', month: 'long', day: 'numeric',
                                        })}
                                        {' · '}ID #{org.id}
                                    </p>
                                </div>

                                {/* Actions */}
                                <div className="flex items-center gap-1.5 shrink-0">
                                    {isAdmin() && (
                                        <>
                                            {/* Infrastructure (edge, keys, invites) */}
                                            <Button
                                                variant="outline"
                                                size="sm"
                                                className="gap-1.5 text-xs h-10 sm:h-8"
                                                onClick={(e) => { e.stopPropagation(); setInfraOrg(org); }}
                                            >
                                                <Server size={13} />
                                                Infrastructure
                                            </Button>

                                            {/* Edit */}
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-10 sm:h-8 w-10 sm:w-8 text-muted-foreground hover:text-foreground"
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    setEditingOrg({ id: org.id, name: org.name });
                                                    setIsEditOpen(true);
                                                }}
                                            >
                                                <Pencil size={14} />
                                            </Button>

                                            {/* Delete */}
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-10 sm:h-8 w-10 sm:w-8 text-red-500 hover:text-red-600 hover:bg-red-500/10"
                                                onClick={(e) => handleDelete(e, org.id)}
                                            >
                                                <Trash2 size={14} />
                                            </Button>
                                        </>
                                    )}

                                    {/* Enter org */}
                                    <Button
                                        variant="default"
                                        size="sm"
                                        className="gap-1.5 text-xs h-10 sm:h-8"
                                        onClick={() => handleSelect(org.id)}
                                    >
                                        Enter
                                        <ChevronRight size={13} />
                                    </Button>
                                </div>
                            </div>
                        </div>
                    ))}
                </div>
            )}

            {/* Edit dialog */}
            <Dialog open={isEditOpen} onOpenChange={setIsEditOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Rename Organization</DialogTitle>
                    </DialogHeader>
                    <div className="grid gap-4 py-4">
                        <div className="space-y-1.5">
                            <Label htmlFor="edit-name">Name</Label>
                            <Input
                                id="edit-name"
                                value={editingOrg?.name || ''}
                                onChange={(e) =>
                                    setEditingOrg((prev) => prev ? { ...prev, name: e.target.value } : null)
                                }
                                onKeyDown={(e) => e.key === 'Enter' && handleUpdate()}
                                autoFocus
                            />
                        </div>
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setIsEditOpen(false)}>Cancel</Button>
                        <Button onClick={handleUpdate}>Save</Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Infrastructure dialog */}
            {infraOrg && (
                <OrgInfrastructureDialog
                    org={infraOrg}
                    open={!!infraOrg}
                    onOpenChange={(open) => { if (!open) setInfraOrg(null); }}
                />
            )}
        </div>
    );
};

export default OrganizationsPage;
