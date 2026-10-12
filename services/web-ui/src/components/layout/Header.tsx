import { useNavigate, useLocation } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ChevronRight, Building2, Factory, MapPin, X, Menu } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useMqttStore } from '@/stores/useMqttStore';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useOrgModules } from '@/hooks/useOrgModules';
import { useAuthStore } from '@/stores/useAuthStore';
import { organizationsApi } from '@/api/organizations';
import { sitesApi } from '@/api/sites';
import { areasApi } from '@/api/areas';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { findNavItem, navSections } from '@/components/layout/navigation';

interface HeaderProps {
    /** Opens the navigation drawer. Only rendered under md, where the sidebar
     *  is not on screen and there would otherwise be no way to navigate. */
    onMenuClick?: () => void;
}

const Header = ({ onMenuClick }: HeaderProps) => {
    const navigate = useNavigate();
    const location = useLocation();

    const { isMqttConnected, connectMqtt } = useMqttStore();
    const { user, isOrgScoped, isAdmin, isGlobalAdmin } = useAuthStore();
    const orgModules = useOrgModules(user?.org_id);
    const { t } = useTranslation();

    // Get navigation context
    const { selectedOrgId, selectedSiteId, selectedAreaId, setSelectedOrgId, setSelectedSiteId, setSelectedAreaId, clearSelection } = useNavigationStore();

    // Organizations for the switcher: a global admin can hop between tenants.
    const { data: orgsData } = useQuery({
        queryKey: ['organizations'],
        queryFn: () => organizationsApi.getAll(),
        enabled: isGlobalAdmin(),
        staleTime: 5 * 60 * 1000,
    });
    const orgs = orgsData ?? [];

    // Fetch context names
    const { data: org } = useQuery({
        queryKey: ['org', selectedOrgId],
        queryFn: () => organizationsApi.get(selectedOrgId ?? -1),
        enabled: !!selectedOrgId
    });

    const { data: site } = useQuery({
        queryKey: ['site', selectedSiteId],
        queryFn: () => sitesApi.get(selectedSiteId ?? -1),
        enabled: !!selectedSiteId
    });

    const { data: area } = useQuery({
        queryKey: ['area', selectedAreaId],
        queryFn: () => areasApi.get(selectedAreaId ?? -1),
        enabled: !!selectedAreaId
    });

    // Connect to MQTT on mount
    useEffect(() => {
        connectMqtt();
    }, [connectMqtt]);

    // Home › section › page › detail, in the user's language. It used to
    // capitalise the URL: "Oee-profiles".
    const getBreadcrumbs = () => {
        const path = location.pathname;
        const sections = navSections({
            isAdmin: isAdmin(),
            isGlobalAdmin: isGlobalAdmin(),
            isOrgScoped: isOrgScoped(),
        }, orgModules);
        const crumbs = [{ name: t('nav.home'), path: '/' }];
        if (path === '/') return crumbs;

        const item = findNavItem(sections, path);
        if (!item) {
            const own: Record<string, string> = { '/profile': 'nav.profile' };
            crumbs.push({ name: own[path] ? t(own[path]) : t('nav.page'), path });
            return crumbs;
        }
        const section = sections.find((s) => s.items.includes(item));
        if (section && section.items.length > 1) {
            crumbs.push({ name: t(section.key), path: item.path });
        }
        crumbs.push({ name: t(item.key), path: item.path });
        if (path !== item.path) {
            const rest = path.slice(item.path.length + 1).split('/');
            crumbs.push({
                name: rest[rest.length - 1] === 'edit' ? t('nav.editing') : t('nav.detail'),
                path,
            });
        }
        return crumbs;
    };

    const breadcrumbs = getBreadcrumbs();

    return (
        <header className="h-16 shrink-0 border-b bg-background px-3 md:px-6 flex items-center justify-between gap-2">
            <div className="flex items-center gap-3 md:gap-6 min-w-0">
                <button
                    type="button"
                    onClick={onMenuClick}
                    aria-label={t('nav.open_menu')}
                    className="md:hidden -ml-1 p-2 text-muted-foreground hover:text-foreground shrink-0"
                >
                    <Menu size={22} />
                </button>

                {/* Breadcrumbs. The full trail needs room it does not have on a
                    phone, so only the last one — where you are — survives there. */}
                <div className="flex items-center gap-2 text-sm text-muted-foreground md:mr-4 min-w-0">
                    {breadcrumbs.map((crumb, index) => (
                        <div key={`${index}-${crumb.path}`} className={cn(
                            "items-center gap-2",
                            index === breadcrumbs.length - 1 ? "flex min-w-0" : "hidden md:flex"
                        )}>
                            {index > 0 && <ChevronRight size={14} className="hidden md:block shrink-0" />}
                            <span
                                className={cn(
                                    "cursor-pointer hover:text-foreground transition-colors truncate",
                                    index === breadcrumbs.length - 1 && "font-semibold text-foreground"
                                )}
                                onClick={() => navigate(crumb.path)}
                            >
                                {crumb.name}
                            </span>
                        </div>
                    ))}
                </div>

                {/* Org switcher (global admin only): the selected org scopes
                    every page. Native select so it works on mobile too. */}
                {isGlobalAdmin() && orgs.length > 0 && (
                    <select
                        className="h-8 max-w-[140px] md:max-w-[200px] px-2 rounded-md border bg-background text-sm shrink-0"
                        value={selectedOrgId ?? ''}
                        onChange={(e) => {
                            const v = e.target.value ? parseInt(e.target.value) : null;
                            setSelectedOrgId(v);
                            setSelectedSiteId(null);
                            setSelectedAreaId(null);
                            navigate('/');
                        }}
                        aria-label={t('nav.select_org')}
                    >
                        <option value="">{t('nav.all_orgs')}</option>
                        {orgs.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
                    </select>
                )}

                {/* Context Indicator (Professional Badge) */}
                {(selectedOrgId || selectedSiteId || selectedAreaId) && (
                    <div className="hidden md:flex h-8 items-center rounded-full border bg-card pl-3 pr-1.5" title={t('nav.context')}>
                        <div className="flex items-center gap-1.5 text-sm">
                            {org && (
                                <div className="flex items-center gap-1">
                                    <Building2 size={13} className="text-muted-foreground" />
                                    <span className="font-medium">{org.name}</span>
                                </div>
                            )}

                            {site && (
                                <>
                                    <span className="text-muted-foreground/50">/</span>
                                    <div className="flex items-center gap-1">
                                        <Factory size={13} className="text-muted-foreground" />
                                        <span className="font-medium">{site.name}</span>
                                    </div>
                                </>
                            )}

                            {area && (
                                <>
                                    <span className="text-muted-foreground/50">/</span>
                                    <div className="flex items-center gap-1">
                                        <MapPin size={13} className="text-muted-foreground" />
                                        <span className="font-medium">{area.name}</span>
                                    </div>
                                </>
                            )}
                        </div>

                        {/* Org-scoped users cannot switch org — hide the clear button */}
                        {!isOrgScoped() && (
                            <button
                                onClick={clearSelection}
                                className="ml-2 grid size-6 place-items-center rounded-full hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                                aria-label={t('nav.clear_context')}
                                title={t('nav.clear_context')}
                            >
                                <X size={14} />
                            </button>
                        )}
                    </div>
                )}
            </div>

            <div className="flex items-center gap-4 shrink-0">
                {/* MQTT Connection Status.
                    On a phone the label costs more room than the header has —
                    it ran off the right edge — so only the light stays there,
                    with the words in its tooltip. */}
                <div
                    title={isMqttConnected ? t('nav.mqtt_connected_hint') : t('nav.mqtt_disconnected_hint')}
                    className={cn(
                        // Connected is the normal state: quiet. Only the loss is coloured.
                        "flex h-8 items-center gap-2 rounded-full border px-2.5 md:px-3 text-xs font-medium",
                        isMqttConnected
                            ? "bg-card text-muted-foreground"
                            : "border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/50 dark:text-red-400"
                    )}>
                    <span className={cn(
                        "size-2 rounded-full shrink-0",
                        isMqttConnected ? "bg-emerald-500" : "bg-red-500 animate-pulse"
                    )}></span>
                    <span className="hidden sm:inline">
                        {isMqttConnected ? t('nav.mqtt_connected') : t('nav.mqtt_disconnected')}
                    </span>
                </div>
            </div>
        </header>
    );
};

export default Header;
