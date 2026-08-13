import { useState } from 'react';
import { useSites } from '@/hooks/useSites';
import { sitesApi } from '@/api/sites';
import { useAreas } from '@/hooks/useAreas';
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
    DialogDescription,
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
import { MapPin, Plus, Trash2, ChevronRight, Factory } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

const AreasPage = () => {
    const navigate = useNavigate();
    const { selectedSiteId, selectedOrgId, setSelectedAreaId } = useNavigationStore();
    const { areas, isLoading, create, remove, update } = useAreas(selectedSiteId);
    const { sites } = useSites(selectedOrgId); // Get sites for current org to map names
    const { isAdmin } = useAuthStore();

    const [isOpen, setIsOpen] = useState(false);
    const [isEditOpen, setIsEditOpen] = useState(false);
    const [newAreaName, setNewAreaName] = useState('');
    const [editingArea, setEditingArea] = useState<{ id: number; name: string } | null>(null);
    const [selectedSiteForCreate, setSelectedSiteForCreate] = useState<string>(
        selectedSiteId ? selectedSiteId.toString() : ''
    );

    // Removed hook useSite to avoid race conditions. We fetch JIT.

    const handleCreate = async () => {
        if (!newAreaName || !selectedSiteForCreate) return;

        let orgIdToUse = selectedOrgId || undefined;

        // Always fetch the site to get the authoritative org_id
        if (selectedSiteForCreate) {
            try {
                const siteData = await sitesApi.get(parseInt(selectedSiteForCreate));
                orgIdToUse = siteData.org_id;
            } catch (e) {
                console.error("Could not fetch site details", e);
                // Fallthrough, might fail
            }
        }

        try {
            await create({
                site_id: parseInt(selectedSiteForCreate),
                name: newAreaName,
                org_id: orgIdToUse
            });
            setIsOpen(false);
            setNewAreaName('');
        } catch (error) {
            console.error('Failed to create area', error);
        }
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (confirm('Are you sure you want to delete this area?')) {
            try {
                await remove(id);
            } catch (error) {
                console.error('Failed to delete area', error);
            }
        }
    };

    const handleSelect = (id: number) => {
        setSelectedAreaId(id);
        navigate('/gateways');
    };

    if (isLoading) {
        return <div className="p-8 text-center text-muted-foreground">Loading areas...</div>;
    }

    const handleEdit = (e: React.MouseEvent, area: { id: number; name: string }) => {
        e.stopPropagation();
        setEditingArea(area);
        setIsEditOpen(true);
    };

    const handleUpdate = async () => {
        if (!editingArea || !editingArea.name) return;
        try {
            await update({ id: editingArea.id, data: { name: editingArea.name } });
            setIsEditOpen(false);
            setEditingArea(null);
        } catch (error) {
            console.error('Failed to update area', error);
        }
    };

    return (
        <div className="space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Areas</h2>
                    <p className="text-muted-foreground">
                        Define logical areas within sites (e.g. Line 1, Warehouse).
                    </p>
                </div>
                {isAdmin() && (
                    <>
                        <Dialog open={isOpen} onOpenChange={setIsOpen}>
                            <DialogTrigger asChild>
                                <Button className="gap-2">
                                    <Plus size={16} /> Add Area
                                </Button>
                            </DialogTrigger>
                            <DialogContent>
                                <DialogHeader>
                                    <DialogTitle>Create Area</DialogTitle>
                                    <DialogDescription>
                                        Enter the details for the new area.
                                    </DialogDescription>
                                </DialogHeader>
                                <div className="grid gap-4 py-4">
                                    <div className="grid gap-2">
                                        <Label htmlFor="site">Site</Label>
                                        <Select
                                            value={selectedSiteForCreate}
                                            onValueChange={setSelectedSiteForCreate}
                                        >
                                            <SelectTrigger>
                                                <SelectValue placeholder="Select Site" />
                                            </SelectTrigger>
                                            <SelectContent>
                                                {sites.map((site) => (
                                                    <SelectItem key={site.id} value={site.id.toString()}>
                                                        {site.name}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div className="grid gap-2">
                                        <Label htmlFor="name">Area Name</Label>
                                        <Input
                                            id="name"
                                            value={newAreaName}
                                            onChange={(e) => setNewAreaName(e.target.value)}
                                            placeholder="e.g. Assembly Line 1"
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
                                    <DialogTitle>Edit Area</DialogTitle>
                                    <DialogDescription>
                                        Update the name of the area.
                                    </DialogDescription>
                                </DialogHeader>
                                <div className="grid gap-4 py-4">
                                    <div className="grid gap-2">
                                        <Label htmlFor="edit-name">Area Name</Label>
                                        <Input
                                            id="edit-name"
                                            value={editingArea?.name || ''}
                                            onChange={(e) => setEditingArea(prev => prev ? { ...prev, name: e.target.value } : null)}
                                            placeholder="e.g. Assembly Line 1"
                                        />
                                    </div>
                                </div>
                                <DialogFooter>
                                    <Button onClick={handleUpdate}>Save Changes</Button>
                                </DialogFooter>
                            </DialogContent>
                        </Dialog>
                    </>
                )}
            </div>

            <div className="clip-chamfer border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[80px]">ID</TableHead>
                            <TableHead>Name</TableHead>
                            <TableHead>Site</TableHead>
                            <TableHead>Created At</TableHead>
                            {isAdmin() && <TableHead className="text-right">Actions</TableHead>}
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {areas.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={5} className="h-24 text-center">
                                    No areas found. {selectedSiteId ? 'Create one for the selected site.' : 'Select a site to filter or create a new area.'}
                                </TableCell>
                            </TableRow>
                        ) : (
                            areas.map((area) => {
                                const siteName = sites.find(s => s.id === area.site_id)?.name || area.site_id;
                                return (
                                    <TableRow
                                        key={area.id}
                                        className="cursor-pointer hover:bg-muted/50"
                                        onClick={() => handleSelect(area.id)}
                                    >
                                        <TableCell className="font-medium">{area.id}</TableCell>
                                        <TableCell className="flex items-center gap-2">
                                            <MapPin size={16} className="text-muted-foreground" />
                                            <span className="font-semibold">{area.name}</span>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-2 text-muted-foreground text-xs">
                                                <Factory size={12} />
                                                {siteName}
                                            </div>
                                        </TableCell>
                                        <TableCell>{new Date(area.created_at).toLocaleDateString()}</TableCell>
                                        {isAdmin() && (
                                            <TableCell className="text-right">
                                                <div className="flex items-center justify-end gap-2">
                                                    <Button
                                                        variant="ghost"
                                                        size="sm"
                                                        onClick={(e) => handleEdit(e, area)}
                                                    >
                                                        Edit
                                                    </Button>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                                                        onClick={(e) => handleDelete(e, area.id)}
                                                    >
                                                        <Trash2 size={16} />
                                                    </Button>
                                                    <ChevronRight size={16} className="text-muted-foreground" />
                                                </div>
                                            </TableCell>
                                        )}
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

export default AreasPage;
