import { useState } from 'react';
import { useOrganizations } from '@/hooks/useOrganizations';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useAuthStore } from '@/stores/useAuthStore';
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
import { Building2, Plus, Trash2, ChevronRight } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

const OrganizationsPage = () => {
    const navigate = useNavigate();
    const { organizations, isLoading, create, update, remove } = useOrganizations();
    const { setSelectedOrgId } = useNavigationStore();
    const { isAdmin } = useAuthStore();
    const [isOpen, setIsOpen] = useState(false);
    const [newOrgName, setNewOrgName] = useState('');

    // Edit state
    const [isEditOpen, setIsEditOpen] = useState(false);
    const [editingOrg, setEditingOrg] = useState<{ id: number, name: string } | null>(null);

    const handleCreate = async () => {
        try {
            await create({ name: newOrgName });
            setIsOpen(false);
            setNewOrgName('');
        } catch (error) {
            console.error('Failed to create organization', error);
        }
    };

    const handleEditClick = (e: React.MouseEvent, org: { id: number, name: string }) => {
        e.stopPropagation();
        setEditingOrg(org);
        setIsEditOpen(true);
    };

    const handleUpdate = async () => {
        if (!editingOrg) return;
        try {
            await update({ id: editingOrg.id, data: { name: editingOrg.name } });
            setIsEditOpen(false);
            setEditingOrg(null);
        } catch (error) {
            console.error('Failed to update organization', error);
        }
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (confirm('Are you sure you want to delete this organization?')) {
            try {
                await remove(id);
            } catch (error) {
                console.error('Failed to delete organization', error);
            }
        }
    };

    const handleSelect = (id: number) => {
        setSelectedOrgId(id);
        navigate('/sites');
    };

    if (isLoading) {
        return <div className="p-8 text-center text-slate-500">Loading organizations...</div>;
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Organizations</h2>
                    <p className="text-muted-foreground">
                        Manage top-level organizations in the system hierarchy.
                    </p>
                </div>
                {isAdmin() && (
                    <>
                        <Dialog open={isOpen} onOpenChange={setIsOpen}>
                            <DialogTrigger asChild>
                                <Button className="gap-2">
                                    <Plus size={16} /> Add Organization
                                </Button>
                            </DialogTrigger>
                            <DialogContent>
                                <DialogHeader>
                                    <DialogTitle>Create Organization</DialogTitle>
                                </DialogHeader>
                                <div className="grid gap-4 py-4">
                                    <div className="grid grid-cols-4 items-center gap-4">
                                        <Label htmlFor="name" className="text-right">
                                            Name
                                        </Label>
                                        <Input
                                            id="name"
                                            value={newOrgName}
                                            onChange={(e) => setNewOrgName(e.target.value)}
                                            className="col-span-3"
                                            placeholder="e.g. Acme Industries"
                                        />
                                    </div>
                                </div>
                                <DialogFooter>
                                    <Button onClick={handleCreate}>Create</Button>
                                </DialogFooter>
                            </DialogContent>
                        </Dialog>

                        <Dialog open={isEditOpen} onOpenChange={setIsEditOpen}>
                            <DialogContent>
                                <DialogHeader>
                                    <DialogTitle>Edit Organization</DialogTitle>
                                </DialogHeader>
                                <div className="grid gap-4 py-4">
                                    <div className="grid grid-cols-4 items-center gap-4">
                                        <Label htmlFor="edit-name" className="text-right">
                                            Name
                                        </Label>
                                        <Input
                                            id="edit-name"
                                            value={editingOrg?.name || ''}
                                            onChange={(e) => setEditingOrg(prev => prev ? { ...prev, name: e.target.value } : null)}
                                            className="col-span-3"
                                        />
                                    </div>
                                </div>
                                <DialogFooter>
                                    <Button onClick={handleUpdate}>Update</Button>
                                </DialogFooter>
                            </DialogContent>
                        </Dialog>
                    </>
                )}
            </div>

            <div className="rounded-md border bg-white">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[80px]">ID</TableHead>
                            <TableHead>Name</TableHead>
                            <TableHead>Created At</TableHead>
                            {isAdmin() && <TableHead className="text-right">Actions</TableHead>}
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {organizations.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={4} className="h-24 text-center">
                                    No organizations found. Create one to get started.
                                </TableCell>
                            </TableRow>
                        ) : (
                            organizations.map((org) => (
                                <TableRow
                                    key={org.id}
                                    className="cursor-pointer hover:bg-slate-50"
                                    onClick={() => handleSelect(org.id)}
                                >
                                    <TableCell className="font-medium">{org.id}</TableCell>
                                    <TableCell className="flex items-center gap-2">
                                        <Building2 size={16} className="text-slate-500" />
                                        <span className="font-semibold">{org.name}</span>
                                    </TableCell>
                                    <TableCell>{new Date(org.created_at).toLocaleDateString()}</TableCell>
                                    {isAdmin() && (
                                        <TableCell className="text-right">
                                            <div className="flex items-center justify-end gap-2">
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-8 w-8 text-blue-500 hover:text-blue-600 hover:bg-blue-50"
                                                    onClick={(e) => handleEditClick(e, org)}
                                                >
                                                    <Building2 size={16} />
                                                </Button>
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-8 w-8 text-red-500 hover:text-red-600 hover:bg-red-50"
                                                    onClick={(e) => handleDelete(e, org.id)}
                                                >
                                                    <Trash2 size={16} />
                                                </Button>
                                                <ChevronRight size={16} className="text-slate-300" />
                                            </div>
                                        </TableCell>
                                    )}
                                </TableRow>
                            ))
                        )}
                    </TableBody>
                </Table>
            </div>
        </div>
    );
};

export default OrganizationsPage;
