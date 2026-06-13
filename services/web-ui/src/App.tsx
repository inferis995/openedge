import { Routes, Route, Navigate, Outlet } from 'react-router-dom';
import MainLayout from '@/components/layout/MainLayout';
import LoginPage from '@/pages/LoginPage';
import RequireAuth from '@/components/RequireAuth';
import RequireAdmin from '@/components/RequireAdmin';

import OrganizationsPage from '@/pages/OrganizationsPage';
import DashboardPage from '@/pages/DashboardPage';
import SitesPage from '@/pages/SitesPage';
import AreasPage from '@/pages/AreasPage';
import GatewaysPage from '@/pages/GatewaysPage';
import TagsPage from '@/pages/TagsPage';
import SystemPage from '@/pages/SystemPage';
import TrendPage from '@/pages/TrendPage';
import HistoryPage from '@/pages/HistoryPage';
import MqttMonitorPage from '@/pages/MqttMonitorPage';
import AlarmsPage from '@/pages/AlarmsPage';
import UsersPage from '@/pages/UsersPage';
import RecipesPage from '@/pages/RecipesPage';
import DiagnosticsPage from '@/pages/DiagnosticsPage';
import ReportsPage from '@/pages/ReportsPage';
import ShiftsPage from '@/pages/ShiftsPage';
import MaintenancePage from '@/pages/MaintenancePage';
import CustomKPIsPage from '@/pages/CustomKPIsPage';
import OEEProfilesPage from '@/pages/OEEProfilesPage';
import OEEKioskPage from '@/pages/OEEKioskPage';
import I3XPage from '@/pages/I3XPage';
import AcceptInvitePage from '@/pages/AcceptInvitePage';

import { useEffect, useState, useRef } from 'react';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useTrendStore } from '@/stores/useTrendStore';
import { organizationsApi } from '@/api/organizations';
import { useSparkplugListener } from '@/hooks/useSparkplugListener';
import { Loader2 } from 'lucide-react';

const LayoutWrapper = () => (
    <MainLayout>
        <Outlet />
    </MainLayout>
);

function App() {
    const { selectedOrgId, setSelectedOrgId } = useNavigationStore();
    const { resetStore: resetTrendStore } = useTrendStore();
    const [isInitializing, setIsInitializing] = useState(true);
    const prevOrgIdRef = useRef<number | null>(null);

    // Global Sparkplug B listener - tracks device online/offline status across all pages
    useSparkplugListener();

    // Reset trend store when organization changes
    useEffect(() => {
        if (prevOrgIdRef.current !== null && prevOrgIdRef.current !== selectedOrgId) {
            console.log('Organization changed, resetting trend store');
            resetTrendStore();
        }
        prevOrgIdRef.current = selectedOrgId;
    }, [selectedOrgId, resetTrendStore]);

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
        <Routes>
            <Route path="/login" element={<LoginPage />} />
            {/* Public route — recipient arrives here from email invite link */}
            <Route path="/accept-invite" element={<AcceptInvitePage />} />

            {/* TV/Kiosk mode — full-screen senza sidebar/header. Resta
                sotto RequireAuth ma fuori dal LayoutWrapper. */}
            <Route element={<RequireAuth />}>
                <Route path="/tv/oee" element={<OEEKioskPage />} />
            </Route>

            <Route element={<RequireAuth />}>
                <Route element={<LayoutWrapper />}>
                    <Route path="/" element={<DashboardPage />} />
                    <Route path="/organizations" element={<OrganizationsPage />} />
                    <Route path="/sites" element={<SitesPage />} />
                    <Route path="/areas" element={<AreasPage />} />
                    <Route path="/gateways" element={<GatewaysPage />} />
                    <Route path="/tags" element={<TagsPage />} />
                    <Route path="/system" element={<SystemPage />} />
                    <Route path="/trend" element={<TrendPage />} />
                    <Route path="/history" element={<HistoryPage />} />
                    <Route path="/alarms" element={<AlarmsPage />} />
                    <Route path="/recipes" element={<RecipesPage />} />
                    <Route path="/reports" element={<ReportsPage />} />
                    <Route path="/shifts" element={<ShiftsPage />} />
                    <Route path="/maintenance" element={<MaintenancePage />} />
                    <Route path="/kpis" element={<CustomKPIsPage />} />
                    <Route path="/oee-profiles" element={<OEEProfilesPage />} />
                    <Route path="/i3x" element={<I3XPage />} />
                    <Route path="/users" element={<UsersPage />} />
                </Route>
            </Route>

            <Route element={<RequireAdmin />}>
                <Route element={<LayoutWrapper />}>
                    <Route path="/diagnostics" element={<DiagnosticsPage />} />
                    <Route path="/mqtt-monitor" element={<MqttMonitorPage />} />
                </Route>
            </Route>

            <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
    );
}

export default App;
