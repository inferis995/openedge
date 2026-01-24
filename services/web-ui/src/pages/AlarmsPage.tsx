import { useState } from 'react';
import { useAlarms } from '@/hooks/useAlarms';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { CheckCircle, RefreshCw } from 'lucide-react';

const AlarmsPage = () => {
    const [filter, setFilter] = useState<string>('active');
    const { alarms, isLoading, acknowledge } = useAlarms(filter);

    const handleAcknowledge = async (id: number) => {
        try {
            await acknowledge(id);
        } catch (error) {
            console.error('Failed to acknowledge alarm', error);
        }
    };

    const getStatusBadge = (state: string) => {
        switch (state) {
            case 'active':
                return <Badge variant="destructive" className="animate-pulse">Active</Badge>;
            case 'rtn':
                return <Badge variant="warning">RTN</Badge>;
            case 'acknowledged':
                return <Badge variant="secondary">Acked</Badge>;
            case 'cleared':
                return <Badge variant="outline">Cleared</Badge>;
            default:
                return <Badge variant="outline">{state}</Badge>;
        }
    };

    if (isLoading) {
        return <div className="p-8 text-center text-slate-500">Loading alarms...</div>;
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">System Alarms</h2>
                    <p className="text-muted-foreground">
                        Monitor and manage system alerts and anomalies.
                    </p>
                </div>

                <div className="flex items-center gap-4">
                    <div className="w-[180px]">
                        <Select
                            value={filter}
                            onValueChange={setFilter}
                        >
                            <SelectTrigger>
                                <SelectValue placeholder="Filter Status" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="all">All Alarms</SelectItem>
                                <SelectItem value="active">Active Only</SelectItem>
                                <SelectItem value="rtn">Return To Normal</SelectItem>
                                <SelectItem value="acknowledged">Acknowledged</SelectItem>
                                <SelectItem value="cleared">History (Cleared)</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>
                    <Button variant="outline" size="icon" onClick={() => window.location.reload()}>
                        <RefreshCw size={16} />
                    </Button>
                </div>
            </div>

            <div className="rounded-md border bg-white">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[80px]">ID</TableHead>
                            <TableHead>Status</TableHead>
                            <TableHead>Message</TableHead>
                            <TableHead>Triggered At</TableHead>
                            <TableHead>Ack At</TableHead>
                            <TableHead className="text-right">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {alarms.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={6} className="h-32 text-center text-muted-foreground">
                                    <div className="flex flex-col items-center justify-center gap-2">
                                        <CheckCircle className="h-8 w-8 text-green-500 opacity-50" />
                                        <p>No alarms found for filter "{filter}"</p>
                                    </div>
                                </TableCell>
                            </TableRow>
                        ) : (
                            alarms.map((alarm) => (
                                <TableRow key={alarm.id}>
                                    <TableCell>{alarm.id}</TableCell>
                                    <TableCell>{getStatusBadge(alarm.state)}</TableCell>
                                    <TableCell>
                                        <div className="flex flex-col">
                                            <span className="font-medium">{alarm.tag_alias || `Tag #${alarm.tag_id}`}</span>
                                            <span className="text-xs text-muted-foreground">{alarm.message}</span>
                                        </div>
                                    </TableCell>
                                    <TableCell className="text-xs">
                                        {new Date(alarm.triggered_at).toLocaleString()}
                                    </TableCell>
                                    <TableCell className="text-xs">
                                        {alarm.acknowledged_at ? new Date(alarm.acknowledged_at).toLocaleString() : '-'}
                                    </TableCell>
                                    <TableCell className="text-right">
                                        {(alarm.state === 'active' || alarm.state === 'rtn') && (
                                            <Button
                                                size="sm"
                                                variant="secondary"
                                                onClick={() => handleAcknowledge(alarm.id)}
                                            >
                                                Acknowledge
                                            </Button>
                                        )}
                                    </TableCell>
                                </TableRow>
                            ))
                        )}
                    </TableBody>
                </Table>
            </div>
        </div>
    );
};

export default AlarmsPage;
