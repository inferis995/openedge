import { Link, useLocation } from 'react-router-dom';
import { cn } from '@/lib/utils';
import {
    LayoutDashboard,
    Building2,
    Factory,
    MapPin,
    Cpu,
    Tags,
    Bell,
    Settings,
    TrendingUp
} from 'lucide-react';

const Sidebar = () => {
    const location = useLocation();

    const navItems = [
        { name: 'Dashboard', path: '/', icon: LayoutDashboard },
        { name: 'Organizations', path: '/organizations', icon: Building2 },
        { name: 'Sites', path: '/sites', icon: Factory },
        { name: 'Areas', path: '/areas', icon: MapPin },
        { name: 'Gateways', path: '/gateways', icon: Cpu },
        { name: 'Tags', path: '/tags', icon: Tags },
        { name: 'Trend', path: '/trend', icon: TrendingUp },
        { name: 'Alarms', path: '/alarms', icon: Bell },
        { name: 'System', path: '/system', icon: Settings },
    ];

    return (
        <div className="h-screen w-64 bg-slate-900 text-white flex flex-col border-r border-slate-800">
            <div className="p-6 border-b border-slate-800">
                <h1 className="text-xl font-bold flex items-center gap-2">
                    <Cpu className="text-blue-500" />
                    <span>Edge Config</span>
                </h1>
                <p className="text-xs text-slate-400 mt-1">System Integrator Tool</p>
            </div>

            <nav className="flex-1 p-4 space-y-1">
                {navItems.map((item) => {
                    const isActive = location.pathname === item.path;
                    return (
                        <Link
                            key={item.path}
                            to={item.path}
                            className={cn(
                                "flex items-center gap-3 px-4 py-3 rounded-lg text-sm font-medium transition-colors",
                                isActive
                                    ? "bg-blue-600 text-white"
                                    : "text-slate-400 hover:bg-slate-800 hover:text-white"
                            )}
                        >
                            <item.icon size={20} />
                            {item.name}
                        </Link>
                    );
                })}
            </nav>

            <div className="p-4 border-t border-slate-800">
                <div className="flex items-center gap-3 px-4 py-3">
                    <div className="h-8 w-8 rounded-full bg-blue-500 flex items-center justify-center text-xs font-bold">
                        SI
                    </div>
                    <div>
                        <p className="text-sm font-medium">System Admin</p>
                        <p className="text-xs text-slate-500">Online</p>
                    </div>
                </div>
            </div>
        </div>
    );
};

export default Sidebar;
