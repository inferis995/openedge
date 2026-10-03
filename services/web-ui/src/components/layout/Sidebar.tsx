import { useEffect, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';
import LanguageSwitch from '@/components/layout/LanguageSwitch';
import {
    ChevronDown,
    ChevronLeft,
    ChevronRight,
    LogOut,
    Moon,
    Search,
    Sun,
    User,
    X,
} from 'lucide-react';
import { isActivePath, navSections, openQuickSearch, type NavItem } from '@/components/layout/navigation';
import { Button } from "@/components/ui/button";
import { useAuthStore } from '@/stores/useAuthStore';
import { useThemeStore } from '@/stores/useThemeStore';

const FOLDED_KEY = 'openedge.nav.folded';

interface SidebarProps {
    /** Open state of the mobile drawer. Ignored from md upwards, where the
     *  sidebar is part of the layout rather than laid over it. */
    mobileOpen?: boolean;
    onMobileClose?: () => void;
}

const Sidebar = ({ mobileOpen = false, onMobileClose }: SidebarProps) => {
    const location = useLocation();
    const navigate = useNavigate();
    const { t } = useTranslation();
    // Collapsing is a desktop affordance: on a phone the sidebar is either
    // over the page or out of the way, and a 20px-wide rail is neither.
    const [collapsed, setCollapsed] = useState(false);

    // Close the drawer when the route changes. Without this, tapping a link
    // navigates underneath a panel that stays open over the page you asked for.
    useEffect(() => {
        onMobileClose?.();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [location.pathname]);
    const { user, logout, isAdmin, isGlobalAdmin, isOrgScoped } = useAuthStore();
    const { theme, toggleTheme } = useThemeStore();

    const sections = navSections({
        isAdmin: isAdmin(),
        isGlobalAdmin: isGlobalAdmin(),
        isOrgScoped: isOrgScoped(),
    });

    // Sections the user folded away, remembered on this browser. A section
    // holding the current page is never folded: the user would lose their
    // place in the menu.
    const [folded, setFolded] = useState<Record<string, boolean>>(() => {
        try {
            return JSON.parse(localStorage.getItem(FOLDED_KEY) || '{}') as Record<string, boolean>;
        } catch {
            return {};
        }
    });
    const toggleSection = (key: string) => {
        setFolded((prev) => {
            const next = { ...prev, [key]: !prev[key] };
            try {
                localStorage.setItem(FOLDED_KEY, JSON.stringify(next));
            } catch {
                // Private window or blocked storage: folding still works until reload.
            }
            return next;
        });
    };

    const renderItem = (item: NavItem) => {
        const isActive = isActivePath(item.path, location.pathname);
        const label = t(item.key);
        return (
            <Link
                key={item.path}
                to={item.path}
                title={collapsed ? label : undefined}
                aria-current={isActive ? 'page' : undefined}
                className={cn(
                    "flex items-center clip-chamfer-sm text-sm font-medium transition-all group",
                    collapsed ? "justify-center w-12 h-12 mx-auto px-0" : "px-4 py-2.5 gap-3 w-full",
                    isActive
                        ? "bg-primary text-primary-foreground shadow-md"
                        : "text-[hsl(var(--sidebar-muted))] hover:bg-[hsl(var(--sidebar-accent))] hover:text-[hsl(var(--sidebar-fg))]"
                )}
            >
                <item.icon
                    size={collapsed ? 24 : 18}
                    className={cn(
                        "shrink-0 transition-transform duration-200",
                        collapsed && !isActive && "group-hover:scale-110"
                    )}
                />
                {!collapsed && <span className="truncate">{label}</span>}
            </Link>
        );
    };

    return (
        <>
            {/* Backdrop. Only under md, and only while the drawer is open —
                otherwise it would swallow every tap on the desktop layout. */}
            <div
                aria-hidden={!mobileOpen}
                onClick={onMobileClose}
                className={cn(
                    "fixed inset-0 z-55 bg-black/60 md:hidden transition-opacity duration-300",
                    mobileOpen ? "opacity-100" : "pointer-events-none opacity-0"
                )}
            />
            <div
                className={cn(
                    "h-screen bg-[hsl(var(--sidebar-bg))] text-[hsl(var(--sidebar-fg))] flex flex-col border-r border-[hsl(var(--sidebar-border))] transition-transform duration-300 ease-in-out",
                    // Phone: a panel laid over the page, off-screen until asked for.
                    // z-60: above the cookie banner (z-50), which otherwise covers
                    // the drawer's footer and puts logout and language out of reach
                    // on a first visit.
                    "fixed inset-y-0 left-0 z-60 w-72 max-w-[85vw]",
                    mobileOpen ? "translate-x-0" : "-translate-x-full",
                    // Desktop: part of the layout again, and it may collapse.
                    "md:static md:z-20 md:translate-x-0 md:max-w-none md:transition-all",
                    collapsed ? "md:w-20" : "md:w-64"
                )}
            >
                {/* Closing the drawer needs a target on the drawer itself: the
                    backdrop is not discoverable, and the hamburger is behind it. */}
                <button
                    type="button"
                    onClick={onMobileClose}
                    aria-label={t('nav.close_menu')}
                    className="md:hidden absolute right-3 top-4 p-2 text-[hsl(var(--sidebar-muted))] hover:text-[hsl(var(--sidebar-fg))]"
                >
                    <X size={20} />
                </button>
            {/* Header / Logo */}
            <div className={cn(
                "p-4 border-b border-[hsl(var(--sidebar-border))] flex items-center h-16 transition-all",
                collapsed ? "justify-center" : "justify-between"
            )}>
                {collapsed ? (
                    <img src="/avatar.png" alt="OpenEdge" className="h-10 sm:h-8 w-10 sm:w-8 rounded-lg object-cover" />
                ) : (
                    <div className="flex items-center gap-2 overflow-hidden whitespace-nowrap">
                        <img src="/avatar.png" alt="OpenEdge" className="h-10 sm:h-8 w-10 sm:w-8 rounded-lg object-cover" />
                        <span className="text-lg font-black tracking-tight text-[#CCFF00]" style={{ WebkitTextStroke: '1px #000', paintOrder: 'stroke fill' }}>OpenEdge</span>
                    </div>
                )}
            </div>

            {/* Toggle Button (Absolute position) */}
            <Button
                variant="ghost"
                size="icon"
                className="absolute -right-3 top-20 h-9 sm:h-6 w-9 sm:w-6 clip-hex bg-[hsl(var(--sidebar-accent))] border border-[hsl(var(--sidebar-border))] text-[hsl(var(--sidebar-muted))] hover:text-[hsl(var(--sidebar-fg))] hover:bg-[hsl(var(--sidebar-accent))]/80 shadow-md z-30 hidden md:flex items-center justify-center p-0"
                onClick={() => setCollapsed(!collapsed)}
            >
                {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
            </Button>

            {/* Quick search: every page by name, from anywhere, with Ctrl+K. */}
            <div className="px-3 pt-3">
                <button
                    type="button"
                    onClick={openQuickSearch}
                    title={collapsed ? t('nav.search_placeholder') : undefined}
                    className={cn(
                        "flex items-center text-sm clip-chamfer-sm border border-[hsl(var(--sidebar-border))] text-[hsl(var(--sidebar-muted))] hover:text-[hsl(var(--sidebar-fg))] hover:bg-[hsl(var(--sidebar-accent))] transition-colors",
                        collapsed ? "justify-center w-12 h-10 mx-auto" : "w-full gap-2 px-3 py-2"
                    )}
                >
                    <Search size={16} className="shrink-0" />
                    {!collapsed && (
                        <>
                            <span className="truncate">{t('nav.search_placeholder')}</span>
                            <kbd className="ml-auto hidden md:inline whitespace-nowrap text-[10px] leading-5 border border-[hsl(var(--sidebar-border))] px-1.5 py-0.5 rounded">Ctrl K</kbd>
                        </>
                    )}
                </button>
            </div>

            {/* Navigation */}
            <nav className="flex-1 p-3 overflow-y-auto overflow-x-hidden scrollbar-thin scrollbar-thumb-[hsl(var(--sidebar-accent))]">
                {sections.map((section, index) => {
                    const holdsCurrent = section.items.some((i) => isActivePath(i.path, location.pathname));
                    const open = collapsed || holdsCurrent || !folded[section.key];
                    // The overview is a single entry; a heading over it is noise.
                    const titled = section.items.length > 1;
                    return (
                        <div key={section.key} className={cn(index > 0 && "mt-3")}>
                            {collapsed && index > 0 && (
                                <div className="mx-3 mb-3 border-t border-[hsl(var(--sidebar-border))]" />
                            )}
                            {!collapsed && titled && (
                                <button
                                    type="button"
                                    onClick={() => toggleSection(section.key)}
                                    disabled={holdsCurrent}
                                    aria-expanded={open}
                                    className="w-full flex items-center justify-between px-4 py-1.5 text-[10px] uppercase tracking-wider font-semibold text-[hsl(var(--sidebar-muted))] hover:text-[hsl(var(--sidebar-fg))] disabled:cursor-default disabled:hover:text-[hsl(var(--sidebar-muted))]"
                                >
                                    <span>{t(section.key)}</span>
                                    {!holdsCurrent && (
                                        <ChevronDown size={12} className={cn("transition-transform", !open && "-rotate-90")} />
                                    )}
                                </button>
                            )}
                            {open && <div className="space-y-0.5">{section.items.map(renderItem)}</div>}
                        </div>
                    );
                })}
            </nav>

            {/* Footer. Compact on purpose: at 900px of height it used to take
                half the sidebar, and the menu above it showed nine entries. */}
            <div className={cn("border-t border-[hsl(var(--sidebar-border))]", collapsed ? "p-2 space-y-2" : "p-3 space-y-2")}>
                <div className={cn("flex items-center", collapsed ? "flex-col gap-2" : "gap-2")}>
                    {/* User — click to open the profile page */}
                    <Link
                        to="/profile"
                        title={collapsed ? (user?.username || t('nav.user_role')) : t('nav.profile')}
                        className={cn(
                            "flex items-center transition-all bg-[hsl(var(--sidebar-accent))]/50 clip-chamfer-sm hover:bg-[hsl(var(--sidebar-accent))] cursor-pointer min-w-0",
                            collapsed ? "justify-center p-2" : "flex-1 gap-2 p-2"
                        )}
                    >
                        <div className="h-8 w-8 min-w-[32px] clip-hex bg-primary flex items-center justify-center text-primary-foreground">
                            <User size={16} />
                        </div>
                        {!collapsed && (
                            <div className="flex-1 overflow-hidden">
                                <p className="text-sm font-bold truncate leading-tight">{user?.username || t('nav.user_role')}</p>
                                <p className="text-xs text-[hsl(var(--sidebar-muted))] truncate leading-tight">
                                    {isAdmin() ? t('nav.admin_role') : t('nav.user_role')}
                                </p>
                            </div>
                        )}
                    </Link>
                    <Button
                        variant="ghost"
                        size="icon"
                        title={t('nav.logout')}
                        aria-label={t('nav.logout')}
                        className="h-10 w-10 shrink-0 text-destructive hover:text-destructive/80 hover:bg-destructive/10 clip-chamfer-sm"
                        onClick={() => {
                            logout();
                            navigate('/login');
                        }}
                    >
                        <LogOut size={18} />
                    </Button>
                </div>

                <div className={cn("flex items-center", collapsed ? "flex-col gap-2" : "gap-2")}>
                    <Button
                        variant="ghost"
                        size="icon"
                        title={theme === 'dark' ? t('nav.light_mode') : t('nav.dark_mode')}
                        aria-label={theme === 'dark' ? t('nav.light_mode') : t('nav.dark_mode')}
                        className="h-9 w-9 shrink-0 text-[hsl(var(--sidebar-muted))] hover:text-[hsl(var(--sidebar-fg))] hover:bg-[hsl(var(--sidebar-accent))] clip-chamfer-sm"
                        onClick={toggleTheme}
                    >
                        {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
                    </Button>
                    {!collapsed && (
                        <>
                            <LanguageSwitch />
                            {/* Vertical padding makes them reachable on a phone; the text stays small. */}
                            <div className="ml-auto flex gap-2 text-[10px] text-[hsl(var(--sidebar-muted))]">
                                <a href="mailto:support@openedge.io" className="hover:underline py-2">{t('nav.support')}</a>
                                <Link to="/privacy" className="hover:underline py-2">{t('nav.privacy')}</Link>
                                <Link to="/terms" className="hover:underline py-2">{t('nav.terms')}</Link>
                            </div>
                        </>
                    )}
                </div>
            </div>
            </div>
        </>
    );
};

export default Sidebar;
