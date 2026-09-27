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
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';
import { EmptyState } from '@/components/ui/empty-state';
import { MapPin as MapPinIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';

const AreasPage = () => {
    const navigate = useNavigate();
    const { selectedSiteId, selectedOrgId, setSelectedAreaId } = useNavigationStore();
    const { areas, isLoading, create, remove, update } = useAreas(selectedSiteId);
    const { sites } = useSites(selectedOrgId); // Get sites for current org to map names
    const { isAdmin } = useAuthStore();
    const { t } = useTranslation();

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
        if (await confirmAction({ title: i18n.t('ask.delete_area'), description: i18n.t('ask.delete_area_desc'), destructive: true })) {
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
                    <h2 className="text-2xl font-bold tracking-tight">{t('nav.areas')}</h2>
                    <p className="text-muted-foreground">
                        {t('areas.subtitle')}
                    </p>
                </div>
                {isAdmin() && (
                    <>
                        <Dialog open={isOpen} onOpenChange={setIsOpen}>
                            <DialogTrigger asChild>
                                <Button className="gap-2">
                                    <Plus size={16} /> {t('areas.add')}
                                </Button>
                            </DialogTrigger>
                            <DialogContent>
                                <DialogHeader>
                                    <DialogTitle>{t('areas.create_title')}</DialogTitle>
                                    <DialogDescription>
                                        {t('empty.areas_desc')}
                                    </DialogDescription>
                                </DialogHeader>
                                <div className="grid gap-4 py-4">
                                    <div className="grid gap-2">
                                        <Label htmlFor="site">{t('common.site')}</Label>
                                        <Select
                                            value={selectedSiteForCreate}
                                            onValueChange={setSelectedSiteForCreate}
                                        >
                                            <SelectTrigger>
                                                <SelectValue placeholder={t('common.select_site')} />
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
                                        <Label htmlFor="name">{t('areas.name')}</Label>
                                        <Input
                                            id="name"
                                            value={newAreaName}
                                            onChange={(e) => setNewAreaName(e.target.value)}
                                            placeholder={t('areas.name_placeholder')}
                                        />
                                    </div>
                                </div>
                                <DialogFooter>
                                    <Button onClick={handleCreate}>{t('common.create')}</Button>
                                </DialogFooter>
                            </DialogContent>
                        </Dialog>

                        <Dialog open={isEditOpen} onOpenChange={setIsEditOpen}>
                            <DialogContent>
                                <DialogHeader>
                                    <DialogTitle>{t('areas.edit_title')}</DialogTitle>
                                    <DialogDescription>
                                        {t('areas.edit_desc')}
                                    </DialogDescription>
                                </DialogHeader>
                                <div className="grid gap-4 py-4">
                                    <div className="grid gap-2">
                                        <Label htmlFor="edit-name">{t('areas.name')}</Label>
                                        <Input
                                            id="edit-name"
                                            value={editingArea?.name || ''}
                                            onChange={(e) => setEditingArea(prev => prev ? { ...prev, name: e.target.value } : null)}
                                            placeholder={t('areas.name_placeholder')}
                                        />
                                    </div>
                                </div>
                                <DialogFooter>
                                    <Button onClick={handleUpdate}>{t('common.save')}</Button>
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
                            <TableHead>{t('common.name')}</TableHead>
                            <TableHead>{t('common.site')}</TableHead>
                            <TableHead>{t('common.created_at')}</TableHead>
                            {isAdmin() && <TableHead className="text-right">{t('common.actions')}</TableHead>}
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {areas.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={5}>
                                    <EmptyState
                                        icon={MapPinIcon}
                                        title={t('empty.areas_title')}
                                        description={selectedSiteId ? t('empty.areas_desc') : t('empty.areas_pick_site')}
                                        action={selectedSiteId
                                            ? (isAdmin() ? <Button onClick={() => setIsOpen(true)}>{t('empty.areas_create')}</Button> : undefined)
                                            : <Button variant="outline" onClick={() => navigate('/sites')}>{t('empty.go_sites')}</Button>}
                                    />
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
                                        <TableCell>{new Date(area.created_at).toLocaleDateString(i18n.language)}</TableCell>
                                        {isAdmin() && (
                                            <TableCell className="text-right">
                                                <div className="flex items-center justify-end gap-2">
                                                    <Button
                                                        variant="ghost"
                                                        size="sm"
                                                        onClick={(e) => handleEdit(e, area)}
                                                    >
                                                        {t('common.edit')}
                                                    </Button>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-10 sm:h-8 w-10 sm:w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
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
