import { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';
import LanguageSwitch from '@/components/layout/LanguageSwitch';
import {
    LayoutDashboard,
    Building2,
    Factory,
    MapPin,
    Cpu,
    Tags,
    Settings,
    TrendingUp,
    Radio,
    ChevronLeft,
    ChevronRight,
    History,
    LogOut,
    User,
    Users,
    Moon,
    Sun,
    Bell,
    Network,
    ChefHat,
    Activity,
    FileText,
    Clock,
    Wrench,
    Target,
    Gauge,
    Shield,
    LayoutTemplate,
    Server,
    Lock,
    PackageOpen,
    ShieldCheck,
    ScanSearch,
    TriangleAlert,
    ClipboardList,
    TrendingDown,
    AlertTriangle,
    Boxes,
    Layers,
} from 'lucide-react';
import { Button } from "@/components/ui/button";
import { useAuthStore } from '@/stores/useAuthStore';
import { useThemeStore } from '@/stores/useThemeStore';

const Sidebar = () => {
    const location = useLocation();
    const navigate = useNavigate();
    const { t } = useTranslation();
    const [collapsed, setCollapsed] = useState(false);
    const { user, logout, isAdmin, isGlobalAdmin, isOrgScoped } = useAuthStore();
    const { theme, toggleTheme } = useThemeStore();

    // Etichette via i18n: lo switch IT/EN nel footer cambia tutto in tempo
    // reale. Le chiavi vivono in src/i18n/locales/{en,it}.json sotto `nav.*`.
    // Core nav — everyone with a valid session sees this.
    const navItems = [
        { name: t('nav.dashboard'), path: '/', icon: LayoutDashboard },
        // Global admins see the full org roster; org-scoped users see only
        // their own org (same page, API already scopes the data).
        ...(isGlobalAdmin()
            ? [{ name: t('nav.organizations'), path: '/organizations', icon: Building2 }]
            : isOrgScoped() && isAdmin()
                // Org admins get a "My Organization" shortcut to manage
                // infrastructure, invite users, and API keys.
                ? [{ name: 'My Organization', path: '/organizations', icon: Building2 }]
                : []
        ),
        { name: t('nav.sites'), path: '/sites', icon: Factory },
        { name: t('nav.areas'), path: '/areas', icon: MapPin },
        { name: t('nav.gateways'), path: '/gateways', icon: Cpu },
        { name: t('nav.tags'), path: '/tags', icon: Tags },
        { name: t('nav.trend'), path: '/trend', icon: TrendingUp },
        { name: t('nav.historian'), path: '/history', icon: History },
        { name: t('nav.alarms'), path: '/alarms', icon: Bell },
        { name: t('nav.recipes'), path: '/recipes', icon: ChefHat },
        { name: t('nav.reports'), path: '/reports', icon: FileText },
        { name: t('nav.kpis'), path: '/kpis', icon: Target },
        { name: t('nav.oee_profiles'), path: '/oee-profiles', icon: Gauge },
        { name: t('nav.shifts'), path: '/shifts', icon: Clock },
        { name: t('nav.maintenance'), path: '/maintenance', icon: Wrench },
        { name: t('nav.i3x'), path: '/i3x', icon: Network },
        { name: 'Tipi (UDT)', path: '/udt/types', icon: Boxes },
        { name: 'Istanze', path: '/udt/instances', icon: Layers },
        { name: 'Sinottici', path: '/synoptics', icon: LayoutTemplate },
    ];

    // OT Compliance section — visible to all authenticated users
    const complianceNavItems = [
        { name: 'Inventario Asset', path: '/compliance/assets', icon: ScanSearch },
        { name: 'Postura Rischio', path: '/compliance/risk', icon: TrendingDown },
        { name: 'Monitor Minacce', path: '/compliance/threats', icon: TriangleAlert },
        { name: 'Report', path: '/compliance/reports', icon: ClipboardList },
        { name: 'CSIRT Art.23', path: '/compliance/csirt', icon: AlertTriangle },
        { name: 'Vendor Risk', path: '/compliance/vendors', icon: Building2 },
    ];

    // Admin section — global admins see System + Diagnostics + Users + MQTT monitor + Audit.
    // Org admins see only Users (to manage their org's members).
    const adminNavItems = [
        ...(isGlobalAdmin()
            ? [
                { name: t('nav.system'), path: '/system', icon: Settings },
                { name: t('nav.diagnostics'), path: '/diagnostics', icon: Activity },
                { name: t('nav.mqtt_monitor'), path: '/mqtt-monitor', icon: Radio },
                { name: t('nav.audit_log'), path: '/audit', icon: Shield },
                { name: 'Fleet', path: '/fleet', icon: Network },
                { name: 'Releases', path: '/releases', icon: PackageOpen },
                { name: 'Security Center', path: '/security', icon: Lock },
                { name: 'Infrastruttura', path: '/infrastructure', icon: Server },
            ]
            : []
        ),
        { name: t('nav.users'), path: '/users', icon: Users },
    ];

    return (
        <div
            className={cn(
                "h-screen bg-[hsl(var(--sidebar-bg))] text-[hsl(var(--sidebar-fg))] flex flex-col border-r border-[hsl(var(--sidebar-border))] transition-all duration-300 ease-in-out relative z-20",
                collapsed ? "w-20" : "w-64"
            )}
        >
            {/* Header / Logo */}
            <div className={cn(
                "p-4 border-b border-[hsl(var(--sidebar-border))] flex items-center h-16 transition-all",
                collapsed ? "justify-center" : "justify-between"
            )}>
                {collapsed ? (
                    <img src="/avatar.png" alt="OpenEdge" className="h-8 w-8 rounded-lg object-cover" />
                ) : (
                    <div className="flex items-center gap-2 overflow-hidden whitespace-nowrap">
                        <img src="/avatar.png" alt="OpenEdge" className="h-8 w-8 rounded-lg object-cover" />
                        <span className="text-lg font-black tracking-tight text-[#CCFF00]" style={{ WebkitTextStroke: '1px #000', paintOrder: 'stroke fill' }}>OpenEdge</span>
                    </div>
                )}
            </div>

            {/* Toggle Button (Absolute position) */}
            <Button
                variant="ghost"
                size="icon"
                className="absolute -right-3 top-20 h-6 w-6 clip-hex bg-[hsl(var(--sidebar-accent))] border border-[hsl(var(--sidebar-border))] text-[hsl(var(--sidebar-muted))] hover:text-[hsl(var(--sidebar-fg))] hover:bg-[hsl(var(--sidebar-accent))]/80 shadow-md z-30 hidden md:flex items-center justify-center p-0"
                onClick={() => setCollapsed(!collapsed)}
            >
                {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
            </Button>

            {/* Navigation */}
            <nav className="flex-1 p-3 space-y-1 overflow-y-auto overflow-x-hidden scrollbar-thin scrollbar-thumb-[hsl(var(--sidebar-accent))]">
                {navItems.map((item) => {
                    const isActive = location.pathname === item.path;

                    return (
                        <Link
                            key={item.path}
                            to={item.path}
                            title={collapsed ? item.name : undefined}
                            className={cn(
                                "flex items-center clip-chamfer-sm text-sm font-medium transition-all group",
                                collapsed ? "justify-center w-12 h-12 mx-auto px-0" : "px-4 py-3 gap-3 w-full",
                                isActive
                                    ? "bg-primary text-primary-foreground shadow-md"
                                    : "text-[hsl(var(--sidebar-muted))] hover:bg-[hsl(var(--sidebar-accent))] hover:text-[hsl(var(--sidebar-fg))]"
                            )}
                        >
                            <item.icon
                                size={collapsed ? 24 : 20}
                                className={cn(
                                    "transition-transform duration-200",
                                    collapsed && !isActive && "group-hover:scale-110"
                                )}
                            />
                            {!collapsed && (
                                <span className="truncate">{item.name}</span>
                            )}
                            {!collapsed && isActive && (
                                <div className="ml-auto w-2 h-2 clip-hex bg-primary-foreground animate-pulse" />
                            )}
                        </Link>
                    );
                })}

                {/* OT Compliance Section */}
                <div className={cn(
                    "my-3 border-t border-[hsl(var(--sidebar-border))]",
                    collapsed && "mx-3"
                )} />
                {!collapsed && (
                    <p className="px-4 text-[10px] text-[hsl(var(--sidebar-muted))] uppercase tracking-wider font-semibold mb-2 flex items-center gap-1.5">
                        <ShieldCheck size={12} /> OT Compliance
                    </p>
                )}
                {complianceNavItems.map((item) => {
                    const isActive = location.pathname === item.path || location.pathname.startsWith(item.path);

                    return (
                        <Link
                            key={item.path}
                            to={item.path}
                            title={collapsed ? item.name : undefined}
                            className={cn(
                                "flex items-center clip-chamfer-sm text-sm font-medium transition-all group",
                                collapsed ? "justify-center w-12 h-12 mx-auto px-0" : "px-4 py-3 gap-3 w-full",
                                isActive
                                    ? "bg-primary text-primary-foreground shadow-md"
                                    : "text-[hsl(var(--sidebar-muted))] hover:bg-[hsl(var(--sidebar-accent))] hover:text-[hsl(var(--sidebar-fg))]"
                            )}
                        >
                            <item.icon
                                size={collapsed ? 24 : 20}
                                className={cn(
                                    "transition-transform duration-200",
                                    collapsed && !isActive && "group-hover:scale-110"
                                )}
                            />
                            {!collapsed && (
                                <span className="truncate">{item.name}</span>
                            )}
                            {!collapsed && isActive && (
                                <div className="ml-auto w-2 h-2 clip-hex bg-primary-foreground animate-pulse" />
                            )}
                        </Link>
                    );
                })}

                {/* Admin Section */}
                {isAdmin() && (
                    <>
                        <div className={cn(
                            "my-3 border-t border-[hsl(var(--sidebar-border))]",
                            collapsed && "mx-3"
                        )} />
                        {!collapsed && (
                            <p className="px-4 text-[10px] text-[hsl(var(--sidebar-muted))] uppercase tracking-wider font-semibold mb-2">{t('nav.admin_section')}</p>
                        )}
                        {adminNavItems.map((item) => {
                            const isActive = location.pathname === item.path;

                            return (
                                <Link
                                    key={item.path}
                                    to={item.path}
                                    title={collapsed ? item.name : undefined}
                                    className={cn(
                                        "flex items-center clip-chamfer-sm text-sm font-medium transition-all group",
                                        collapsed ? "justify-center w-12 h-12 mx-auto px-0" : "px-4 py-3 gap-3 w-full",
                                        isActive
                                            ? "bg-primary text-primary-foreground shadow-md"
                                            : "text-[hsl(var(--sidebar-muted))] hover:bg-[hsl(var(--sidebar-accent))] hover:text-[hsl(var(--sidebar-fg))]"
                                    )}
                                >
                                    <item.icon
                                        size={collapsed ? 24 : 20}
                                        className={cn(
                                            "transition-transform duration-200",
                                            collapsed && !isActive && "group-hover:scale-110"
                                        )}
                                    />
                                    {!collapsed && (
                                        <span className="truncate">{item.name}</span>
                                    )}
                                    {!collapsed && isActive && (
                                        <div className="ml-auto w-2 h-2 clip-hex bg-primary-foreground animate-pulse" />
                                    )}
                                </Link>
                            );
                        })}
                    </>
                )}
            </nav>

            {/* Footer / User Profile & Theme Toggle */}
            <div className="p-4 border-t border-[hsl(var(--sidebar-border))] space-y-3">
                {/* Theme Toggle */}
                <Button
                    variant="ghost"
                    size={collapsed ? "icon" : "sm"}
                    className={cn(
                        "text-[hsl(var(--sidebar-muted))] hover:text-[hsl(var(--sidebar-fg))] hover:bg-[hsl(var(--sidebar-accent))] transition-colors clip-chamfer-sm",
                        collapsed ? "w-12 h-10 mx-auto" : "w-full justify-start gap-2"
                    )}
                    onClick={toggleTheme}
                >
                    {theme === 'dark' ? <Sun size={18} /> : <Moon size={18} />}
                    {!collapsed && <span>{theme === 'dark' ? t('nav.light_mode') : t('nav.dark_mode')}</span>}
                </Button>

                {!collapsed && (
                    <div className="flex items-center justify-between px-2 py-1 text-xs text-[hsl(var(--sidebar-muted))]">
                        <span>{t('nav.language_label')}</span>
                        <LanguageSwitch />
                    </div>
                )}

                {/* User Profile — click to open profile/settings page */}
                <Link to="/profile" className={cn(
                    "flex items-center transition-all bg-[hsl(var(--sidebar-accent))]/50 clip-chamfer-sm hover:bg-[hsl(var(--sidebar-accent))] cursor-pointer",
                    collapsed ? "justify-center p-2" : "gap-3 p-3 border border-[hsl(var(--sidebar-border))]/50"
                )}>
                    <div className="h-9 w-9 min-w-[36px] clip-hex bg-primary flex items-center justify-center text-primary-foreground">
                        <User size={18} />
                    </div>
                    {!collapsed && (
                        <div className="flex-1 overflow-hidden">
                            <p className="text-sm font-bold truncate">{user?.username || t('nav.user_role')}</p>
                            <div className="flex items-center gap-1.5">
                                <div className="w-2 h-2 clip-hex bg-green-500 animate-pulse" />
                                <p className="text-xs text-[hsl(var(--sidebar-muted))] truncate">
                                    {isAdmin() ? t('nav.admin_role') : t('nav.user_role')}
                                </p>
                            </div>
                        </div>
                    )}
                </Link>

                {/* Support link */}
                {!collapsed && (
                    <div className="px-1 pb-1 flex gap-3 text-[10px] text-[hsl(var(--sidebar-muted))]">
                        <a href="mailto:support@openedge.io" className="hover:underline">Supporto</a>
                        <Link to="/privacy" className="hover:underline">Privacy</Link>
                        <Link to="/terms" className="hover:underline">Termini</Link>
                    </div>
                )}

                {/* Logout Button */}
                <Button
                    variant="ghost"
                    size={collapsed ? "icon" : "sm"}
                    className={cn(
                        "text-destructive hover:text-destructive/80 hover:bg-destructive/10 transition-colors clip-chamfer-sm",
                        collapsed ? "w-12 h-10 mx-auto" : "w-full justify-start gap-2"
                    )}
                    onClick={() => {
                        logout();
                        navigate('/login');
                    }}
                >
                    <LogOut size={18} />
                    {!collapsed && <span>{t('nav.logout')}</span>}
                </Button>
            </div>
        </div>
    );
};

export default Sidebar;
