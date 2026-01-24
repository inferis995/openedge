import { useState } from 'react';
import { useSites } from '@/hooks/useSites';
import { useOrganizations } from '@/hooks/useOrganizations';
import { useNavigationStore } from '@/stores/useNavigationStore';
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
import { Factory, Plus, Trash2, ChevronRight, Building2 } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

const SitesPage = () => {
    const navigate = useNavigate();
    const { selectedOrgId, setSelectedSiteId } = useNavigationStore();
    const { sites, isLoading, create, remove } = useSites(selectedOrgId);
    const { organizations } = useOrganizations();

    const [isOpen, setIsOpen] = useState(false);
    const [newSiteName, setNewSiteName] = useState('');
    const [selectedOrgForCreate, setSelectedOrgForCreate] = useState<string>(
        selectedOrgId ? selectedOrgId.toString() : ''
    );

    const handleCreate = async () => {
        if (!newSiteName || !selectedOrgForCreate) return;
        try {
            await create({
                org_id: parseInt(selectedOrgForCreate),
                name: newSiteName
            });
            setIsOpen(false);
            setNewSiteName('');
        } catch (error) {
            console.error('Failed to create site', error);
        }
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (confirm('Are you sure you want to delete this site?')) {
            try {
                await remove(id);
            } catch (error) {
                console.error('Failed to delete site', error);
            }
        }
    };

    const handleSelect = (id: number) => {
        setSelectedSiteId(id);
        navigate('/areas');
    };

    if (isLoading) {
        return <div className="p-8 text-center text-slate-500">Loading sites...</div>;
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Sites</h2>
                    <p className="text-muted-foreground">
                        Manage production sites and facilities.
                    </p>
                </div>
                <Dialog open={isOpen} onOpenChange={setIsOpen}>
                    <DialogTrigger asChild>
                        <Button className="gap-2">
                            <Plus size={16} /> Add Site
                        </Button>
                    </DialogTrigger>
                    <DialogContent>
                        <DialogHeader>
                            <DialogTitle>Create Site</DialogTitle>
                        </DialogHeader>
                        <div className="grid gap-4 py-4">
                            <div className="grid gap-2">
                                <Label htmlFor="org">Organization</Label>
                                <Select
                                    value={selectedOrgForCreate}
                                    onValueChange={setSelectedOrgForCreate}
                                >
                                    <SelectTrigger>
                                        <SelectValue placeholder="Select Organization" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {organizations.map((org) => (
                                            <SelectItem key={org.id} value={org.id.toString()}>
                                                {org.name}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>
                            <div className="grid gap-2">
                                <Label htmlFor="name">Site Name</Label>
                                <Input
                                    id="name"
                                    value={newSiteName}
                                    onChange={(e) => setNewSiteName(e.target.value)}
                                    placeholder="e.g. Milano Production Plant"
                                />
                            </div>
                        </div>
                        <DialogFooter>
                            <Button onClick={handleCreate}>Create</Button>
                        </DialogFooter>
                    </DialogContent>
                </Dialog>
            </div>

            <div className="rounded-md border bg-white">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[80px]">ID</TableHead>
                            <TableHead>Name</TableHead>
                            <TableHead>Organization</TableHead>
                            <TableHead>Created At</TableHead>
                            <TableHead className="text-right">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {sites.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={5} className="h-24 text-center">
                                    No sites found. {selectedOrgId ? 'Create one for the selected organization.' : 'Select an organization to filter or create a new site.'}
                                </TableCell>
                            </TableRow>
                        ) : (
                            sites.map((site) => {
                                const orgName = organizations.find(o => o.id === site.organization_id)?.name || site.organization_id;
                                return (
                                    <TableRow
                                        key={site.id}
                                        className="cursor-pointer hover:bg-slate-50"
                                        onClick={() => handleSelect(site.id)}
                                    >
                                        <TableCell className="font-medium">{site.id}</TableCell>
                                        <TableCell className="flex items-center gap-2">
                                            <Factory size={16} className="text-slate-500" />
                                            <span className="font-semibold">{site.name}</span>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-2 text-muted-foreground text-xs">
                                                <Building2 size={12} />
                                                {orgName}
                                            </div>
                                        </TableCell>
                                        <TableCell>{new Date(site.created_at).toLocaleDateString()}</TableCell>
                                        <TableCell className="text-right">
                                            <div className="flex items-center justify-end gap-2">
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-8 w-8 text-red-500 hover:text-red-600 hover:bg-red-50"
                                                    onClick={(e) => handleDelete(e, site.id)}
                                                >
                                                    <Trash2 size={16} />
                                                </Button>
                                                <ChevronRight size={16} className="text-slate-300" />
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                );
                            })
                        )}
                    </TableBody>
                </Table>
            </div>
        </div>
    );
};

export default SitesPage;
