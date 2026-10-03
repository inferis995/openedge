import { useState, useEffect, useMemo } from 'react';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Checkbox } from '@/components/ui/checkbox';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { CheckCircle2, AlertTriangle, AlertCircle, Info, RefreshCw, Search, Download, Trash2, BellOff } from 'lucide-react';
import { alarmsApi, AlarmEvent } from '@/api/alarms';
import { tagsApi } from '@/api/tags';
import { Tag } from '@/types';
import { toast } from 'sonner';
import { alarmTypeBadgeLabel } from '@/lib/alarmTypes';
import { useAuthStore } from '@/stores/useAuthStore';
import { useTranslation } from 'react-i18next';
import { startLoad } from '@/lib/startLoad';

function TableSkeletonRows({ cols, rows = 6 }: { cols: number; rows?: number }) {
    return (
        <>
            {Array.from({ length: rows }).map((_, i) => (
                <TableRow key={i}>
                    {Array.from({ length: cols }).map((_, j) => (
                        <TableCell key={j}>
                            <Skeleton className="h-4 w-full" />
                        </TableCell>
                    ))}
                </TableRow>
            ))}
        </>
    );
}

export default function AlarmsPage() {
    const { t: tr, i18n } = useTranslation();
    const { isAdmin } = useAuthStore();
    const [activeAlarms, setActiveAlarms] = useState<AlarmEvent[]>([]);
    const [history, setHistory] = useState<AlarmEvent[]>([]);
    const [tagsMap, setTagsMap] = useState<Record<number, Tag>>({});
    const [isLoading, setIsLoading] = useState(true);

    // Bulk selection (active tab)
    const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());

    // Confirm dialogs
    const [confirmDelete, setConfirmDelete] = useState<number | null>(null);
    const [confirmClear, setConfirmClear] = useState(false);
    const [confirmBulkAck, setConfirmBulkAck] = useState(false);

    // Search and filter state
    const [searchQuery, setSearchQuery] = useState('');
    const [dateFrom, setDateFrom] = useState('');
    const [dateTo, setDateTo] = useState('');

    // Fetch without flipping the loading flag first: on mount it is already true.
    const fetchData = async () => {
        try {
            const tags = await tagsApi.getAllTags();
            const tMap: Record<number, Tag> = {};
            tags.forEach(t => tMap[t.id] = t);
            setTagsMap(tMap);

            const [active, hist] = await Promise.all([
                alarmsApi.getActiveAlarms(),
                alarmsApi.getAlarmHistory(500),
            ]);
            setActiveAlarms(active || []);
            setHistory(hist || []);
            setSelectedIds(new Set());
        } catch {
            toast.error(tr('alarmsPage.load_failed'));
        } finally {
            setIsLoading(false);
        }
    };

    const loadData = () => {
        setIsLoading(true);
        return fetchData();
    };

    useEffect(() => {
        startLoad(fetchData);
        const interval = setInterval(loadData, 30000);
        return () => clearInterval(interval);
    }, []);

    const filteredHistory = useMemo(() => {
        let filtered = history;
        if (searchQuery.trim()) {
            const q = searchQuery.toLowerCase();
            filtered = filtered.filter(event => {
                const tag = tagsMap[event.tag_id];
                const tagName = tag ? (tag.alias || tag.code) : '';
                return tagName.toLowerCase().includes(q) ||
                    (event.message || '').toLowerCase().includes(q) ||
                    (event.alarm_type || '').toLowerCase().includes(q) ||
                    (event.severity || '').toLowerCase().includes(q);
            });
        }
        if (dateFrom) {
            const from = new Date(dateFrom);
            filtered = filtered.filter(e => new Date(e.trigger_time) >= from);
        }
        if (dateTo) {
            const to = new Date(dateTo + 'T23:59:59');
            filtered = filtered.filter(e => new Date(e.trigger_time) <= to);
        }
        return filtered;
    }, [history, searchQuery, dateFrom, dateTo, tagsMap]);

    const handleAcknowledge = async (id: number) => {
        try {
            await alarmsApi.acknowledgeAlarm(id);
            toast.success(tr('alarmsPage.ack_ok'));
            loadData();
        } catch {
            toast.error(tr('alarmsPage.ack_failed'));
        }
    };

    const handleBulkAcknowledge = async () => {
        const ids = Array.from(selectedIds);
        try {
            await Promise.all(ids.map(id => alarmsApi.acknowledgeAlarm(id)));
            toast.success(tr('alarmsPage.bulk_ack_ok', { count: ids.length }));
            loadData();
        } catch {
            toast.error(tr('alarmsPage.bulk_ack_failed'));
        }
    };

    const handleDeleteHistory = async (id: number) => {
        try {
            await alarmsApi.deleteAlarmHistory(id);
            toast.success(tr('alarmsPage.event_deleted'));
            loadData();
        } catch {
            toast.error(tr('alarmsPage.delete_failed'));
        }
    };

    const handleClearAllHistory = async () => {
        try {
            await alarmsApi.deleteAllAlarmHistory();
            toast.success(tr('alarmsPage.history_cleared'));
            loadData();
        } catch {
            toast.error(tr('alarmsPage.clear_failed'));
        }
    };

    const handleExportCSV = () => {
        try {
            const headers = [
                tr('alarmsPage.col_status'), tr('alarmsPage.col_severity'), 'Tag', tr('alarmsPage.col_type'),
                tr('alarmsPage.col_message'), tr('alarmsPage.col_value'), tr('alarmsPage.csv_trigger'), tr('alarmsPage.csv_clear'),
                tr('alarmsPage.col_ack_by'), tr('alarmsPage.csv_ack_time'),
            ];
            const rows = filteredHistory.map(e => {
                const tag = tagsMap[e.tag_id];
                const tagName = tag ? (tag.alias || tag.code) : `ID: ${e.tag_id}`;
                return [
                    `"${e.status}"`, `"${e.severity}"`, `"${tagName}"`, `"${e.alarm_type}"`,
                    `"${(e.message || '').replace(/"/g, '""')}"`,
                    `"${e.value_at_trigger?.toFixed(2) ?? ''}"`,
                    `"${formatTime(e.trigger_time)}"`, `"${formatTime(e.clear_time)}"`,
                    `"${e.bg_ack_user || ''}"`, `"${formatTime(e.ack_time)}"`,
                ].join(',');
            });
            const csvContent = 'data:text/csv;charset=utf-8,﻿' + [headers.join(','), ...rows].join('\n');
            const link = document.createElement('a');
            link.setAttribute('href', encodeURI(csvContent));
            link.setAttribute('download', `${tr('alarmsPage.csv_filename')}_${new Date().toISOString().split('T')[0]}.csv`);
            document.body.appendChild(link);
            link.click();
            document.body.removeChild(link);
            toast.success(tr('alarmsPage.csv_ok'));
        } catch {
            toast.error(tr('alarmsPage.csv_failed'));
        }
    };

    const formatTime = (ts: string | null) => {
        if (!ts) return '-';
        return new Date(ts).toLocaleString(i18n.language);
    };

    const getSeverityIcon = (severity: string) => {
        switch (severity.toLowerCase()) {
            case 'critical': return <AlertTriangle className="h-4 w-4 text-red-500" />;
            case 'warning':  return <AlertCircle className="h-4 w-4 text-orange-500" />;
            case 'info':     return <Info className="h-4 w-4 text-blue-500" />;
            default:         return <AlertCircle className="h-4 w-4" />;
        }
    };

    const getStatusBadge = (status: string) => {
        switch (status) {
            case 'ACTIVE':       return <Badge variant="destructive">{tr('alarmsPage.status_active')}</Badge>;
            case 'ACKNOWLEDGED': return <Badge variant="secondary" className="bg-orange-500/20 text-orange-500">{tr('alarmsPage.status_acknowledged')}</Badge>;
            case 'CLEARED':      return <Badge variant="outline" className="text-green-500 border-green-500">{tr('alarmsPage.status_cleared')}</Badge>;
            default:             return <Badge variant="outline">{status}</Badge>;
        }
    };

    const ackableActive = activeAlarms.filter(e => e.status === 'ACTIVE');
    const allSelected = ackableActive.length > 0 && ackableActive.every(e => selectedIds.has(e.id));

    const toggleAll = (checked: boolean) => {
        setSelectedIds(checked ? new Set(ackableActive.map(e => e.id)) : new Set());
    };

    const toggleRow = (id: number, checked: boolean) => {
        setSelectedIds(prev => {
            const next = new Set(prev);
            if (checked) { next.add(id); } else { next.delete(id); }
            return next;
        });
    };

    const renderActiveTable = () => (
        <Table>
            <TableHeader>
                <TableRow>
                    <TableHead className="w-8">
                        <Checkbox
                            checked={allSelected}
                            onCheckedChange={toggleAll}
                            disabled={ackableActive.length === 0}
                        />
                    </TableHead>
                    <TableHead>{tr('alarmsPage.col_status')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_severity')}</TableHead>
                    <TableHead>Tag</TableHead>
                    <TableHead>{tr('alarmsPage.col_type')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_message')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_value')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_trigger_time')}</TableHead>
                    <TableHead className="text-right">{tr('common.actions')}</TableHead>
                </TableRow>
            </TableHeader>
            <TableBody>
                {isLoading ? (
                    <TableSkeletonRows cols={9} />
                ) : activeAlarms.length === 0 ? (
                    <TableRow>
                        <TableCell colSpan={9} className="text-center py-12">
                            <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                <BellOff className="w-10 sm:w-8 h-10 sm:h-8 opacity-30" />
                                <p className="font-medium">{tr('alarmsPage.no_active')}</p>
                                <p className="text-xs">{tr('alarmsPage.no_active_desc')}</p>
                            </div>
                        </TableCell>
                    </TableRow>
                ) : activeAlarms.map((event, idx) => {
                    const tag = tagsMap[event.tag_id];
                    const tagName = tag ? (tag.alias || tag.code) : `ID: ${event.tag_id}`;
                    const isAckable = event.status === 'ACTIVE';
                    return (
                        <TableRow key={`${event.id}-${idx}`} className={selectedIds.has(event.id) ? 'bg-muted/40' : ''}>
                            <TableCell>
                                {isAckable && (
                                    <Checkbox
                                        checked={selectedIds.has(event.id)}
                                        onCheckedChange={v => toggleRow(event.id, !!v)}
                                    />
                                )}
                            </TableCell>
                            <TableCell>{getStatusBadge(event.status)}</TableCell>
                            <TableCell>
                                <div className="flex items-center gap-2">
                                    {getSeverityIcon(event.severity)}
                                    <span className="capitalize">{tr(`alarmsPage.severity_${(event.severity || '').toLowerCase()}`, { defaultValue: event.severity })}</span>
                                </div>
                            </TableCell>
                            <TableCell className="font-medium">{tagName}</TableCell>
                            <TableCell><Badge variant="outline" className="text-xs">{alarmTypeBadgeLabel(event.alarm_type)}</Badge></TableCell>
                            <TableCell>{event.message}</TableCell>
                            <TableCell>{event.value_at_trigger?.toFixed(2) ?? '-'}</TableCell>
                            <TableCell>{formatTime(event.trigger_time)}</TableCell>
                            <TableCell className="text-right">
                                {isAckable && (
                                    <Button size="sm" variant="outline" onClick={() => handleAcknowledge(event.id)} className="h-10 sm:h-8 gap-1">
                                        <CheckCircle2 size={14} /> {tr('alarmsPage.ack')}
                                    </Button>
                                )}
                            </TableCell>
                        </TableRow>
                    );
                })}
            </TableBody>
        </Table>
    );

    const renderHistoryTable = () => (
        <Table>
            <TableHeader>
                <TableRow>
                    <TableHead>{tr('alarmsPage.col_status')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_severity')}</TableHead>
                    <TableHead>Tag</TableHead>
                    <TableHead>{tr('alarmsPage.col_type')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_message')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_value')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_trigger_time')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_clear_time')}</TableHead>
                    <TableHead>{tr('alarmsPage.col_ack_by')}</TableHead>
                    {isAdmin() && <TableHead className="text-right">{tr('common.actions')}</TableHead>}
                </TableRow>
            </TableHeader>
            <TableBody>
                {isLoading ? (
                    <TableSkeletonRows cols={isAdmin() ? 10 : 9} />
                ) : filteredHistory.length === 0 ? (
                    <TableRow>
                        <TableCell colSpan={10} className="text-center py-12">
                            <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                <Info className="w-10 sm:w-8 h-10 sm:h-8 opacity-30" />
                                <p className="font-medium">{tr('alarmsPage.no_history')}</p>
                                {(searchQuery || dateFrom || dateTo) && (
                                    <p className="text-xs">{tr('alarmsPage.no_history_filtered')}</p>
                                )}
                            </div>
                        </TableCell>
                    </TableRow>
                ) : filteredHistory.map((event, idx) => {
                    const tag = tagsMap[event.tag_id];
                    const tagName = tag ? (tag.alias || tag.code) : `ID: ${event.tag_id}`;
                    return (
                        <TableRow key={`${event.id}-${idx}`}>
                            <TableCell>{getStatusBadge(event.status)}</TableCell>
                            <TableCell>
                                <div className="flex items-center gap-2">
                                    {getSeverityIcon(event.severity)}
                                    <span className="capitalize">{tr(`alarmsPage.severity_${(event.severity || '').toLowerCase()}`, { defaultValue: event.severity })}</span>
                                </div>
                            </TableCell>
                            <TableCell className="font-medium">{tagName}</TableCell>
                            <TableCell><Badge variant="outline" className="text-xs">{alarmTypeBadgeLabel(event.alarm_type)}</Badge></TableCell>
                            <TableCell>{event.message}</TableCell>
                            <TableCell>{event.value_at_trigger?.toFixed(2) ?? '-'}</TableCell>
                            <TableCell>{formatTime(event.trigger_time)}</TableCell>
                            <TableCell>{formatTime(event.clear_time)}</TableCell>
                            <TableCell>
                                <div className="text-xs">
                                    {event.bg_ack_user || '-'}
                                    {event.bg_ack_user && <><br />{formatTime(event.ack_time)}</>}
                                </div>
                            </TableCell>
                            {isAdmin() && (
                                <TableCell className="text-right">
                                    <Button
                                        size="sm" variant="outline"
                                        onClick={() => setConfirmDelete(event.id)}
                                        className="h-10 sm:h-8 w-10 sm:w-8 p-0 text-red-500 hover:text-red-600 hover:bg-red-50"
                                        title={tr('common.delete')}
                                    >
                                        <Trash2 size={14} />
                                    </Button>
                                </TableCell>
                            )}
                        </TableRow>
                    );
                })}
            </TableBody>
        </Table>
    );

    return (
        <div className="h-full flex flex-col p-6 overflow-hidden">
            {/* Confirm dialogs */}
            <ConfirmDialog
                open={confirmDelete !== null}
                title={tr('alarmsPage.delete_title')}
                description={tr('alarmsPage.delete_desc')}
                confirmLabel={tr('common.delete')}
                destructive
                onConfirm={() => confirmDelete !== null && handleDeleteHistory(confirmDelete)}
                onCancel={() => setConfirmDelete(null)}
            />
            <ConfirmDialog
                open={confirmClear}
                title={tr('alarmsPage.clear_title')}
                description={tr('alarmsPage.clear_desc')}
                confirmLabel={tr('alarmsPage.clear_confirm')}
                destructive
                onConfirm={handleClearAllHistory}
                onCancel={() => setConfirmClear(false)}
            />
            <ConfirmDialog
                open={confirmBulkAck}
                title={tr('alarmsPage.bulk_ack_title', { count: selectedIds.size })}
                description={tr('alarmsPage.bulk_ack_desc')}
                confirmLabel={tr('alarmsPage.bulk_ack_confirm')}
                onConfirm={handleBulkAcknowledge}
                onCancel={() => setConfirmBulkAck(false)}
            />

            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between mb-6">
                <div>
                    <h1 className="text-3xl font-bold tracking-tight">{tr('alarmsPage.title')}</h1>
                    <p className="text-muted-foreground mt-1">{tr('alarmsPage.subtitle')}</p>
                </div>
                <Button variant="outline" size="icon" onClick={loadData} disabled={isLoading} title={tr('alarmsPage.refresh')}>
                    <RefreshCw className={`h-4 w-4 ${isLoading ? 'animate-spin' : ''}`} />
                </Button>
            </div>

            <Card className="flex-1 overflow-hidden flex flex-col shadow-md border-border/50">
                <Tabs defaultValue="active" className="h-full flex flex-col">
                    <CardHeader className="pb-3 flex flex-row items-center justify-between space-y-0 shrink-0">
                        <TabsList className="bg-muted">
                            <TabsTrigger value="active" className="gap-2 px-6">
                                <AlertTriangle size={16} className="text-red-500" />
                                {tr('alarmsPage.tab_active')}
                                {activeAlarms.length > 0 && (
                                    <Badge variant="destructive" className="ml-1 px-1.5 py-0 h-5 text-xs rounded-full">
                                        {activeAlarms.length}
                                    </Badge>
                                )}
                            </TabsTrigger>
                            <TabsTrigger value="history" className="gap-2 px-6">
                                <Info size={16} />
                                {tr('alarmsPage.tab_history')}
                                {history.length > 0 && (
                                    <Badge variant="secondary" className="ml-1 px-1.5 py-0 h-5 text-xs rounded-full">
                                        {history.length}
                                    </Badge>
                                )}
                            </TabsTrigger>
                        </TabsList>
                    </CardHeader>

                    <CardContent className="flex-1 overflow-auto p-0">
                        {/* Active tab */}
                        <TabsContent value="active" className="mt-0 flex flex-col h-full">
                            {/* Bulk action bar */}
                            {selectedIds.size > 0 && (
                                <div className="flex items-center gap-3 px-4 py-2 bg-primary/5 border-b">
                                    <span className="text-sm font-medium">{tr('alarmsPage.selected_count', { count: selectedIds.size })}</span>
                                    <Button size="sm" className="h-10 sm:h-8 gap-1.5" onClick={() => setConfirmBulkAck(true)}>
                                        <CheckCircle2 size={14} /> {tr('alarmsPage.ack_selected')}
                                    </Button>
                                    <Button size="sm" variant="ghost" className="h-10 sm:h-8" onClick={() => setSelectedIds(new Set())}>
                                        {tr('alarmsPage.deselect')}
                                    </Button>
                                </div>
                            )}
                            {renderActiveTable()}
                        </TabsContent>

                        {/* History tab */}
                        <TabsContent value="history" className="mt-0">
                            <div className="flex items-center gap-3 p-4 border-b bg-muted/30 flex-wrap">
                                <div className="relative flex-1 min-w-[200px] max-w-sm">
                                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                                    <Input
                                        placeholder={tr('alarmsPage.search_placeholder')}
                                        value={searchQuery}
                                        onChange={e => setSearchQuery(e.target.value)}
                                        className="pl-9 h-9"
                                    />
                                </div>
                                <div className="flex items-center gap-2 text-sm text-muted-foreground flex-wrap">
                                    <span>{tr('alarmsPage.date_from')}</span>
                                    <Input type="date" value={dateFrom} onChange={e => setDateFrom(e.target.value)} className="h-9 w-40" />
                                    <span>{tr('alarmsPage.date_to')}</span>
                                    <Input type="date" value={dateTo} onChange={e => setDateTo(e.target.value)} className="h-9 w-40" />
                                    {(searchQuery || dateFrom || dateTo) && (
                                        <Button variant="ghost" size="sm" className="h-9 px-2"
                                            onClick={() => { setSearchQuery(''); setDateFrom(''); setDateTo(''); }}>
                                            {tr('alarmsPage.reset_filters')}
                                        </Button>
                                    )}
                                </div>
                                <div className="flex items-center gap-2 ml-auto">
                                    <span className="text-xs text-muted-foreground">{tr('alarmsPage.results', { count: filteredHistory.length })}</span>
                                    <Button variant="outline" size="sm" className="h-9 gap-2"
                                        onClick={handleExportCSV} disabled={filteredHistory.length === 0}>
                                        <Download size={16} /> {tr('alarmsPage.download_csv')}
                                    </Button>
                                    {isAdmin() && (
                                        <Button variant="outline" size="sm"
                                            className="h-9 gap-2 text-red-500 hover:text-red-600 hover:bg-red-50"
                                            onClick={() => setConfirmClear(true)} disabled={history.length === 0}>
                                            <Trash2 size={16} /> {tr('alarmsPage.delete_all')}
                                        </Button>
                                    )}
                                </div>
                            </div>
                            {renderHistoryTable()}
                        </TabsContent>
                    </CardContent>
                </Tabs>
            </Card>
        </div>
    );
}
