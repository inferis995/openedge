import { Routes, Route, Navigate } from 'react-router-dom';
import MainLayout from '@/components/layout/MainLayout';

import OrganizationsPage from '@/pages/OrganizationsPage';

import DashboardPage from '@/pages/DashboardPage';

import SitesPage from '@/pages/SitesPage';
import AreasPage from '@/pages/AreasPage';

import GatewaysPage from '@/pages/GatewaysPage';

import TagsPage from '@/pages/TagsPage';

import AlarmsPage from '@/pages/AlarmsPage';
import SystemPage from '@/pages/SystemPage';

import TrendPage from '@/pages/TrendPage';
import MqttMonitorPage from '@/pages/MqttMonitorPage';

import { useEffect, useState } from 'react';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { organizationsApi } from '@/api/organizations';
import { Loader2 } from 'lucide-react';

function App() {
    const { selectedOrgId, setSelectedOrgId } = useNavigationStore();
    const [isInitializing, setIsInitializing] = useState(true);

    useEffect(() => {
        const initOrganization = async () => {
            if (!selectedOrgId) {
                try {
                    const orgs = await organizationsApi.getAll();
                    if (orgs && orgs.length > 0) {
                        console.log('Auto-selecting organization:', orgs[0].name);
                        setSelectedOrgId(orgs[0].id);
                    }
                } catch (error) {
                    console.error('Failed to fetch organizations for auto-selection', error);
                }
            }
            setIsInitializing(false);
        };

        initOrganization();
    }, [selectedOrgId, setSelectedOrgId]);

    if (isInitializing) {
        return (
            <div className="flex h-screen w-full items-center justify-center">
                <Loader2 className="h-8 w-8 animate-spin text-primary" />
                <span className="ml-2 text-lg">Initializing...</span>
            </div>
        );
    }

    return (
        <MainLayout>
            <Routes>
                <Route path="/" element={<DashboardPage />} />
                <Route path="/organizations" element={<OrganizationsPage />} />
                <Route path="/sites" element={<SitesPage />} />
                <Route path="/areas" element={<AreasPage />} />
                <Route path="/gateways" element={<GatewaysPage />} />
                <Route path="/tags" element={<TagsPage />} />
                <Route path="/alarms" element={<AlarmsPage />} />
                <Route path="/system" element={<SystemPage />} />
                <Route path="/trend" element={<TrendPage />} />
                <Route path="/mqtt-monitor" element={<MqttMonitorPage />} />
                <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
        </MainLayout>
    );
}

export default App;
