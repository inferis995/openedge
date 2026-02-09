import { useNavigate, useLocation } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';

import { RefreshCw, ChevronRight, Building2, Factory, MapPin, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useMqttStore } from '@/stores/useMqttStore';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { organizationsApi } from '@/api/organizations';
import { sitesApi } from '@/api/sites';
import { areasApi } from '@/api/areas';
import { useEffect } from 'react';

const Header = () => {
    const navigate = useNavigate();
    const location = useLocation();

    const { isMqttConnected, connectMqtt } = useMqttStore();

    // Get navigation context
    const { selectedOrgId, selectedSiteId, selectedAreaId, clearSelection } = useNavigationStore();

    // Fetch context names
    const { data: org } = useQuery({
        queryKey: ['org', selectedOrgId],
        queryFn: () => organizationsApi.get(selectedOrgId!),
        enabled: !!selectedOrgId
    });

    const { data: site } = useQuery({
        queryKey: ['site', selectedSiteId],
        queryFn: () => sitesApi.get(selectedSiteId!),
        enabled: !!selectedSiteId
    });

    const { data: area } = useQuery({
        queryKey: ['area', selectedAreaId],
        queryFn: () => areasApi.get(selectedAreaId!),
        enabled: !!selectedAreaId
    });

    // Connect to MQTT on mount
    useEffect(() => {
        connectMqtt();
    }, [connectMqtt]);

    const getBreadcrumbs = () => {
        const path = location.pathname;
        const parts = path.split('/').filter(Boolean);

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
            <div className="flex items-center gap-6">
                {/* Standard Breadcrumbs */}
                <div className="flex items-center gap-2 text-sm text-muted-foreground mr-4">
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

                {/* Context Indicator (Professional Badge) */}
                {(selectedOrgId || selectedSiteId || selectedAreaId) && (
                    <div className="hidden md:flex items-center bg-slate-100 dark:bg-slate-800 rounded-full px-3 py-1 border border-slate-200 dark:border-slate-700">
                        <span className="text-xs font-medium text-slate-500 mr-2 uppercase tracking-wider">Context:</span>

                        <div className="flex items-center gap-1 text-sm text-slate-700 dark:text-slate-300">
                            {org && (
                                <div className="flex items-center gap-1">
                                    <Building2 size={12} className="text-blue-500" />
                                    <span className="font-medium">{org.name}</span>
                                </div>
                            )}

                            {site && (
                                <>
                                    <span className="text-slate-400">/</span>
                                    <div className="flex items-center gap-1">
                                        <Factory size={12} className="text-indigo-500" />
                                        <span className="font-medium">{site.name}</span>
                                    </div>
                                </>
                            )}

                            {area && (
                                <>
                                    <span className="text-slate-400">/</span>
                                    <div className="flex items-center gap-1">
                                        <MapPin size={12} className="text-violet-500" />
                                        <span className="font-medium">{area.name}</span>
                                    </div>
                                </>
                            )}
                        </div>

                        <button
                            onClick={clearSelection}
                            className="ml-3 p-0.5 rounded-full hover:bg-slate-200 dark:hover:bg-slate-700 text-slate-400 hover:text-red-500 transition-colors"
                            title="Clear Context Filter"
                        >
                            <X size={14} />
                        </button>
                    </div>
                )}
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
            </div>
        </header>
    );
};

export default Header;
