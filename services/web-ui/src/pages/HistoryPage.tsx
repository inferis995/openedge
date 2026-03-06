import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Download, Activity, Users } from 'lucide-react';
import { format, startOfDay, endOfDay } from 'date-fns';
import { cn } from '@/lib/utils';
import { useNavigationStore } from '@/stores/useNavigationStore';
import api from '@/api/client';
import { Calendar } from 'lucide-react';

// Event types
interface HistoryEvent {
    timestamp: number;
    type: string;
    source: string;
    status: string;
    message: string;
}

interface AuditLog {
    id: number;
    user_id: number | null;
    username: string;
    action: string;
    ip_address: string;
    user_agent: string;
    details: { org_id?: number; role?: string; reason?: string } | null;
    success: boolean;
    created_at: string;
}

type LogType = 'plc' | 'users';

const HistoryPage = () => {
    const { selectedOrgId } = useNavigationStore();
    const [date, setDate] = useState<Date | undefined>(new Date());
    const [logType, setLogType] = useState<LogType>('plc');
    const [actionFilter, setActionFilter] = useState<string>('all');

    // Fetch PLC Events Query
    const { data: events, isLoading: eventsLoading } = useQuery({
        queryKey: ['history', 'events', selectedOrgId, date],
        queryFn: async () => {
            if (!selectedOrgId || !date) return [];
            const start = startOfDay(date).toISOString();
            const end = endOfDay(date).toISOString();
            const response = await api.get<HistoryEvent[]>(`/history/events`, { params: { start, end } });
            return response.data;
        },
        enabled: !!selectedOrgId && !!date,
    });

    // Fetch Audit Logs Query
    const { data: auditLogs, isLoading: auditLoading } = useQuery({
        queryKey: ['audit', 'logs', selectedOrgId, date, actionFilter],
        queryFn: async () => {
            if (!selectedOrgId || !date) return [];
            const start = startOfDay(date).toISOString();
            const end = endOfDay(date).toISOString();
            const params: Record<string, string> = { start, end };
            if (actionFilter !== 'all') params.action = actionFilter;
            const response = await api.get<AuditLog[]>(`/audit/logs`, { params });
            return response.data;
        },
        enabled: !!selectedOrgId && !!date,
    });

    // Filter events to show only state changes
    const filteredEvents = (() => {
        if (!events) return [];
        // Sort chronologically (oldest first)
        const sorted = [...events].sort((a, b) => a.timestamp - b.timestamp);
        const result: HistoryEvent[] = [];
        const lastStatus: Record<string, string> = {};

        for (const event of sorted) {
            if (event.type !== 'connection') {
                result.push(event);
                continue;
            }
            const key = event.source;
            if (lastStatus[key] !== event.status) {
                result.push(event);
                lastStatus[key] = event.status;
            }
        }
        return result.reverse(); // Most recent first for display
    })();

    return (
        <div className="space-y-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <h2 className="text-3xl font-bold tracking-tight">Historian & Audit Log</h2>
                    <p className="text-muted-foreground">Review system events and user activity.</p>
                </div>

                <div className="flex items-center gap-2">
                    <div className="relative">
                        <Calendar className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground z-10 pointer-events-none" />
                        <input
                            type="date"
                            className="flex h-10 w-full pl-10 pr-3 rounded-md border border-input bg-card text-foreground px-3 py-2 text-sm clip-chamfer-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary"
                            value={date ? format(date, 'yyyy-MM-dd') : ''}
                            onChange={(e) => setDate(e.target.value ? new Date(e.target.value) : undefined)}
                        />
                    </div>
                    <Button variant="outline" size="icon" className="clip-chamfer-sm">
                        <Download className="h-4 w-4" />
                    </Button>
                </div>
            </div>

            {/* Tab Buttons */}
            <div className="flex gap-2">
                <Button
                    variant={logType === 'plc' ? 'default' : 'outline'}
                    onClick={() => setLogType('plc')}
                    className="gap-2"
                >
                    <Activity className="h-4 w-4" />
                    PLC Events
                </Button>
                <Button
                    variant={logType === 'users' ? 'default' : 'outline'}
                    onClick={() => setLogType('users')}
                    className="gap-2"
                >
                    <Users className="h-4 w-4" />
                    User Activity
                </Button>
            </div>

            {/* PLC Events Card */}
            {logType === 'plc' && (
                <Card className="clip-chamfer border-border bg-card">
                    <CardHeader className="border-b border-border pb-4">
                        <CardTitle>PLC Connection Events</CardTitle>
                        <CardDescription>
                            Shows only state changes (online → offline or offline → online)
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="pt-6">
                        {eventsLoading ? (
                            <div className="text-center py-8">Loading events...</div>
                        ) : filteredEvents.length === 0 ? (
                            <div className="text-center py-8 text-muted-foreground">No PLC events found.</div>
                        ) : (
                            <div className="rounded-md border border-border bg-card">
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead>Time</TableHead>
                                            <TableHead>Gateway</TableHead>
                                            <TableHead>Status</TableHead>
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {filteredEvents.map((event, i) => (
                                            <TableRow key={i}>
                                                <TableCell className="font-medium whitespace-nowrap">
                                                    {format(new Date(event.timestamp), 'HH:mm:ss')}
                                                    <span className="text-xs text-muted-foreground ml-2">
                                                        {format(new Date(event.timestamp), 'MMM d')}
                                                    </span>
                                                </TableCell>
                                                <TableCell>{event.source}</TableCell>
                                                <TableCell>
                                                    <Badge className={cn(
                                                        event.status === 'online' && "bg-green-500",
                                                        event.status === 'offline' && "bg-red-500",
                                                        !['online', 'offline'].includes(event.status) && "bg-slate-500"
                                                    )}>
                                                        {event.status}
                                                    </Badge>
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </div>
                        )}
                    </CardContent>
                </Card>
            )}

            {/* User Activity Card */}
            {logType === 'users' && (
                <Card className="clip-chamfer border-border bg-card">
                    <CardHeader className="border-b border-border pb-4">
                        <div className="flex items-center justify-between">
                            <div>
                                <CardTitle>User Activity Log</CardTitle>
                                <CardDescription>Login, logout, and other user actions</CardDescription>
                            </div>
                            <Select value={actionFilter} onValueChange={setActionFilter}>
                                <SelectTrigger className="w-[150px]">
                                    <SelectValue placeholder="Filter" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="all">All Actions</SelectItem>
                                    <SelectItem value="login">Login</SelectItem>
                                    <SelectItem value="logout">Logout</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </CardHeader>
                    <CardContent>
                        {auditLoading ? (
                            <div className="text-center py-8">Loading...</div>
                        ) : !auditLogs || auditLogs.length === 0 ? (
                            <div className="text-center py-8 text-muted-foreground">No user activity found.</div>
                        ) : (
                            <div className="rounded-md border border-border bg-card">
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead>Time</TableHead>
                                            <TableHead>User</TableHead>
                                            <TableHead>Action</TableHead>
                                            <TableHead>IP Address</TableHead>
                                            <TableHead>Status</TableHead>
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {auditLogs.map((log) => (
                                            <TableRow key={log.id}>
                                                <TableCell className="font-medium whitespace-nowrap">
                                                    {format(new Date(log.created_at), 'HH:mm:ss')}
                                                    <span className="text-xs text-muted-foreground ml-2">
                                                        {format(new Date(log.created_at), 'MMM d')}
                                                    </span>
                                                </TableCell>
                                                <TableCell>{log.username}</TableCell>
                                                <TableCell>
                                                    <Badge variant={log.action === 'login' ? 'default' : 'secondary'}>
                                                        {log.action}
                                                    </Badge>
                                                </TableCell>
                                                <TableCell className="font-mono text-sm">{log.ip_address}</TableCell>
                                                <TableCell>
                                                    <Badge className={cn(log.success ? "bg-green-500" : "bg-red-500")}>
                                                        {log.success ? 'Success' : 'Failed'}
                                                    </Badge>
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </div>
                        )}
                    </CardContent>
                </Card>
            )}
        </div>
    );
};

export default HistoryPage;
