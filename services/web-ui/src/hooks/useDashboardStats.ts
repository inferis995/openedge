import { useQuery } from '@tanstack/react-query';
import { organizationsApi } from '@/api/organizations';
import { sitesApi } from '@/api/sites';
import { gatewaysApi } from '@/api/gateways';
// import { alarmsApi } from '@/api/alarms';

export const useDashboardStats = () => {
    const orgsQuery = useQuery({
        queryKey: ['organizations'],
        queryFn: organizationsApi.getAll,
    });

    const sitesQuery = useQuery({
        queryKey: ['sites'],
        queryFn: () => sitesApi.getAll(),
    });

    const gatewaysQuery = useQuery({
        queryKey: ['gateways'],
        queryFn: () => gatewaysApi.getAll(),
    });

    // Alarms Query removed

    const stats = {
        organizations: orgsQuery.data?.length || 0,
        sites: sitesQuery.data?.length || 0,
        gateways: gatewaysQuery.data?.length || 0,
        gatewaysOnline: gatewaysQuery.data?.filter(g => g.status === 'online').length || 0,
        // activeAlarms: alarmsQuery.data?.length || 0,
    };

    // const recentAlarms = alarmsQuery.data?.slice(0, 5) || [];
    const recentGateways = gatewaysQuery.data?.slice(0, 5) || [];

    return {
        stats,
        // recentAlarms,
        recentGateways,
        isLoading: orgsQuery.isLoading || gatewaysQuery.isLoading,
    };
};
