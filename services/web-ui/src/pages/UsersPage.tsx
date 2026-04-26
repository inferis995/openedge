import { useState, useEffect } from 'react';
import { usersApi, User, CreateUserRequest, UpdateUserRequest } from '@/api/users';
import { useAuthStore } from '@/stores/useAuthStore';
import { useOrganizations } from '@/hooks/useOrganizations';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
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
import { Users, Plus, Trash2, Pencil, Shield, User as UserIcon, Building2, Network } from 'lucide-react';
import { Switch } from '@/components/ui/switch';

const UsersPage = () => {
    const [users, setUsers] = useState<User[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isCreateOpen, setIsCreateOpen] = useState(false);
    const [isEditOpen, setIsEditOpen] = useState(false);
    const [selectedUser, setSelectedUser] = useState<User | null>(null);
    const [error, setError] = useState<string | null>(null);

    // Organizations
    const { organizations, isLoading: orgsLoading } = useOrganizations();

    // Form state for create
    const [newUsername, setNewUsername] = useState('');
    const [newPassword, setNewPassword] = useState('');
    const [newRole, setNewRole] = useState<'admin' | 'user'>('user');
    const [newFullName, setNewFullName] = useState('');
    const [newOrgId, setNewOrgId] = useState<number | null>(null);
    const [newI3xWrite, setNewI3xWrite] = useState(false);

    // Form state for edit
    const [editPassword, setEditPassword] = useState('');
    const [editRole, setEditRole] = useState<'admin' | 'user'>('user');
    const [editFullName, setEditFullName] = useState('');
    const [editOrgId, setEditOrgId] = useState<number | null>(null);
    const [editI3xWrite, setEditI3xWrite] = useState(false);

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
        setIsEditOpen(true);
    };

    const resetCreateForm = () => {
        setNewUsername('');
        setNewPassword('');
        setNewRole('user');
        setNewFullName('');
        setNewOrgId(null);
        setNewI3xWrite(false);
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
                    <DialogContent>
                        <DialogHeader>
                            <DialogTitle>Create User</DialogTitle>
                        </DialogHeader>
                        <div className="grid gap-4 py-4">
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label htmlFor="username" className="text-right">
                                    Username
                                </Label>
                                <Input
                                    id="username"
                                    value={newUsername}
                                    onChange={(e) => setNewUsername(e.target.value)}
                                    className="col-span-3"
                                    placeholder="e.g. john.doe"
                                />
                            </div>
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label htmlFor="password" className="text-right">
                                    Password
                                </Label>
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
                                <Label htmlFor="role" className="text-right">
                                    Role
                                </Label>
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
                                <Label htmlFor="fullname" className="text-right">
                                    Full Name
                                </Label>
                                <Input
                                    id="fullname"
                                    value={newFullName}
                                    onChange={(e) => setNewFullName(e.target.value)}
                                    className="col-span-3"
                                    placeholder="e.g. John Doe"
                                />
                            </div>
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label htmlFor="org" className="text-right">
                                    Organization
                                </Label>
                                <Select value={newOrgId?.toString() || 'global'} onValueChange={(v) => setNewOrgId(v === 'global' ? null : parseInt(v))}>
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
                            {newRole !== 'admin' && (
                                <div className="grid grid-cols-4 items-center gap-4">
                                    <Label className="text-right flex items-center justify-end gap-1">
                                        <Network size={13} className="text-muted-foreground" />
                                        i3X Write
                                    </Label>
                                    <div className="col-span-3 flex items-center gap-3">
                                        <Switch
                                            checked={newI3xWrite}
                                            onCheckedChange={setNewI3xWrite}
                                        />
                                        <span className="text-sm text-muted-foreground">
                                            {newI3xWrite ? 'Read + Write' : 'Read only'}
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
                            <TableHead>i3X</TableHead>
                            <TableHead>Created At</TableHead>
                            <TableHead className="text-right">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {users.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={7} className="h-24 text-center">
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
                                            <span className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium bg-green-100 text-green-700">
                                                <Building2 size={12} />
                                                {user.org_name || `Org ${user.org_id}`}
                                            </span>
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
                                                <Network size={11} /> Full
                                            </span>
                                        ) : user.i3x_write ? (
                                            <span className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium bg-blue-100 text-blue-700">
                                                <Network size={11} /> R+W
                                            </span>
                                        ) : (
                                            <span className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium bg-gray-100 text-gray-500">
                                                <Network size={11} /> Read
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
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Edit User: {selectedUser?.username}</DialogTitle>
                    </DialogHeader>
                    <div className="grid gap-4 py-4">
                        <div className="grid grid-cols-4 items-center gap-4">
                            <Label htmlFor="edit-password" className="text-right">
                                New Password
                            </Label>
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
                            <Label htmlFor="edit-role" className="text-right">
                                Role
                            </Label>
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
                            <Label htmlFor="edit-fullname" className="text-right">
                                Full Name
                            </Label>
                            <Input
                                id="edit-fullname"
                                value={editFullName}
                                onChange={(e) => setEditFullName(e.target.value)}
                                className="col-span-3"
                            />
                        </div>
                        <div className="grid grid-cols-4 items-center gap-4">
                            <Label htmlFor="edit-org" className="text-right">
                                Organization
                            </Label>
                            <Select value={editOrgId?.toString() || 'global'} onValueChange={(v) => setEditOrgId(v === 'global' ? null : parseInt(v))}>
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
                        {editRole !== 'admin' && (
                            <div className="grid grid-cols-4 items-center gap-4">
                                <Label className="text-right flex items-center justify-end gap-1">
                                    <Network size={13} className="text-muted-foreground" />
                                    i3X Write
                                </Label>
                                <div className="col-span-3 flex items-center gap-3">
                                    <Switch
                                        checked={editI3xWrite}
                                        onCheckedChange={setEditI3xWrite}
                                    />
                                    <span className="text-sm text-muted-foreground">
                                        {editI3xWrite ? 'Read + Write' : 'Read only'}
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
