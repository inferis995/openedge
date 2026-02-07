import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
// import { Calendar } from '@/components/ui/calendar';
// import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Download, RefreshCw } from 'lucide-react';
import { format, startOfDay, endOfDay } from 'date-fns';
import { cn } from '@/lib/utils';
import { useNavigationStore } from '@/stores/useNavigationStore';
import api from '@/api/client';

// Temporary types until API client is fully typed
interface HistoryEvent {
    timestamp: number;
    type: string;
    source: string;
    status: string;
    message: string;
}

const HistoryPage = () => {
    const { selectedOrgId } = useNavigationStore();
    const [date, setDate] = useState<Date | undefined>(new Date());
    const [eventType, setEventType] = useState<string>('all');

    // Fetch Events Query
    const { data: events, isLoading, refetch } = useQuery({
        queryKey: ['history', 'events', selectedOrgId, date, eventType],
        queryFn: async () => {
            if (!selectedOrgId || !date) return [];

            const start = startOfDay(date).toISOString();
            const end = endOfDay(date).toISOString();

            // Call the new endpoint
            const response = await api.get<HistoryEvent[]>(`/history/events`, {
                params: { start, end }
            });
            return response.data;
        },
        enabled: !!selectedOrgId && !!date,
    });

    const filteredEvents = events?.filter(e => {
        if (eventType === 'all') return true;
        if (eventType === 'connection') return e.type === 'connection';
        return true;
    }) || [];

    return (
        <div className="space-y-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <h2 className="text-3xl font-bold tracking-tight">Historian & Audit Log</h2>
                    <p className="text-muted-foreground">Review historical data and system events.</p>
                </div>

                <div className="flex items-center gap-2">
                    <div className="relative">
                        <input
                            type="date"
                            className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                            value={date ? format(date, 'yyyy-MM-dd') : ''}
                            onChange={(e) => setDate(e.target.value ? new Date(e.target.value) : undefined)}
                        />
                    </div>

                    <Select value={eventType} onValueChange={setEventType}>
                        <SelectTrigger className="w-[180px]">
                            <SelectValue placeholder="Event Type" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="all">All Events</SelectItem>
                            <SelectItem value="connection">Connections</SelectItem>

                        </SelectContent>
                    </Select>

                    <Button variant="ghost" size="icon" onClick={() => refetch()}>
                        <RefreshCw className="h-4 w-4" />
                    </Button>

                    <Button variant="outline" size="icon">
                        <Download className="h-4 w-4" />
                    </Button>
                </div>
            </div>

            {/* Event Log Table */}
            <Card>
                <CardHeader>
                    <CardTitle>Event Log</CardTitle>
                    <CardDescription>System events, connection status changes, and critical alerts.</CardDescription>
                </CardHeader>
                <CardContent>
                    {isLoading ? (
                        <div className="text-center py-8">Loading events...</div>
                    ) : filteredEvents.length === 0 ? (
                        <div className="text-center py-8 text-muted-foreground">No events found for this period.</div>
                    ) : (
                        <div className="rounded-md border">
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>Time</TableHead>
                                        <TableHead>Type</TableHead>
                                        <TableHead>Source</TableHead>
                                        <TableHead>Message</TableHead>
                                        <TableHead>Status</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {filteredEvents.map((event, i) => (
                                        <TableRow key={i}>
                                            <TableCell className="font-medium whitespace-nowrap">
                                                {format(new Date(event.timestamp * 1000), 'HH:mm:ss')}
                                                <span className="text-xs text-muted-foreground ml-2">
                                                    {format(new Date(event.timestamp * 1000), 'MMM d')}
                                                </span>
                                            </TableCell>
                                            <TableCell>
                                                <Badge variant={event.type === 'connection' ? 'outline' : 'secondary'}>
                                                    {event.type}
                                                </Badge>
                                            </TableCell>
                                            <TableCell>{event.source}</TableCell>
                                            <TableCell>{event.message}</TableCell>
                                            <TableCell>
                                                <Badge className={cn(
                                                    event.status === 'online' && "bg-green-500",
                                                    event.status === 'offline' && "bg-red-500",
                                                    event.status === 'connected' && "bg-green-500",
                                                    event.status === 'disconnected' && "bg-red-500",
                                                    // Default fallback
                                                    !['online', 'offline', 'connected', 'disconnected'].includes(event.status) && "bg-slate-500"
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
        </div>
    );
};

export default HistoryPage;
