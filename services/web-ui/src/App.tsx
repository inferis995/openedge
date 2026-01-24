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

function App() {
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
                <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
        </MainLayout>
    );
}

export default App;
