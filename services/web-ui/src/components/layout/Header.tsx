import { useNavigate, useLocation } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Bell, RefreshCw, ChevronRight } from 'lucide-react';
import { cn } from '@/lib/utils';

const Header = () => {
    const navigate = useNavigate();
    const location = useLocation();

    // Mock active alarms count (will be real later)
    const activeAlarms = 3;

    const getBreadcrumbs = () => {
        const path = location.pathname;
        const parts = path.split('/').filter(Boolean);

        // Simple breadcrumb logic based on path
        // In a real app, we would fetch names based on IDs from store
        const crumbs = [
            { name: 'Home', path: '/' },
            ...parts.map((part, index) => {
                const url = `/${parts.slice(0, index + 1).join('/')}`;
                return { name: part.charAt(0).toUpperCase() + part.slice(1), path: url };
            })
        ];
        return crumbs;
    };

    const breadcrumbs = getBreadcrumbs();

    return (
        <header className="h-16 border-b bg-background px-6 flex items-center justify-between">
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
                {breadcrumbs.map((crumb, index) => (
                    <div key={crumb.path} className="flex items-center gap-2">
                        {index > 0 && <ChevronRight size={14} />}
                        <span
                            className={cn(
                                "cursor-pointer hover:text-foreground transition-colors",
                                index === breadcrumbs.length - 1 && "font-semibold text-foreground"
                            )}
                            onClick={() => navigate(crumb.path)}
                        >
                            {crumb.name}
                        </span>
                    </div>
                ))}
            </div>

            <div className="flex items-center gap-4">
                <div className="flex items-center gap-2 px-3 py-1.5 bg-secondary/50 rounded-md text-xs font-medium">
                    <span className="w-2 h-2 rounded-full bg-green-500 animate-pulse"></span>
                    MQTT Connected
                </div>

                <Button variant="ghost" size="icon" className="relative" onClick={() => navigate('/alarms')}>
                    <Bell size={20} />
                    {activeAlarms > 0 && (
                        <span className="absolute top-2 right-2 h-2 w-2 rounded-full bg-red-500" />
                    )}
                </Button>

                <Button variant="outline" size="sm" className="gap-2">
                    <RefreshCw size={14} />
                    Reload Config
                </Button>
            </div>
        </header>
    );
};

export default Header;
