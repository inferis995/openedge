// Design System: See tasks/design-system.md
import { useEffect, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { Building2, Settings, TrendingUp, AlertTriangle, LogOut, Menu, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { getOrganizations } from '@/services/api';
import type { Organization } from '@/types/api';

interface LayoutProps {
  children: React.ReactNode;
}

export function Layout({ children }: LayoutProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const [organization, setOrganization] = useState<Organization | null>(null);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  // Check for organization ID in localStorage and fetch organization details
  useEffect(() => {
    const checkOrganization = async () => {
      const orgId = localStorage.getItem('ralph_org_id');
      if (!orgId) {
        // Redirect to login if no organization selected
        navigate('/login');
        return;
      }

      try {
        const orgs = await getOrganizations();
        const currentOrg = orgs.find((org) => org.id === parseInt(orgId, 10));
        if (currentOrg) {
          setOrganization(currentOrg);
        } else {
          // Organization not found, redirect to login
          localStorage.removeItem('ralph_org_id');
          navigate('/login');
        }
      } catch (error) {
        console.error('Failed to fetch organization:', error);
      }
    };

    checkOrganization();
  }, [navigate]);

  // Handle change organization
  const handleChangeOrganization = () => {
    localStorage.removeItem('ralph_org_id');
    navigate('/login');
  };

  // Navigation links
  const navLinks = [
    { path: '/config', label: 'Configuration', icon: Settings },
    { path: '/trend', label: 'Trend/Historian', icon: TrendingUp },
    { path: '/alarms', label: 'Alarms', icon: AlertTriangle },
  ];

  const isActive = (path: string) => location.pathname === path || location.pathname.startsWith(path + '/');

  return (
    <div className="flex h-screen overflow-hidden bg-gray-50">
      {/* Mobile sidebar overlay */}
      {mobileMenuOpen && (
        <div
          className="fixed inset-0 z-40 bg-black bg-opacity-50 lg:hidden"
          onClick={() => setMobileMenuOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={`
          fixed inset-y-0 left-0 z-50 w-64 transform bg-gray-900 text-white transition-transform duration-200 ease-in-out lg:static lg:translate-x-0
          ${mobileMenuOpen ? 'translate-x-0' : '-translate-x-full'}
        `}
      >
        {/* Sidebar header */}
        <div className="flex h-16 items-center justify-between px-6 border-b border-gray-800">
          <div className="flex items-center gap-2">
            <Building2 className="h-6 w-6 text-violet-400" />
            <span className="text-lg font-bold">Industrial Edge</span>
          </div>
          <button
            onClick={() => setMobileMenuOpen(false)}
            className="lg:hidden text-gray-400 hover:text-white"
          >
            <X className="h-6 w-6" />
          </button>
        </div>

        {/* Organization info */}
        {organization && (
          <div className="px-6 py-4 border-b border-gray-800">
            <div className="text-sm text-gray-400">Organization</div>
            <div className="text-sm font-medium text-white truncate">{organization.name}</div>
          </div>
        )}

        {/* Navigation */}
        <nav className="flex-1 px-4 py-4 space-y-1 overflow-y-auto">
          {navLinks.map((link) => {
            const Icon = link.icon;
            return (
              <Link
                key={link.path}
                to={link.path}
                onClick={() => setMobileMenuOpen(false)}
                className={`
                  flex items-center gap-3 px-4 py-3 rounded-lg text-sm font-medium transition-colors
                  ${isActive(link.path)
                    ? 'bg-violet-600 text-white'
                    : 'text-gray-300 hover:bg-gray-800 hover:text-white'
                  }
                `}
              >
                <Icon className="h-5 w-5" />
                {link.label}
              </Link>
            );
          })}
        </nav>

        {/* Sidebar footer */}
        <div className="p-4 border-t border-gray-800">
          <Button
            variant="ghost"
            className="w-full justify-start text-gray-300 hover:text-white hover:bg-gray-800"
            onClick={handleChangeOrganization}
          >
            <LogOut className="h-5 w-5 mr-3" />
            Change Organization
          </Button>
        </div>
      </aside>

      {/* Main content area */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Header */}
        <header className="flex h-16 items-center justify-between px-6 bg-white border-b border-gray-200">
          {/* Mobile menu button */}
          <button
            onClick={() => setMobileMenuOpen(true)}
            className="lg:hidden text-gray-600 hover:text-gray-900"
          >
            <Menu className="h-6 w-6" />
          </button>

          {/* Page title */}
          <div className="flex-1">
            <h1 className="text-xl font-semibold text-gray-900">
              {navLinks.find((link) => isActive(link.path))?.label || 'Industrial Edge Middleware'}
            </h1>
          </div>

          {/* Organization display in header (desktop) */}
          <div className="hidden lg:flex items-center gap-4">
            {organization && (
              <div className="flex items-center gap-2 text-sm text-gray-600">
                <Building2 className="h-4 w-4" />
                <span>{organization.name}</span>
              </div>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={handleChangeOrganization}
            >
              <LogOut className="h-4 w-4 mr-2" />
              Change
            </Button>
          </div>
        </header>

        {/* Main content */}
        <main className="flex-1 overflow-y-auto p-6">
          {children}
        </main>
      </div>
    </div>
  );
}

export default Layout;
