import { useState, useEffect } from 'react';
import { usersApi, User, CreateUserRequest, UpdateUserRequest } from '@/api/users';
import { sitesApi } from '@/api/sites';
import { areasApi } from '@/api/areas';
import { useAuthStore } from '@/stores/useAuthStore';
import { useOrganizations } from '@/hooks/useOrganizations';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Checkbox } from '@/components/ui/checkbox';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
    DialogTrigger,
} from '@/components/ui/dialog';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Users, Plus, Trash2, Pencil, Shield, User as UserIcon, Building2, Network, MapPin, Layers } from 'lucide-react';
import { Switch } from '@/components/ui/switch';
import { Site, Area } from '@/types';

// ---------- scope selector sub-component ----------

interface ScopeSelectorProps {
    orgId: number | null;
    selectedSiteIds: number[];
    selectedAreaIds: number[];
    onSiteIdsChange: (ids: number[]) => void;
    onAreaIdsChange: (ids: number[]) => void;
}

const ScopeSelector = ({
    orgId,
    selectedSiteIds,
    selectedAreaIds,
    onSiteIdsChange,
    onAreaIdsChange,
}: ScopeSelectorProps) => {
    const [sites, setSites] = useState<Site[]>([]);
    const [areas, setAreas] = useState<Area[]>([]);
    const [loadingSites, setLoadingSites] = useState(false);
    const [loadingAreas, setLoadingAreas] = useState(false);

    // Load sites when org changes
    useEffect(() => {
        if (!orgId) {
            setSites([]);
            setAreas([]);
            onSiteIdsChange([]);
            onAreaIdsChange([]);
            return;
        }
        setLoadingSites(true);
        sitesApi.getByOrg(orgId)
            .then(setSites)
            .catch(() => setSites([]))
            .finally(() => setLoadingSites(false));
        // reset area selection when org changes
        setAreas([]);
        onSiteIdsChange([]);
        onAreaIdsChange([]);
    }, [orgId]);

    // Load areas for all selected sites
    useEffect(() => {
        if (selectedSiteIds.length === 0) {
            setAreas([]);
            onAreaIdsChange([]);
            return;
        }
        setLoadingAreas(true);
        Promise.all(selectedSiteIds.map((sid) => areasApi.getAll(sid)))
            .then((results) => setAreas(results.flat()))
            .catch(() => setAreas([]))
            .finally(() => setLoadingAreas(false));
        // drop area selections that no longer belong to selected sites
        onAreaIdsChange([]);
    }, [selectedSiteIds.join(',')]);

    const toggleSite = (siteId: number, checked: boolean) => {
        onSiteIdsChange(checked ? [...selectedSiteIds, siteId] : selectedSiteIds.filter((id) => id !== siteId));
    };

    const toggleArea = (areaId: number, checked: boolean) => {
        onAreaIdsChange(checked ? [...selectedAreaIds, areaId] : selectedAreaIds.filter((id) => id !== areaId));
    };

    if (!orgId) return null;

    const allSites = selectedSiteIds.length === 0;
    const allAreas = selectedAreaIds.length === 0;

    return (
        <div className="col-span-4 space-y-3 border border-border rounded-md p-3 bg-muted/30">
            {/* Site scope */}
            <div>
                <div className="flex items-center gap-2 mb-2">
                    <MapPin size={13} className="text-muted-foreground" />
                    <span className="text-sm font-medium">Scope siti</span>
                    {allSites && (
                        <span className="text-xs text-muted-foreground ml-auto">Tutti i siti dell'org</span>
                    )}
                </div>
                {loadingSites ? (
                    <p className="text-xs text-muted-foreground">Caricamento siti...</p>
                ) : sites.length === 0 ? (
                    <p className="text-xs text-muted-foreground">Nessun sito disponibile</p>
                ) : (
                    <div className="flex flex-wrap gap-x-4 gap-y-1">
                        {sites.map((site) => (
                            <label key={site.id} className="flex items-center gap-1.5 text-sm cursor-pointer">
                                <Checkbox
                                    checked={selectedSiteIds.includes(site.id)}
                                    onCheckedChange={(v) => toggleSite(site.id, !!v)}
                                />
                                {site.name}
                            </label>
                        ))}
                    </div>
                )}
            </div>

            {/* Area scope — visible only when at least one site is selected */}
            {selectedSiteIds.length > 0 && (
                <div className="border-t border-border pt-3">
                    <div className="flex items-center gap-2 mb-2">
                        <Layers size={13} className="text-muted-foreground" />
                        <span className="text-sm font-medium">Scope aree</span>
                        {allAreas && (
                            <span className="text-xs text-muted-foreground ml-auto">Tutte le aree dei siti selezionati</span>
                        )}
                    </div>
                    {loadingAreas ? (
                        <p className="text-xs text-muted-foreground">Caricamento aree...</p>
                    ) : areas.length === 0 ? (
                        <p className="text-xs text-muted-foreground">Nessuna area disponibile</p>
                    ) : (
                        <div className="flex flex-wrap gap-x-4 gap-y-1">
                            {areas.map((area) => (
                                <label key={area.id} className="flex items-center gap-1.5 text-sm cursor-pointer">
                                    <Checkbox
                                        checked={selectedAreaIds.includes(area.id)}
                                        onCheckedChange={(v) => toggleArea(area.id, !!v)}
                                    />
                                    {area.name}
                                </label>
                            ))}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
};

// ---------- main page ----------

const UsersPage = () => {
    const [users, setUsers] = useState<User[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isCreateOpen, setIsCreateOpen] = useState(false);
    const [isEditOpen, setIsEditOpen] = useState(false);
    const [selectedUser, setSelectedUser] = useState<User | null>(null);
    const [error, setError] = useState<string | null>(null);

    const { organizations, isLoading: orgsLoading } = useOrganizations();

    // Form state — create
    const [newUsername, setNewUsername] = useState('');
    const [newPassword, setNewPassword] = useState('');
    const [newRole, setNewRole] = useState<'admin' | 'user'>('user');
    const [newFullName, setNewFullName] = useState('');
    const [newOrgId, setNewOrgId] = useState<number | null>(null);
    const [newI3xWrite, setNewI3xWrite] = useState(false);
    const [newSiteIds, setNewSiteIds] = useState<number[]>([]);
    const [newAreaIds, setNewAreaIds] = useState<number[]>([]);

    // Form state — edit
    const [editPassword, setEditPassword] = useState('');
    const [editRole, setEditRole] = useState<'admin' | 'user'>('user');
    const [editFullName, setEditFullName] = useState('');
    const [editOrgId, setEditOrgId] = useState<number | null>(null);
    const [editI3xWrite, setEditI3xWrite] = useState(false);
    const [editSiteIds, setEditSiteIds] = useState<number[]>([]);
    const [editAreaIds, setEditAreaIds] = useState<number[]>([]);

    const { user: currentUser } = useAuthStore();

    const fetchUsers = async () => {
        try {
            setIsLoading(true);
            const data = await usersApi.list();
            setUsers(data);
            setError(null);
        } catch (err) {
            setError('Failed to load users');
            console.error(err);
        } finally {
            setIsLoading(false);
        }
    };

    useEffect(() => {
        fetchUsers();
    }, []);

    const handleCreate = async () => {
        try {
            const req: CreateUserRequest = {
                username: newUsername,
                password: newPassword,
                role: newRole,
                full_name: newFullName,
                org_id: newOrgId,
                i3x_write: newRole === 'admin' ? true : newI3xWrite,
                site_ids: newSiteIds,
                area_ids: newAreaIds,
            };
            await usersApi.create(req);
            setIsCreateOpen(false);
            resetCreateForm();
            fetchUsers();
        } catch (err: unknown) {
            const error = err as { response?: { data?: { error?: string } } };
            if (error.response?.data?.error === 'Username already exists') {
                setError('Username already exists');
            } else {
                setError('Failed to create user');
            }
            console.error(err);
        }
    };

    const handleUpdate = async () => {
        if (!selectedUser) return;
        try {
            const req: UpdateUserRequest = {
                role: editRole,
                full_name: editFullName,
                org_id: editOrgId,
                i3x_write: editRole === 'admin' ? true : editI3xWrite,
                site_ids: editSiteIds,
                area_ids: editAreaIds,
            };
            if (editPassword) {
                req.password = editPassword;
            }
            await usersApi.update(selectedUser.id, req);
            setIsEditOpen(false);
            setSelectedUser(null);
            fetchUsers();
        } catch (err) {
            setError('Failed to update user');
            console.error(err);
        }
    };

    const handleDelete = async (user: User) => {
        if (user.id === currentUser?.id) {
            setError('Cannot delete your own account');
            return;
        }
        if (confirm(`Are you sure you want to delete user "${user.username}"?`)) {
            try {
                await usersApi.delete(user.id);
                fetchUsers();
            } catch (err: unknown) {
                const error = err as { response?: { data?: { error?: string } } };
                setError(error.response?.data?.error || 'Failed to delete user');
                console.error(err);
            }
        }
    };

    const openEditDialog = (user: User) => {
        setSelectedUser(user);
        setEditRole(user.role);
        setEditFullName(user.full_name || '');
        setEditPassword('');
        setEditOrgId(user.org_id);
        setEditI3xWrite(user.i3x_write ?? false);
        setEditSiteIds(user.site_ids ?? []);
        setEditAreaIds(user.area_ids ?? []);
        setIsEditOpen(true);
    };

    const resetCreateForm = () => {
        setNewUsername('');
        setNewPassword('');
        setNewRole('user');
        setNewFullName('');
        setNewOrgId(null);
        setNewI3xWrite(false);
        setNewSiteIds([]);
        setNewAreaIds([]);
    };

    const scopeBadge = (user: User) => {
        if (!user.org_id) return null;
        const hasSiteScope = user.site_ids?.length > 0;
        const hasAreaScope = user.area_ids?.length > 0;
        if (!hasSiteScope && !hasAreaScope) return null;
        const parts: string[] = [];
        if (hasSiteScope) parts.push(`${user.site_ids.length} sito/i`);
        if (hasAreaScope) parts.push(`${user.area_ids.length} area/e`);
        return (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-orange-100 text-orange-700 ml-1">
                <MapPin size={10} />
                {parts.join(' · ')}
            </span>
        );
    };

    if (isLoading) {
        return <div className="p-8 text-center text-slate-500">Loading users...</div>;
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">User Management</h2>
                    <p className="text-muted-foreground">
                        Create, modify and delete system users.
                    </p>
                </div>
                <Dialog open={isCreateOpen} onOpenChange={setIsCreateOpen}>
                    <DialogTrigger asChild>
                        <Button className="gap-2">
                            <Plus size={16} /> Add User
                        </Button>
                    </DialogTrigger>
                    <DialogContent className="max-w-lg">
                        <DialogHeader>
                            <DialogTitle>Create User</DialogTitle>
                        </DialogHeader>
                        <div className="grid gap-4 py-4">
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label htmlFor="username" className="text-right">Username</Label>
                                <Input
                                    id="username"
                                    value={newUsername}
                                    onChange={(e) => setNewUsername(e.target.value)}
                                    className="col-span-3"
                                    placeholder="e.g. john.doe"
                                />
                            </div>
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label htmlFor="password" className="text-right">Password</Label>
                                <Input
                                    id="password"
                                    type="password"
                                    value={newPassword}
                                    onChange={(e) => setNewPassword(e.target.value)}
                                    className="col-span-3"
                                    placeholder="Min. 6 characters"
                                />
                            </div>
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label htmlFor="role" className="text-right">Role</Label>
                                <Select value={newRole} onValueChange={(v) => setNewRole(v as 'admin' | 'user')}>
                                    <SelectTrigger className="col-span-3">
                                        <SelectValue placeholder="Select role" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="user">User (Read Only)</SelectItem>
                                        <SelectItem value="admin">Admin (Full Access)</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label htmlFor="fullname" className="text-right">Full Name</Label>
                                <Input
                                    id="fullname"
                                    value={newFullName}
                                    onChange={(e) => setNewFullName(e.target.value)}
                                    className="col-span-3"
                                    placeholder="e.g. John Doe"
                                />
                            </div>
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label htmlFor="org" className="text-right">Organization</Label>
                                <Select
                                    value={newOrgId?.toString() || 'global'}
                                    onValueChange={(v) => setNewOrgId(v === 'global' ? null : parseInt(v))}
                                >
                                    <SelectTrigger className="col-span-3">
                                        <SelectValue placeholder="Select organization" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="global">Global Admin (All Organizations)</SelectItem>
                                        {!orgsLoading && organizations.map((org: any) => (
                                            <SelectItem key={org.id} value={org.id.toString()}>
                                                {org.name}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>

                            {/* Site / Area scope — only for org-scoped users */}
                            {newOrgId && (
                                <ScopeSelector
                                    orgId={newOrgId}
                                    selectedSiteIds={newSiteIds}
                                    selectedAreaIds={newAreaIds}
                                    onSiteIdsChange={setNewSiteIds}
                                    onAreaIdsChange={setNewAreaIds}
                                />
                            )}

                            {newRole !== 'admin' && (
                                <div className="grid grid-cols-4 items-center gap-4">
                                    <Label className="text-right flex items-center justify-end gap-1">
                                        <Network size={13} className="text-muted-foreground" />
                                        Permessi scrittura
                                    </Label>
                                    <div className="col-span-3 flex items-center gap-3">
                                        <Switch
                                            checked={newI3xWrite}
                                            onCheckedChange={setNewI3xWrite}
                                        />
                                        <span className="text-sm text-muted-foreground">
                                            {newI3xWrite ? 'Lettura + Scrittura' : 'Solo lettura'}
                                        </span>
                                    </div>
                                </div>
                            )}
                        </div>
                        <DialogFooter>
                            <Button variant="outline" onClick={() => setIsCreateOpen(false)}>Cancel</Button>
                            <Button onClick={handleCreate} disabled={!newUsername || !newPassword || newPassword.length < 6}>
                                Create
                            </Button>
                        </DialogFooter>
                    </DialogContent>
                </Dialog>
            </div>

            {error && (
                <div className="p-4 rounded-md bg-red-50 text-red-700 border border-red-200">
                    {error}
                    <button className="ml-2 underline" onClick={() => setError(null)}>Dismiss</button>
                </div>
            )}

            <div className="rounded-md border border-border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[80px]">ID</TableHead>
                            <TableHead>Username</TableHead>
                            <TableHead>Full Name</TableHead>
                            <TableHead>Role</TableHead>
                            <TableHead>Organization</TableHead>
                            <TableHead>Accesso</TableHead>
                            <TableHead>Created At</TableHead>
                            <TableHead className="text-right">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {users.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={8} className="h-24 text-center">
                                    No users found.
                                </TableCell>
                            </TableRow>
                        ) : (
                            users.map((user) => (
                                <TableRow key={user.id}>
                                    <TableCell className="font-medium">{user.id}</TableCell>
                                    <TableCell className="flex items-center gap-2">
                                        <Users size={16} className="text-slate-500" />
                                        <span className="font-semibold">{user.username}</span>
                                        {user.id === currentUser?.id && (
                                            <span className="text-xs bg-blue-100 text-blue-700 px-2 py-0.5 rounded">You</span>
                                        )}
                                    </TableCell>
                                    <TableCell>{user.full_name || '-'}</TableCell>
                                    <TableCell>
                                        <span className={`inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium ${user.role === 'admin'
                                            ? 'bg-purple-100 text-purple-700'
                                            : 'bg-slate-100 text-slate-700'
                                            }`}>
                                            {user.role === 'admin' ? <Shield size={12} /> : <UserIcon size={12} />}
                                            {user.role === 'admin' ? 'Admin' : 'User'}
                                        </span>
                                    </TableCell>
                                    <TableCell>
                                        {user.org_id ? (
                                            <div className="flex flex-wrap items-center gap-1">
                                                <span className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium bg-green-100 text-green-700">
                                                    <Building2 size={12} />
                                                    {user.org_name || `Org ${user.org_id}`}
                                                </span>
                                                {scopeBadge(user)}
                                            </div>
                                        ) : (
                                            <span className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium bg-gray-100 text-gray-600">
                                                <Building2 size={12} />
                                                Global Admin
                                            </span>
                                        )}
                                    </TableCell>
                                    <TableCell>
                                        {user.role === 'admin' ? (
                                            <span className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium bg-purple-100 text-purple-700">
                                                <Network size={11} /> Admin
                                            </span>
                                        ) : user.i3x_write ? (
                                            <span className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium bg-blue-100 text-blue-700">
                                                <Network size={11} /> Lettura + Scrittura
                                            </span>
                                        ) : (
                                            <span className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium bg-gray-100 text-gray-500">
                                                <Network size={11} /> Solo lettura
                                            </span>
                                        )}
                                    </TableCell>
                                    <TableCell>{new Date(user.created_at).toLocaleDateString()}</TableCell>
                                    <TableCell className="text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-8 w-8 text-slate-500 hover:text-slate-700"
                                                onClick={() => openEditDialog(user)}
                                            >
                                                <Pencil size={16} />
                                            </Button>
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-8 w-8 text-red-500 hover:text-red-600 hover:bg-red-50"
                                                onClick={() => handleDelete(user)}
                                                disabled={user.id === currentUser?.id}
                                            >
                                                <Trash2 size={16} />
                                            </Button>
                                        </div>
                                    </TableCell>
                                </TableRow>
                            ))
                        )}
                    </TableBody>
                </Table>
            </div>

            {/* Edit Dialog */}
            <Dialog open={isEditOpen} onOpenChange={setIsEditOpen}>
                <DialogContent className="max-w-lg">
                    <DialogHeader>
                        <DialogTitle>Edit User: {selectedUser?.username}</DialogTitle>
                    </DialogHeader>
                    <div className="grid gap-4 py-4">
                        <div className="grid grid-cols-4 items-center gap-4">
                            <Label htmlFor="edit-password" className="text-right">New Password</Label>
                            <Input
                                id="edit-password"
                                type="password"
                                value={editPassword}
                                onChange={(e) => setEditPassword(e.target.value)}
                                className="col-span-3"
                                placeholder="Leave empty to keep current"
                            />
                        </div>
                        <div className="grid grid-cols-4 items-center gap-4">
                            <Label htmlFor="edit-role" className="text-right">Role</Label>
                            <Select value={editRole} onValueChange={(v) => setEditRole(v as 'admin' | 'user')}>
                                <SelectTrigger className="col-span-3">
                                    <SelectValue placeholder="Select role" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="user">User (Read Only)</SelectItem>
                                    <SelectItem value="admin">Admin (Full Access)</SelectItem>
                                </SelectContent>
            </Select>
                        </div>
                        <div className="grid grid-cols-4 items-center gap-4">
                            <Label htmlFor="edit-fullname" className="text-right">Full Name</Label>
                            <Input
                                id="edit-fullname"
                                value={editFullName}
                                onChange={(e) => setEditFullName(e.target.value)}
                                className="col-span-3"
                            />
                        </div>
                        <div className="grid grid-cols-4 items-center gap-4">
                            <Label htmlFor="edit-org" className="text-right">Organization</Label>
                            <Select
                                value={editOrgId?.toString() || 'global'}
                                onValueChange={(v) => setEditOrgId(v === 'global' ? null : parseInt(v))}
                            >
                                <SelectTrigger className="col-span-3">
                                    <SelectValue placeholder="Select organization" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="global">Global Admin (All Organizations)</SelectItem>
                                    {!orgsLoading && organizations.map((org: any) => (
                                        <SelectItem key={org.id} value={org.id.toString()}>
                                            {org.name}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>

                        {/* Site / Area scope */}
                        {editOrgId && (
                            <ScopeSelector
                                orgId={editOrgId}
                                selectedSiteIds={editSiteIds}
                                selectedAreaIds={editAreaIds}
                                onSiteIdsChange={setEditSiteIds}
                                onAreaIdsChange={setEditAreaIds}
                            />
                        )}

                        {editRole !== 'admin' && (
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label className="text-right flex items-center justify-end gap-1">
                                    <Network size={13} className="text-muted-foreground" />
                                    Permessi scrittura
                                </Label>
                                <div className="col-span-3 flex items-center gap-3">
                                    <Switch
                                        checked={editI3xWrite}
                                        onCheckedChange={setEditI3xWrite}
                                    />
                                    <span className="text-sm text-muted-foreground">
                                        {editI3xWrite ? 'Lettura + Scrittura' : 'Solo lettura'}
                                    </span>
                                </div>
                            </div>
                        )}
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setIsEditOpen(false)}>Cancel</Button>
                        <Button onClick={handleUpdate}>Save Changes</Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
};

export default UsersPage;
