import { Routes, Route, Outlet } from 'react-router-dom';
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
import SynopticsPage from '@/pages/SynopticsPage';
import SynopticEditorPage from '@/pages/SynopticEditorPage';
import AcceptInvitePage from '@/pages/AcceptInvitePage';
import AuditPage from '@/pages/AuditPage';
import ForgotPasswordPage from '@/pages/ForgotPasswordPage';
import ResetPasswordPage from '@/pages/ResetPasswordPage';
import ProfilePage from '@/pages/ProfilePage';
import FleetPage from '@/pages/FleetPage';
import SecurityPage from '@/pages/SecurityPage';
import InfrastructurePage from '@/pages/InfrastructurePage';
import ReleasesPage from '@/pages/ReleasesPage';
import NotFoundPage from '@/pages/NotFoundPage';

import { useEffect, useState, useRef } from 'react';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useTrendStore } from '@/stores/useTrendStore';
import { useAuthStore } from '@/stores/useAuthStore';
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
    const { user } = useAuthStore();
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
                // Org-scoped users have org_id in their JWT — use it directly,
                // no network round-trip needed.
                if (user?.org_id) {
                    setSelectedOrgId(user.org_id);
                    setIsInitializing(false);
                    return;
                }
                // Global admins: pick first org from the list.
                try {
                    const orgs = await organizationsApi.getAll();
                    if (orgs && orgs.length > 0) {
                        setSelectedOrgId(orgs[0].id);
                    }
                } catch (error) {
                    console.error('Failed to fetch organizations for auto-selection', error);
                }
            }
            setIsInitializing(false);
        };

        initOrganization();
    }, [selectedOrgId, setSelectedOrgId, user?.org_id]);

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
            {/* Public routes — no auth required */}
            <Route path="/accept-invite" element={<AcceptInvitePage />} />
            <Route path="/forgot-password" element={<ForgotPasswordPage />} />
            <Route path="/reset-password" element={<ResetPasswordPage />} />

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
                    <Route path="/synoptics" element={<SynopticsPage />} />
                    <Route path="/synoptics/:id" element={<SynopticEditorPage mode="view" />} />
                    <Route path="/synoptics/:id/edit" element={<SynopticEditorPage mode="edit" />} />
                    <Route path="/users" element={<UsersPage />} />
                    <Route path="/profile" element={<ProfilePage />} />
                </Route>
            </Route>

            <Route element={<RequireAdmin />}>
                <Route element={<LayoutWrapper />}>
                    <Route path="/diagnostics" element={<DiagnosticsPage />} />
                    <Route path="/mqtt-monitor" element={<MqttMonitorPage />} />
                    <Route path="/audit" element={<AuditPage />} />
                    <Route path="/fleet" element={<FleetPage />} />
                    <Route path="/releases" element={<ReleasesPage />} />
                    <Route path="/security" element={<SecurityPage />} />
                    <Route path="/infrastructure" element={<InfrastructurePage />} />
                </Route>
            </Route>

            <Route path="*" element={<NotFoundPage />} />
        </Routes>
    );
}

export default App;
