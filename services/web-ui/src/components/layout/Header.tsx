import { useNavigate, useLocation } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Bell, RefreshCw, ChevronRight } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useAlarmStore } from '@/stores/useAlarmStore';
import { useEffect } from 'react';

const Header = () => {
    const navigate = useNavigate();
    const location = useLocation();

    // Get real-time alarm state from store
    const { activeAlarmCount, isMqttConnected, connectMqtt } = useAlarmStore();

    // Connect to MQTT on mount
    useEffect(() => {
        connectMqtt();
    }, [connectMqtt]);

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
                {/* MQTT Connection Status */}
                <div className={cn(
                    "flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-medium",
                    isMqttConnected ? "bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-400" : "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-400"
                )}>
                    <span className={cn(
                        "w-2 h-2 rounded-full",
                        isMqttConnected ? "bg-green-500 animate-pulse" : "bg-red-500"
                    )}></span>
                    {isMqttConnected ? 'MQTT Connected' : 'MQTT Disconnected'}
                </div>

                {/* Alarms Button with Badge */}
                <Button
                    variant="ghost"
                    size="icon"
                    className="relative"
                    onClick={() => navigate('/alarms')}
                    title={activeAlarmCount > 0 ? `${activeAlarmCount} active alarm${activeAlarmCount > 1 ? 's' : ''}` : 'No active alarms'}
                >
                    <Bell size={20} />
                    {activeAlarmCount > 0 && (
                        <Badge
                            variant="destructive"
                            className="absolute -top-1 -right-1 h-5 min-w-5 px-1 flex items-center justify-center text-xs"
                        >
                            {activeAlarmCount > 99 ? '99+' : activeAlarmCount}
                        </Badge>
                    )}
                </Button>

                {/* Reload Config Button */}
                <Button variant="outline" size="sm" className="gap-2">
                    <RefreshCw size={14} />
                    Reload Config
                </Button>
            </div>
        </header>
    );
};

export default Header;
