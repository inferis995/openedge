import { useState } from 'react';
import { useSites } from '@/hooks/useSites';
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
import { Factory, Plus, Trash2, ChevronRight, Building2 } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';
import { EmptyState } from '@/components/ui/empty-state';
import { Factory as FactoryIcon } from 'lucide-react';

const SitesPage = () => {
    const navigate = useNavigate();
    const { selectedOrgId, setSelectedSiteId } = useNavigationStore();
    const { sites, isLoading, create, remove, update } = useSites(selectedOrgId);
    const { organizations } = useOrganizations();
    const { isAdmin } = useAuthStore();
    const { t } = useTranslation();

    const [isOpen, setIsOpen] = useState(false);
    const [isEditOpen, setIsEditOpen] = useState(false);
    const [newSiteName, setNewSiteName] = useState('');
    const [editingSite, setEditingSite] = useState<{ id: number; name: string } | null>(null);
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
        if (await confirmAction({ title: i18n.t('ask.delete_site'), description: i18n.t('ask.delete_site_desc'), destructive: true })) {
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
        return <div className="p-8 text-center text-muted-foreground">Loading sites...</div>;
    }

    const handleEdit = (e: React.MouseEvent, site: { id: number; name: string }) => {
        e.stopPropagation();
        setEditingSite(site);
        setIsEditOpen(true);
    };

    const handleUpdate = async () => {
        if (!editingSite || !editingSite.name) return;
        try {
            await update({ id: editingSite.id, data: { name: editingSite.name } });
            setIsEditOpen(false);
            setEditingSite(null);
        } catch (error) {
            console.error('Failed to update site', error);
        }
    };

    return (
        <div className="space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">{t('nav.sites')}</h2>
                    <p className="text-muted-foreground">
                        {t('sites.subtitle')}
                    </p>
                </div>
                {isAdmin() && (
                    <>
                        <Dialog open={isOpen} onOpenChange={setIsOpen}>
                            <DialogTrigger asChild>
                                <Button className="gap-2">
                                    <Plus size={16} /> {t('sites.add')}
                                </Button>
                            </DialogTrigger>
                            <DialogContent>
                                <DialogHeader>
                                    <DialogTitle>{t('sites.create_title')}</DialogTitle>
                                    <DialogDescription>
                                        {t('empty.sites_desc')}
                                    </DialogDescription>
                                </DialogHeader>
                                <div className="grid gap-4 py-4">
                                    <div className="grid gap-2">
                                        <Label htmlFor="org">{t('common.organization')}</Label>
                                        <Select
                                            value={selectedOrgForCreate}
                                            onValueChange={setSelectedOrgForCreate}
                                        >
                                            <SelectTrigger>
                                                <SelectValue placeholder={t('common.select_organization')} />
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
                                        <Label htmlFor="name">{t('sites.name')}</Label>
                                        <Input
                                            id="name"
                                            value={newSiteName}
                                            onChange={(e) => setNewSiteName(e.target.value)}
                                            placeholder={t('sites.name_placeholder')}
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
                                    <DialogTitle>{t('sites.edit_title')}</DialogTitle>
                                    <DialogDescription>
                                        {t('sites.edit_desc')}
                                    </DialogDescription>
                                </DialogHeader>
                                <div className="grid gap-4 py-4">
                                    <div className="grid gap-2">
                                        <Label htmlFor="edit-name">{t('sites.name')}</Label>
                                        <Input
                                            id="edit-name"
                                            value={editingSite?.name || ''}
                                            onChange={(e) => setEditingSite(prev => prev ? { ...prev, name: e.target.value } : null)}
                                            placeholder={t('sites.name_placeholder')}
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

            <div className="rounded-md border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[80px]">ID</TableHead>
                            <TableHead>{t('common.name')}</TableHead>
                            <TableHead>{t('common.organization')}</TableHead>
                            <TableHead>{t('common.created_at')}</TableHead>
                            {isAdmin() && <TableHead className="text-right">{t('common.actions')}</TableHead>}
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {sites.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={5}>
                                    <EmptyState
                                        icon={FactoryIcon}
                                        title={t('empty.sites_title')}
                                        description={selectedOrgId ? t('empty.sites_desc') : t('empty.sites_pick_org')}
                                        action={selectedOrgId && isAdmin() ? (
                                            <Button onClick={() => setIsOpen(true)}>{t('empty.sites_create')}</Button>
                                        ) : undefined}
                                    />
                                </TableCell>
                            </TableRow>
                        ) : (
                            sites.map((site) => {
                                const orgName = organizations.find(o => o.id === site.org_id)?.name || site.org_id;
                                return (
                                    <TableRow
                                        key={site.id}
                                        className="cursor-pointer hover:bg-muted/50"
                                        onClick={() => handleSelect(site.id)}
                                    >
                                        <TableCell className="font-medium">{site.id}</TableCell>
                                        <TableCell className="flex items-center gap-2">
                                            <Factory size={16} className="text-muted-foreground" />
                                            <span className="font-semibold">{site.name}</span>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-2 text-muted-foreground text-xs">
                                                <Building2 size={12} />
                                                {orgName}
                                            </div>
                                        </TableCell>
                                        <TableCell>{new Date(site.created_at).toLocaleDateString(i18n.language)}</TableCell>
                                        {isAdmin() && (
                                            <TableCell className="text-right">
                                                <div className="flex items-center justify-end gap-2">
                                                    <Button
                                                        variant="ghost"
                                                        size="sm"
                                                        onClick={(e) => handleEdit(e, site)}
                                                    >
                                                        {t('common.edit')}
                                                    </Button>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-10 sm:h-8 w-10 sm:w-8 text-red-500 hover:text-red-600 hover:bg-red-500/10"
                                                        onClick={(e) => handleDelete(e, site.id)}
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
        </div >
    );
};

export default SitesPage;
