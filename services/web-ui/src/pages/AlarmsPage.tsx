import { useState, useCallback, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useAlarms } from '@/hooks/useAlarms';
import { useTags } from '@/hooks/useTags';
import { useAlarmStore } from '@/stores/useAlarmStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
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
import AlarmDetailDialog from '@/components/AlarmDetailDialog';
import { CheckCircle, RefreshCw, Pause, Play, Filter, X, Calendar } from 'lucide-react';
import { Alarm } from '@/types';

const AlarmsPage = () => {
    const [searchParams, setSearchParams] = useSearchParams();

    // Get alarm store for real-time updates
    const { lastAlarmEvent, setLastAlarmEvent } = useAlarmStore();

    // Get filter state from URL params or defaults
    const getStateParam = () => {
        const state = searchParams.get('state');
        if (state === 'active' || state === 'rtn' || state === 'acknowledged' || state === 'cleared' || state === 'all') {
            return state;
        }
        return 'active'; // Default
    };

    const getStartParam = () => {
        const start = searchParams.get('start');
        return start || '';
    };

    const getEndParam = () => {
        const end = searchParams.get('end');
        return end || '';
    };

    const getTagIdParam = () => {
        const tagId = searchParams.get('tag_id');
        return tagId ? parseInt(tagId) : null;
    };

    // Local state for filters
    const [stateFilter, setStateFilter] = useState<string>(getStateParam());
    const [startDate, setStartDate] = useState<string>(getStartParam());
    const [endDate, setEndDate] = useState<string>(getEndParam());
    const [tagIdFilter, setTagIdFilter] = useState<number | null>(getTagIdParam());
    const [autoRefresh, setAutoRefresh] = useState(true);
    const [applyFilters, setApplyFilters] = useState(false);
    const [selectedAlarm, setSelectedAlarm] = useState<Alarm | null>(null);
    const [isDetailDialogOpen, setIsDetailDialogOpen] = useState(false);

    const { tags } = useTags(null);
    const { alarms, isLoading, acknowledge, refetch } = useAlarms(
        applyFilters ? stateFilter === 'all' ? undefined : stateFilter : getStateParam() === 'all' ? undefined : getStateParam(),
        applyFilters ? startDate : undefined,
        applyFilters ? endDate : undefined,
        applyFilters ? tagIdFilter : undefined,
        autoRefresh ? 10000 : false // 10 seconds
    );

    // Auto-refresh alarms list when new MQTT alarm event is received
    useEffect(() => {
        if (lastAlarmEvent) {
            // Refetch alarms to get the latest data
            refetch();
            // Clear the event after processing
            setLastAlarmEvent(null);
        }
    }, [lastAlarmEvent, refetch, setLastAlarmEvent]);

    // Update URL params when filters change
    const updateUrlParams = useCallback((params: Record<string, string | number | null>) => {
        const newParams = new URLSearchParams();
        Object.entries(params).forEach(([key, value]) => {
            if (value !== null && value !== undefined && value !== '') {
                newParams.set(key, String(value));
            }
        });
        setSearchParams(newParams);
    }, [setSearchParams]);

    // Handle apply filters
    const handleApplyFilters = () => {
        setApplyFilters(true);
        updateUrlParams({
            state: stateFilter,
            start: startDate,
            end: endDate,
            tag_id: tagIdFilter,
        });
    };

    // Handle clear filters
    const handleClearFilters = () => {
        setStateFilter('active');
        setStartDate('');
        setEndDate('');
        setTagIdFilter(null);
        setApplyFilters(false);
        setSearchParams({});
    };

    // Handle acknowledge
    const handleAcknowledge = async (id: number) => {
        try {
            await acknowledge(id);
        } catch (error) {
            console.error('Failed to acknowledge alarm', error);
        }
    };

    // Toggle auto-refresh
    const toggleAutoRefresh = () => {
        setAutoRefresh(!autoRefresh);
    };

    // Manual refresh
    const handleRefresh = () => {
        refetch();
    };

    // Handle row click to show alarm details
    const handleRowClick = (alarm: Alarm) => {
        setSelectedAlarm(alarm);
        setIsDetailDialogOpen(true);
    };

    const getStatusBadge = (state: string) => {
        switch (state) {
            case 'active':
                return <Badge variant="destructive" className="animate-pulse">Active</Badge>;
            case 'rtn':
                return <Badge variant="warning" className="bg-yellow-500 text-white">RTN</Badge>;
            case 'acknowledged':
                return <Badge variant="secondary" className="bg-blue-500 text-white">Acknowledged</Badge>;
            case 'cleared':
                return <Badge variant="outline">Cleared</Badge>;
            default:
                return <Badge variant="outline">{state}</Badge>;
        }
    };

    const activeAlarmsCount = alarms.filter(a => a.state === 'active').length;

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">System Alarms</h2>
                    <p className="text-muted-foreground">
                        Monitor and manage system alerts and anomalies.
                        {alarms.length > 0 && (
                            <span className="ml-2 font-semibold">
                                ({alarms.length} {alarms.length === 1 ? 'alarm' : 'alarms'}
                                {activeAlarmsCount > 0 && `, ${activeAlarmsCount} active`})
                            </span>
                        )}
                    </p>
                </div>

                <div className="flex items-center gap-3">
                    {/* Auto-refresh toggle */}
                    <Button
                        variant={autoRefresh ? "default" : "outline"}
                        size="sm"
                        onClick={toggleAutoRefresh}
                        className="flex items-center gap-2"
                    >
                        {autoRefresh ? <Pause size={16} /> : <Play size={16} />}
                        {autoRefresh ? 'Auto-refreshing' : 'Paused'}
                    </Button>

                    {/* Manual refresh button */}
                    <Button variant="outline" size="icon" onClick={handleRefresh} title="Refresh">
                        <RefreshCw size={16} />
                    </Button>
                </div>
            </div>

            {/* Filters */}
            <div className="rounded-lg border bg-white p-4">
                <div className="flex items-center gap-2 mb-3">
                    <Filter size={16} className="text-muted-foreground" />
                    <span className="text-sm font-medium">Filters</span>
                </div>

                <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                    {/* State filter */}
                    <div className="space-y-2">
                        <Label htmlFor="state-filter">State</Label>
                        <Select
                            value={stateFilter}
                            onValueChange={setStateFilter}
                        >
                            <SelectTrigger id="state-filter">
                                <SelectValue placeholder="Filter by state" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="active">Active</SelectItem>
                                <SelectItem value="rtn">RTN (Return to Normal)</SelectItem>
                                <SelectItem value="acknowledged">Acknowledged</SelectItem>
                                <SelectItem value="cleared">Cleared</SelectItem>
                                <SelectItem value="all">All</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>

                    {/* Tag filter */}
                    <div className="space-y-2">
                        <Label htmlFor="tag-filter">Tag</Label>
                        <Select
                            value={tagIdFilter?.toString() || 'all'}
                            onValueChange={(val) => setTagIdFilter((val && val !== 'all') ? parseInt(val) : null)}
                        >
                            <SelectTrigger id="tag-filter">
                                <SelectValue placeholder="All tags" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="all">All tags</SelectItem>
                                {tags.map(tag => (
                                    <SelectItem key={tag.id} value={tag.id.toString()}>
                                        {tag.alias || tag.code}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    {/* Start date */}
                    <div className="space-y-2">
                        <Label htmlFor="start-date">
                            <span className="flex items-center gap-1">
                                <Calendar size={14} />
                                Start Date
                            </span>
                        </Label>
                        <Input
                            id="start-date"
                            type="datetime-local"
                            value={startDate}
                            onChange={(e) => setStartDate(e.target.value)}
                        />
                    </div>

                    {/* End date */}
                    <div className="space-y-2">
                        <Label htmlFor="end-date">
                            <span className="flex items-center gap-1">
                                <Calendar size={14} />
                                End Date
                            </span>
                        </Label>
                        <Input
                            id="end-date"
                            type="datetime-local"
                            value={endDate}
                            onChange={(e) => setEndDate(e.target.value)}
                        />
                    </div>
                </div>

                {/* Filter actions */}
                <div className="flex items-center gap-3 mt-4">
                    <Button onClick={handleApplyFilters} className="flex items-center gap-2">
                        <Filter size={16} />
                        Apply Filters
                    </Button>
                    <Button variant="outline" onClick={handleClearFilters} className="flex items-center gap-2">
                        <X size={16} />
                        Clear Filters
                    </Button>
                </div>
            </div>

            {/* Alarms table */}
            <div className="rounded-md border bg-white">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[80px]">ID</TableHead>
                            <TableHead>Tag</TableHead>
                            <TableHead>State</TableHead>
                            <TableHead>Message</TableHead>
                            <TableHead>Triggered At</TableHead>
                            <TableHead>Acknowledged At</TableHead>
                            <TableHead>Cleared At</TableHead>
                            <TableHead className="text-right">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {isLoading ? (
                            <TableRow>
                                <TableCell colSpan={8} className="h-32 text-center text-muted-foreground">
                                    Loading alarms...
                                </TableCell>
                            </TableRow>
                        ) : alarms.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={8} className="h-32 text-center text-muted-foreground">
                                    <div className="flex flex-col items-center justify-center gap-2">
                                        <CheckCircle className="h-8 w-8 text-green-500 opacity-50" />
                                        <p>No alarms found</p>
                                    </div>
                                </TableCell>
                            </TableRow>
                        ) : (
                            alarms.map((alarm) => (
                                <TableRow
                                    key={alarm.id}
                                    className="cursor-pointer hover:bg-muted/50 transition-colors"
                                    onClick={() => handleRowClick(alarm)}
                                >
                                    <TableCell>{alarm.id}</TableCell>
                                    <TableCell>
                                        <span className="font-medium">
                                            {alarm.tag_alias || `Tag #${alarm.tag_id}`}
                                        </span>
                                    </TableCell>
                                    <TableCell>{getStatusBadge(alarm.state)}</TableCell>
                                    <TableCell>
                                        <span className="text-sm">{alarm.message}</span>
                                    </TableCell>
                                    <TableCell className="text-xs">
                                        {new Date(alarm.triggered_at).toLocaleString()}
                                    </TableCell>
                                    <TableCell className="text-xs">
                                        {alarm.acknowledged_at
                                            ? new Date(alarm.acknowledged_at).toLocaleString()
                                            : '-'}
                                    </TableCell>
                                    <TableCell className="text-xs">
                                        {alarm.cleared_at
                                            ? new Date(alarm.cleared_at).toLocaleString()
                                            : '-'}
                                    </TableCell>
                                    <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                                        {alarm.state === 'active' && (
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

            {/* Alarm Detail Dialog */}
            <AlarmDetailDialog
                alarm={selectedAlarm}
                open={isDetailDialogOpen}
                onOpenChange={setIsDetailDialogOpen}
                onAcknowledge={handleAcknowledge}
            />
        </div>
    );
};

export default AlarmsPage;
