import { useDashboardStats } from '@/hooks/useDashboardStats';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Building2, Cpu, Bell, Activity } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { useNavigate } from 'react-router-dom';

const StatCard = ({ title, value, icon: Icon, color, subtext }: any) => (
    <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">{title}</CardTitle>
            <Icon className={`h-4 w-4 ${color}`} />
        </CardHeader>
        <CardContent>
            <div className="text-2xl font-bold">{value}</div>
            <p className="text-xs text-muted-foreground">{subtext}</p>
        </CardContent>
    </Card>
);

const DashboardPage = () => {
    const navigate = useNavigate();
    const { stats, recentAlarms, recentGateways, isLoading } = useDashboardStats();

    if (isLoading) {
        return <div className="p-8 text-center text-slate-500">Loading dashboard...</div>;
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <h2 className="text-3xl font-bold tracking-tight">System Overview</h2>
                <div className="flex items-center gap-2">
                    <Button onClick={() => navigate('/organizations')}>Configure Hierarchy</Button>
                    <Button variant="outline" onClick={() => navigate('/gateways')}>Manage Gateways</Button>
                </div>
            </div>

            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
                <StatCard
                    title="Total Organizations"
                    value={stats.organizations}
                    icon={Building2}
                    color="text-blue-500"
                    subtext="Active tenants"
                />
                <StatCard
                    title="Total Gateways"
                    value={stats.gateways}
                    icon={Cpu}
                    color="text-indigo-500"
                    subtext={`${stats.gatewaysOnline} online`}
                />
                <StatCard
                    title="Active Alarms"
                    value={stats.activeAlarms}
                    icon={Bell}
                    color="text-red-500"
                    subtext="Requires attention"
                />
                <StatCard
                    title="System Status"
                    value="Healthy"
                    icon={Activity}
                    color="text-green-500"
                    subtext="All services running"
                />
            </div>

            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-7">
                <Card className="col-span-4">
                    <CardHeader>
                        <CardTitle>Recent Gateways</CardTitle>
                        <CardDescription>
                            Latest gateway connection status.
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <div className="space-y-4">
                            {recentGateways.length === 0 ? (
                                <p className="text-sm text-center text-muted-foreground py-4">No gateways configured</p>
                            ) : (
                                recentGateways.map((gw) => (
                                    <div key={gw.id} className="flex items-center justify-between border-b pb-2 last:border-0 last:pb-0">
                                        <div className="flex items-center gap-4">
                                            <div className={`h-2 w-2 rounded-full ${gw.connection_status === 'online' ? 'bg-green-500' : 'bg-red-500'}`} />
                                            <div>
                                                <p className="text-sm font-medium leading-none">{gw.name}</p>
                                                <p className="text-xs text-muted-foreground">{gw.driver_type} • {gw.connection_config?.ip_address || ''}</p>
                                            </div>
                                        </div>
                                        <Badge variant={gw.enabled ? 'outline' : 'secondary'}>{gw.enabled ? 'Enabled' : 'Disabled'}</Badge>
                                    </div>
                                ))
                            )}
                        </div>
                    </CardContent>
                </Card>

                <Card className="col-span-3">
                    <CardHeader>
                        <CardTitle>Active Alarms</CardTitle>
                        <CardDescription>
                            Most recent active alarms.
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <div className="space-y-4">
                            {recentAlarms.length === 0 ? (
                                <div className="flex flex-col items-center justify-center py-8 text-center text-slate-500">
                                    <Bell className="h-8 w-8 mb-2 opacity-20" />
                                    <p className="text-sm">No active alarms</p>
                                </div>
                            ) : (
                                recentAlarms.map((alarm) => (
                                    <div key={alarm.id} className="flex items-start gap-4 p-3 rounded-lg bg-red-50 border border-red-100">
                                        <Bell className="h-5 w-5 text-red-500 mt-0.5" />
                                        <div>
                                            <p className="text-sm font-medium text-red-900">{alarm.message}</p>
                                            <p className="text-xs text-red-700 mt-1">{new Date(alarm.triggered_at).toLocaleTimeString()}</p>
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>
                    </CardContent>
                </Card>
            </div>
        </div>
    );
};

export default DashboardPage;
