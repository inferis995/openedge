import { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { cn } from '@/lib/utils';
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
} from 'lucide-react';
import { Button } from "@/components/ui/button";
import { useAuthStore } from '@/stores/useAuthStore';

const Sidebar = () => {
    const location = useLocation();
    const navigate = useNavigate();
    const [collapsed, setCollapsed] = useState(false);
    const { user, logout, isAdmin } = useAuthStore();

    const navItems = [
        { name: 'Dashboard', path: '/', icon: LayoutDashboard },
        { name: 'Organizations', path: '/organizations', icon: Building2 },
        { name: 'Sites', path: '/sites', icon: Factory },
        { name: 'Areas', path: '/areas', icon: MapPin },
        { name: 'Gateways', path: '/gateways', icon: Cpu },
        { name: 'Tags', path: '/tags', icon: Tags },
        { name: 'Trend', path: '/trend', icon: TrendingUp },
        { name: 'Historian', path: '/history', icon: History },
        { name: 'MQTT Monitor', path: '/mqtt-monitor', icon: Radio },
    ];

    const adminNavItems = [
        { name: 'System', path: '/system', icon: Settings },
        { name: 'Users', path: '/users', icon: Users },
    ];

    return (
        <div
            className={cn(
                "h-screen bg-slate-950 text-white flex flex-col border-r border-slate-800 transition-all duration-300 ease-in-out relative z-20",
                collapsed ? "w-20" : "w-64"
            )}
        >
            {/* Header / Logo */}
            <div className={cn(
                "p-4 border-b border-slate-800 flex items-center h-16 transition-all",
                collapsed ? "justify-center" : "justify-between"
            )}>
                {collapsed ? (
                    <div className="bg-blue-600/10 rounded-lg p-1 shadow-lg shadow-blue-900/20">
                        <img src="/logo.png" alt="OpenEdge" className="h-8 w-8 object-contain" />
                    </div>
                ) : (
                    <div className="flex items-center gap-3 overflow-hidden whitespace-nowrap">
                        <img src="/logo.png" alt="OpenEdge" className="h-10 w-10 object-contain rounded-md" />
                        <div>
                            <h1 className="text-lg font-bold tracking-tight text-white">OpenEdge</h1>
                            <p className="text-[10px] text-slate-400 font-medium uppercase tracking-wider">Professional</p>
                        </div>
                    </div>
                )}
            </div>

            {/* Toggle Button (Absolute position) */}
            <Button
                variant="ghost"
                size="icon"
                className="absolute -right-3 top-20 h-6 w-6 rounded-full bg-slate-800 border border-slate-700 text-slate-400 hover:text-white hover:bg-slate-700 shadow-md z-30 hidden md:flex"
                onClick={() => setCollapsed(!collapsed)}
            >
                {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
            </Button>

            {/* Navigation */}
            <nav className="flex-1 p-3 space-y-1.5 overflow-y-auto overflow-x-hidden scrollbar-thin scrollbar-thumb-slate-800">
                {navItems.map((item) => {
                    const isActive = location.pathname === item.path;

                    return (
                        <Link
                            key={item.path}
                            to={item.path}
                            title={collapsed ? item.name : undefined}
                            className={cn(
                                "flex items-center rounded-lg text-sm font-medium transition-all group",
                                collapsed ? "justify-center w-12 h-12 mx-auto px-0" : "px-4 py-3 gap-3 w-full",
                                isActive
                                    ? "bg-blue-600 text-white shadow-md shadow-blue-900/20"
                                    : "text-slate-400 hover:bg-slate-800 hover:text-white hover:shadow-sm"
                            )}
                        >
                            <item.icon
                                size={collapsed ? 24 : 20}
                                className={cn(
                                    "transition-transform duration-200",
                                    collapsed && !isActive && "group-hover:scale-110",
                                    isActive && "animate-pulse-slow"
                                )}
                            />
                            {!collapsed && (
                                <span className="truncate">{item.name}</span>
                            )}
                            {!collapsed && isActive && (
                                <div className="ml-auto w-1.5 h-1.5 rounded-full bg-white animate-pulse" />
                            )}
                        </Link>
                    );
                })}

                {/* Admin Section */}
                {isAdmin() && (
                    <>
                        <div className={cn(
                            "my-3 border-t border-slate-800",
                            collapsed && "mx-3"
                        )} />
                        {!collapsed && (
                            <p className="px-4 text-[10px] text-slate-500 uppercase tracking-wider font-semibold mb-2">Admin</p>
                        )}
                        {adminNavItems.map((item) => {
                            const isActive = location.pathname === item.path;

                            return (
                                <Link
                                    key={item.path}
                                    to={item.path}
                                    title={collapsed ? item.name : undefined}
                                    className={cn(
                                        "flex items-center rounded-lg text-sm font-medium transition-all group",
                                        collapsed ? "justify-center w-12 h-12 mx-auto px-0" : "px-4 py-3 gap-3 w-full",
                                        isActive
                                            ? "bg-blue-600 text-white shadow-md shadow-blue-900/20"
                                            : "text-slate-400 hover:bg-slate-800 hover:text-white hover:shadow-sm"
                                    )}
                                >
                                    <item.icon
                                        size={collapsed ? 24 : 20}
                                        className={cn(
                                            "transition-transform duration-200",
                                            collapsed && !isActive && "group-hover:scale-110",
                                            isActive && "animate-pulse-slow"
                                        )}
                                    />
                                    {!collapsed && (
                                        <span className="truncate">{item.name}</span>
                                    )}
                                    {!collapsed && isActive && (
                                        <div className="ml-auto w-1.5 h-1.5 rounded-full bg-white animate-pulse" />
                                    )}
                                </Link>
                            );
                        })}
                    </>
                )}
            </nav>

            {/* Footer / User Profile */}
            <div className="p-4 border-t border-slate-800 space-y-3">
                <div className={cn(
                    "flex items-center transition-all bg-slate-900/50 rounded-lg",
                    collapsed ? "justify-center p-2" : "gap-3 p-3 border border-slate-800/50"
                )}>
                    <div className="h-9 w-9 min-w-[36px] rounded-lg bg-gradient-to-br from-blue-500 to-indigo-600 flex items-center justify-center text-white shadow-md">
                        <User size={18} />
                    </div>
                    {!collapsed && (
                        <div className="flex-1 overflow-hidden">
                            <p className="text-sm font-bold truncate">{user?.username || 'User'}</p>
                            <div className="flex items-center gap-1.5">
                                <div className="w-2 h-2 rounded-full bg-green-500 animate-pulse" />
                                <p className="text-xs text-slate-400 truncate">{isAdmin() ? 'Admin' : 'User'}</p>
                            </div>
                        </div>
                    )}
                </div>
                {/* Logout Button */}
                <Button
                    variant="ghost"
                    size={collapsed ? "icon" : "sm"}
                    className={cn(
                        "text-red-400 hover:text-red-300 hover:bg-red-900/20 transition-colors",
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
