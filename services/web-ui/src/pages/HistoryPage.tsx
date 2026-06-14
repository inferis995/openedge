import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { Download, Activity, Users, Calendar, Wifi, WifiOff, LogIn, LogOut } from 'lucide-react';
import { format, startOfDay, endOfDay } from 'date-fns';
import { cn } from '@/lib/utils';
import { useNavigationStore } from '@/stores/useNavigationStore';
import api from '@/api/client';
import { toast } from 'sonner';

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

function TableSkeleton({ cols, rows = 6 }: { cols: number; rows?: number }) {
    return (
        <>
            {Array.from({ length: rows }).map((_, i) => (
                <TableRow key={i}>
                    {Array.from({ length: cols }).map((_, j) => (
                        <TableCell key={j}><Skeleton className="h-4 w-full" /></TableCell>
                    ))}
                </TableRow>
            ))}
        </>
    );
}

const HistoryPage = () => {
    const { selectedOrgId } = useNavigationStore();
    const [date, setDate] = useState<Date | undefined>(new Date());
    const [logType, setLogType] = useState<LogType>('plc');
    const [actionFilter, setActionFilter] = useState<string>('all');

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

    const filteredEvents = (() => {
        if (!events) return [];
        const sorted = [...events].sort((a, b) => a.timestamp - b.timestamp);
        const result: HistoryEvent[] = [];
        const lastStatus: Record<string, string> = {};
        for (const event of sorted) {
            if (event.type !== 'connection') { result.push(event); continue; }
            const key = event.source;
            if (lastStatus[key] !== event.status) {
                result.push(event);
                lastStatus[key] = event.status;
            }
        }
        return result.reverse();
    })();

    const exportEventsCSV = () => {
        const rows = filteredEvents.map(e => [
            `"${format(new Date(e.timestamp), 'dd/MM/yyyy HH:mm:ss')}"`,
            `"${e.source}"`,
            `"${e.status}"`,
            `"${e.message || ''}"`,
        ].join(','));
        const csv = ['Data/Ora,Gateway,Stato,Messaggio', ...rows].join('\n');
        const blob = new Blob([csv], { type: 'text/csv' });
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = `plc_events_${date ? format(date, 'yyyyMMdd') : 'export'}.csv`;
        a.click();
        URL.revokeObjectURL(a.href);
        toast.success('Export completato');
    };

    const exportAuditCSV = () => {
        if (!auditLogs) return;
        const rows = auditLogs.map(l => [
            `"${format(new Date(l.created_at), 'dd/MM/yyyy HH:mm:ss')}"`,
            `"${l.username}"`,
            `"${l.action}"`,
            `"${l.ip_address}"`,
            `"${l.success ? 'Success' : 'Failed'}"`,
        ].join(','));
        const csv = ['Data/Ora,Utente,Azione,IP,Esito', ...rows].join('\n');
        const blob = new Blob([csv], { type: 'text/csv' });
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = `audit_${date ? format(date, 'yyyyMMdd') : 'export'}.csv`;
        a.click();
        URL.revokeObjectURL(a.href);
        toast.success('Export completato');
    };

    return (
        <div className="space-y-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <h2 className="text-3xl font-bold tracking-tight">Historian & Audit Log</h2>
                    <p className="text-muted-foreground">Revisiona eventi di sistema e attività utenti.</p>
                </div>
                <div className="relative">
                    <Calendar className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground z-10 pointer-events-none" />
                    <input
                        type="date"
                        className="flex h-10 w-full pl-10 pr-3 rounded-md border border-input bg-card text-foreground text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary"
                        value={date ? format(date, 'yyyy-MM-dd') : ''}
                        onChange={e => setDate(e.target.value ? new Date(e.target.value) : undefined)}
                    />
                </div>
            </div>

            {/* Tab Buttons */}
            <div className="flex gap-2">
                <Button variant={logType === 'plc' ? 'default' : 'outline'} onClick={() => setLogType('plc')} className="gap-2">
                    <Activity className="h-4 w-4" /> PLC Events
                    {filteredEvents.length > 0 && logType === 'plc' && (
                        <Badge variant="secondary" className="ml-1">{filteredEvents.length}</Badge>
                    )}
                </Button>
                <Button variant={logType === 'users' ? 'default' : 'outline'} onClick={() => setLogType('users')} className="gap-2">
                    <Users className="h-4 w-4" /> User Activity
                    {auditLogs && auditLogs.length > 0 && logType === 'users' && (
                        <Badge variant="secondary" className="ml-1">{auditLogs.length}</Badge>
                    )}
                </Button>
            </div>

            {/* PLC Events */}
            {logType === 'plc' && (
                <Card>
                    <CardHeader className="border-b pb-4">
                        <div className="flex items-center justify-between">
                            <div>
                                <CardTitle>PLC Connection Events</CardTitle>
                                <CardDescription>Solo i cambiamenti di stato (online → offline o viceversa)</CardDescription>
                            </div>
                            <Button variant="outline" size="sm" className="gap-2" onClick={exportEventsCSV}
                                disabled={filteredEvents.length === 0}>
                                <Download className="h-4 w-4" /> Export CSV
                            </Button>
                        </div>
                    </CardHeader>
                    <CardContent className="pt-4 p-0">
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>Data/Ora</TableHead>
                                    <TableHead>Gateway</TableHead>
                                    <TableHead>Stato</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {eventsLoading ? (
                                    <TableSkeleton cols={3} />
                                ) : filteredEvents.length === 0 ? (
                                    <TableRow>
                                        <TableCell colSpan={3} className="text-center py-12">
                                            <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                                <Activity className="w-8 h-8 opacity-30" />
                                                <p className="font-medium">Nessun evento PLC trovato</p>
                                                <p className="text-xs">per il giorno selezionato</p>
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                ) : filteredEvents.map((event, i) => (
                                    <TableRow key={i}>
                                        <TableCell className="font-medium whitespace-nowrap">
                                            {format(new Date(event.timestamp), 'HH:mm:ss')}
                                            <span className="text-xs text-muted-foreground ml-2">
                                                {format(new Date(event.timestamp), 'dd MMM')}
                                            </span>
                                        </TableCell>
                                        <TableCell className="font-mono text-sm">{event.source}</TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-2">
                                                {event.status === 'online'
                                                    ? <Wifi className="w-4 h-4 text-green-500" />
                                                    : <WifiOff className="w-4 h-4 text-red-500" />}
                                                <Badge className={cn(
                                                    event.status === 'online' && 'bg-green-500',
                                                    event.status === 'offline' && 'bg-red-500',
                                                    !['online', 'offline'].includes(event.status) && 'bg-slate-500',
                                                )}>
                                                    {event.status}
                                                </Badge>
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    </CardContent>
                </Card>
            )}

            {/* User Activity */}
            {logType === 'users' && (
                <Card>
                    <CardHeader className="border-b pb-4">
                        <div className="flex items-center justify-between flex-wrap gap-3">
                            <div>
                                <CardTitle>User Activity Log</CardTitle>
                                <CardDescription>Login, logout e azioni degli utenti</CardDescription>
                            </div>
                            <div className="flex items-center gap-2">
                                <Select value={actionFilter} onValueChange={setActionFilter}>
                                    <SelectTrigger className="w-[150px]">
                                        <SelectValue placeholder="Filter" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="all">Tutte le azioni</SelectItem>
                                        <SelectItem value="login">Login</SelectItem>
                                        <SelectItem value="logout">Logout</SelectItem>
                                    </SelectContent>
                                </Select>
                                <Button variant="outline" size="sm" className="gap-2" onClick={exportAuditCSV}
                                    disabled={!auditLogs || auditLogs.length === 0}>
                                    <Download className="h-4 w-4" /> Export CSV
                                </Button>
                            </div>
                        </div>
                    </CardHeader>
                    <CardContent className="p-0">
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>Data/Ora</TableHead>
                                    <TableHead>Utente</TableHead>
                                    <TableHead>Azione</TableHead>
                                    <TableHead>IP Address</TableHead>
                                    <TableHead>Esito</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {auditLoading ? (
                                    <TableSkeleton cols={5} />
                                ) : !auditLogs || auditLogs.length === 0 ? (
                                    <TableRow>
                                        <TableCell colSpan={5} className="text-center py-12">
                                            <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                                <Users className="w-8 h-8 opacity-30" />
                                                <p className="font-medium">Nessuna attività utente trovata</p>
                                                <p className="text-xs">per il giorno selezionato</p>
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                ) : auditLogs.map(log => (
                                    <TableRow key={log.id}>
                                        <TableCell className="font-medium whitespace-nowrap">
                                            {format(new Date(log.created_at), 'HH:mm:ss')}
                                            <span className="text-xs text-muted-foreground ml-2">
                                                {format(new Date(log.created_at), 'dd MMM')}
                                            </span>
                                        </TableCell>
                                        <TableCell className="font-medium">{log.username}</TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-1.5">
                                                {log.action === 'login'
                                                    ? <LogIn className="w-3.5 h-3.5 text-green-500" />
                                                    : <LogOut className="w-3.5 h-3.5 text-muted-foreground" />}
                                                <Badge variant={log.action === 'login' ? 'default' : 'secondary'}>
                                                    {log.action}
                                                </Badge>
                                            </div>
                                        </TableCell>
                                        <TableCell className="font-mono text-sm text-muted-foreground">
                                            {log.ip_address}
                                        </TableCell>
                                        <TableCell>
                                            <Badge className={cn(log.success ? 'bg-green-500' : 'bg-red-500')}>
                                                {log.success ? 'Successo' : 'Fallito'}
                                            </Badge>
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    </CardContent>
                </Card>
            )}
        </div>
    );
};

export default HistoryPage;
