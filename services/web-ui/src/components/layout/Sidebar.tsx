import { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
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
} from 'lucide-react';
import { Button } from "@/components/ui/button";
import { useAuthStore } from '@/stores/useAuthStore';
import { useThemeStore } from '@/stores/useThemeStore';

const Sidebar = () => {
    const location = useLocation();
    const navigate = useNavigate();
    const [collapsed, setCollapsed] = useState(false);
    const { user, logout, isAdmin } = useAuthStore();
    const { theme, toggleTheme } = useThemeStore();

    const navItems = [
        { name: 'Dashboard', path: '/', icon: LayoutDashboard },
        { name: 'Organizations', path: '/organizations', icon: Building2 },
        { name: 'Sites', path: '/sites', icon: Factory },
        { name: 'Areas', path: '/areas', icon: MapPin },
        { name: 'Gateways', path: '/gateways', icon: Cpu },
        { name: 'Tags', path: '/tags', icon: Tags },
        { name: 'Trend', path: '/trend', icon: TrendingUp },
        { name: 'Historian', path: '/history', icon: History },
        { name: 'Alarms', path: '/alarms', icon: Bell },
        { name: 'Recipes', path: '/recipes', icon: ChefHat },
        { name: 'i3X API',      path: '/i3x',          icon: Network },
    ];

    const adminNavItems = [
        { name: 'System', path: '/system', icon: Settings },
        { name: 'Diagnostics', path: '/diagnostics', icon: Activity },
        { name: 'Users', path: '/users', icon: Users },
        { name: 'MQTT Monitor', path: '/mqtt-monitor', icon: Radio },
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
                    <div className="p-1">
                        <img src={theme === 'dark' ? '/logo-dark.png' : '/logo-light.png'} alt="OpenEdge" className="h-10 w-auto object-contain" />
                    </div>
                ) : (
                    <div className="flex items-center gap-2 overflow-hidden whitespace-nowrap">
                        <img src={theme === 'dark' ? '/logo-dark.png' : '/logo-light.png'} alt="OpenEdge" className="h-10 w-auto object-contain" />
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

                {/* Admin Section */}
                {isAdmin() && (
                    <>
                        <div className={cn(
                            "my-3 border-t border-[hsl(var(--sidebar-border))]",
                            collapsed && "mx-3"
                        )} />
                        {!collapsed && (
                            <p className="px-4 text-[10px] text-[hsl(var(--sidebar-muted))] uppercase tracking-wider font-semibold mb-2">Admin</p>
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
                    {!collapsed && <span>{theme === 'dark' ? 'Light Mode' : 'Dark Mode'}</span>}
                </Button>

                {!collapsed && (
                    <div className="flex items-center justify-between px-2 py-1 text-xs text-[hsl(var(--sidebar-muted))]">
                        <span>Language</span>
                        <LanguageSwitch />
                    </div>
                )}

                {/* User Profile */}
                <div className={cn(
                    "flex items-center transition-all bg-[hsl(var(--sidebar-accent))]/50 clip-chamfer-sm",
                    collapsed ? "justify-center p-2" : "gap-3 p-3 border border-[hsl(var(--sidebar-border))]/50"
                )}>
                    <div className="h-9 w-9 min-w-[36px] clip-hex bg-primary flex items-center justify-center text-primary-foreground">
                        <User size={18} />
                    </div>
                    {!collapsed && (
                        <div className="flex-1 overflow-hidden">
                            <p className="text-sm font-bold truncate">{user?.username || 'User'}</p>
                            <div className="flex items-center gap-1.5">
                                <div className="w-2 h-2 clip-hex bg-green-500 animate-pulse" />
                                <p className="text-xs text-[hsl(var(--sidebar-muted))] truncate">{isAdmin() ? 'Admin' : 'User'}</p>
                            </div>
                        </div>
                    )}
                </div>

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
                    {!collapsed && <span>Logout</span>}
                </Button>
            </div>
        </div>
    );
};

export default Sidebar;
