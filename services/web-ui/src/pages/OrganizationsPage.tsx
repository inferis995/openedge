import { useState } from 'react';
import { useOrganizations } from '@/hooks/useOrganizations';
import { useOrgLogo } from '@/hooks/useOrgLogo';
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
    Building2, Plus, Trash2, ChevronRight, Settings, WifiOff, Radar,
} from 'lucide-react';
import { Link, useNavigate } from 'react-router-dom';
import { organizationsApi } from '@/api/organizations';
import { Organization } from '@/types';
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';
import { useTranslation } from 'react-i18next';

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
    const { t } = useTranslation();
    const { data } = useEdgeStatus(orgId, true);
    if (!data) return null;
    return data.online ? (
        <Badge variant="success" className="gap-1.5">
            <span className="size-1.5 rounded-full bg-current" />
            {t('organizationsPage.edge_online')}
        </Badge>
    ) : (
        <Badge variant="muted" className="gap-1.5">
            <WifiOff size={11} />
            {t('organizationsPage.edge_offline')}
        </Badge>
    );
}

/** The organization's logo, or a neutral mark with a stripe of its colour. */
function OrgAvatar({ org }: { org: Organization }) {
    const { data: logo } = useOrgLogo(org.id);
    if (logo) {
        return (
            <div className="flex h-10 w-16 shrink-0 items-center justify-center overflow-hidden rounded-md border bg-background p-1">
                <img src={logo} alt="" className="max-h-full max-w-full object-contain" />
            </div>
        );
    }
    return (
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border bg-muted"
            style={org.theme_color ? { boxShadow: `inset 0 -3px 0 ${org.theme_color}` } : undefined}>
            <Building2 size={18} className="text-muted-foreground" />
        </div>
    );
}

const OrganizationsPage = () => {
    const navigate = useNavigate();
    const { t } = useTranslation();
    const { organizations, isLoading, create, remove } = useOrganizations();
    const { setSelectedOrgId } = useNavigationStore();
    const { isAdmin, isGlobalAdmin } = useAuthStore();

    // Create dialog
    const [isOpen, setIsOpen] = useState(false);
    const [newOrgName, setNewOrgName] = useState('');

    const handleCreate = async () => {
        if (!newOrgName.trim()) return;
        await create({ name: newOrgName.trim() });
        setIsOpen(false);
        setNewOrgName('');
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (await confirmAction({ title: i18n.t('ask.delete_org'), description: i18n.t('ask.delete_org_desc'), destructive: true })) {
            await remove(id);
        }
    };

    const handleSelect = (id: number) => {
        setSelectedOrgId(id);
        navigate('/sites');
    };

    if (isLoading) {
        return <div className="p-8 text-center text-muted-foreground">{t('organizationsPage.loading')}</div>;
    }

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h1 className="text-2xl font-semibold tracking-tight">{t('nav.organizations')}</h1>
                    <p className="text-muted-foreground text-sm">
                        {t('organizationsPage.subtitle')}
                    </p>
                </div>
                <div className="flex flex-col gap-2 sm:flex-row">
                {isGlobalAdmin() && (
                    <Button variant="outline" className="gap-2" asChild>
                        <Link to="/fleet"><Radar size={16} /> {t('orgSettings.fleet')}</Link>
                    </Button>
                )}
                {isAdmin() && (
                    <Dialog open={isOpen} onOpenChange={setIsOpen}>
                        <DialogTrigger asChild>
                            <Button className="gap-2">
                                <Plus size={16} /> {t('organizationsPage.new')}
                            </Button>
                        </DialogTrigger>
                        <DialogContent>
                            <DialogHeader>
                                <DialogTitle>{t('organizationsPage.create_title')}</DialogTitle>
                                <DialogDescription>
                                    {t('organizationsPage.create_desc')}
                                </DialogDescription>
                            </DialogHeader>
                            <div className="grid gap-4 py-4">
                                <div className="space-y-1.5">
                                    <Label htmlFor="name">{t('organizationsPage.name')}</Label>
                                    <Input
                                        id="name"
                                        value={newOrgName}
                                        onChange={(e) => setNewOrgName(e.target.value)}
                                        placeholder={t('organizationsPage.name_placeholder')}
                                        onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
                                        autoFocus
                                    />
                                </div>
                            </div>
                            <DialogFooter>
                                <Button variant="outline" onClick={() => setIsOpen(false)}>{t('common.cancel')}</Button>
                                <Button onClick={handleCreate} disabled={!newOrgName.trim()}>{t('common.create')}</Button>
                            </DialogFooter>
                        </DialogContent>
                    </Dialog>
                )}
                </div>
            </div>

            {/* Org cards */}
            {organizations.length === 0 ? (
                <div className="rounded-lg border border-dashed p-12 text-center space-y-3">
                    <Building2 size={40} className="mx-auto text-muted-foreground/40" />
                    <p className="text-muted-foreground">{t('organizationsPage.empty')}</p>
                    {isAdmin() && (
                        <Button variant="outline" onClick={() => setIsOpen(true)} className="gap-2">
                            <Plus size={14} /> {t('organizationsPage.create_first')}
                        </Button>
                    )}
                </div>
            ) : (
                <div className="space-y-3">
                    <p className="text-sm text-muted-foreground">{t('orgSettings.count', { count: organizations.length })}</p>
                    {organizations.map((org) => (
                        <div
                            key={org.id}
                            className="group rounded-lg border bg-card hover:border-primary/40 transition-colors"
                        >
                            {/* Main row: stacked on mobile so actions never cover the metadata */}
                            <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:gap-4">
                                <div className="flex min-w-0 flex-1 items-center gap-3">
                                    <OrgAvatar org={org} />

                                    <div className="flex-1 min-w-0">
                                        <div className="flex items-center gap-2 flex-wrap">
                                            <span className="font-semibold text-base">{org.name}</span>
                                            <EdgeBadge orgId={org.id} />
                                        </div>
                                        <p className="text-xs text-muted-foreground mt-0.5">
                                            {t('organizationsPage.created_on', { date: new Date(org.created_at).toLocaleDateString(i18n.language, {
                                                year: 'numeric', month: 'long', day: 'numeric',
                                            }) })}
                                            {' · '}ID #{org.id}
                                        </p>
                                    </div>
                                </div>

                                {/* Actions */}
                                <div className="flex flex-wrap items-center gap-1.5 shrink-0 sm:justify-end">
                                    {isAdmin() && (
                                        <>
                                            {/* All the organization's settings: name, colour, logo,
                                                pages, security, edge boxes, keys, invites… */}
                                            <Button variant="outline" size="sm" className="gap-1.5 text-xs h-10 sm:h-8" asChild>
                                                <Link to={`/organizations/${org.id}/settings`}>
                                                    <Settings size={13} />
                                                    {t('orgSettings.open')}
                                                </Link>
                                            </Button>

                                            {/* Delete */}
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-10 sm:h-8 w-10 sm:w-8 text-red-500 hover:text-red-600 hover:bg-red-500/10"
                                                aria-label={t('common.delete')}
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
                                        {t('organizationsPage.enter')}
                                        <ChevronRight size={13} />
                                    </Button>
                                </div>
                            </div>
                        </div>
                    ))}
                </div>
            )}

        </div>
    );
};

export default OrganizationsPage;
