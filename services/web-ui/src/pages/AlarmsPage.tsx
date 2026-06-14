import { useState, useEffect, useMemo, useCallback } from 'react';
import { formatDistanceToNow } from 'date-fns';
import { it } from 'date-fns/locale';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Checkbox } from '@/components/ui/checkbox';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import {
    CheckCircle2, AlertTriangle, AlertCircle, Info, RefreshCw,
    Search, Download, Trash2, BellOff, Clock, Filter,
} from 'lucide-react';
import { alarmsApi, AlarmEvent } from '@/api/alarms';
import { tagsApi } from '@/api/tags';
import { Tag } from '@/types';
import { toast } from 'sonner';
import { useAuthStore } from '@/stores/useAuthStore';
import { cn } from '@/lib/utils';

// ─── Helpers ─────────────────────────────────────────────────────────────────

function TableSkeletonRows({ cols, rows = 6 }: { cols: number; rows?: number }) {
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

function relativeTime(ts: string | null | undefined): string {
    if (!ts) return '—';
    try {
        return formatDistanceToNow(new Date(ts), { addSuffix: true, locale: it });
    } catch {
        return '—';
    }
}

function absoluteTime(ts: string | null | undefined): string {
    if (!ts) return '—';
    return new Date(ts).toLocaleString('it-IT');
}

function SeverityIcon({ severity }: { severity: string }) {
    switch (severity.toLowerCase()) {
        case 'critical': return <AlertTriangle className="h-4 w-4 text-red-500 shrink-0" />;
        case 'warning':  return <AlertCircle  className="h-4 w-4 text-orange-500 shrink-0" />;
        case 'info':     return <Info         className="h-4 w-4 text-blue-500 shrink-0" />;
        default:         return <AlertCircle  className="h-4 w-4 text-muted-foreground shrink-0" />;
    }
}

function SeverityBadge({ severity }: { severity: string }) {
    const cls = {
        critical: 'bg-red-500/15 text-red-600 border-red-500/30',
        warning:  'bg-orange-500/15 text-orange-600 border-orange-500/30',
        info:     'bg-blue-500/15 text-blue-600 border-blue-500/30',
    }[severity.toLowerCase()] ?? 'bg-muted text-muted-foreground';
    return (
        <Badge variant="outline" className={cn('text-xs font-semibold capitalize', cls)}>
            {severity}
        </Badge>
    );
}

function StatusBadge({ status }: { status: string }) {
    switch (status) {
        case 'ACTIVE':
            return (
                <Badge className="bg-red-500/15 text-red-600 border-red-500/40 text-xs font-semibold">
                    <span className="inline-block w-1.5 h-1.5 rounded-full bg-red-500 animate-pulse mr-1.5" />
                    ATTIVO
                </Badge>
            );
        case 'ACKNOWLEDGED':
            return (
                <Badge variant="outline" className="bg-amber-500/10 text-amber-600 border-amber-500/30 text-xs font-semibold">
                    RICONOSCIUTO
                </Badge>
            );
        case 'CLEARED':
            return (
                <Badge variant="outline" className="text-green-600 border-green-500/30 text-xs font-semibold">
                    RIENTRATO
                </Badge>
            );
        default:
            return <Badge variant="outline" className="text-xs">{status}</Badge>;
    }
}

// ─── Main page ────────────────────────────────────────────────────────────────

const HISTORY_PAGE_SIZE = 100;
const SEVERITY_OPTIONS = ['all', 'critical', 'warning', 'info'] as const;

export default function AlarmsPage() {
    const { isAdmin } = useAuthStore();

    const [activeAlarms, setActiveAlarms]   = useState<AlarmEvent[]>([]);
    const [history, setHistory]             = useState<AlarmEvent[]>([]);
    const [tagsMap, setTagsMap]             = useState<Record<number, Tag>>({});
    const [isLoading, setIsLoading]         = useState(true);
    const [ackingIds, setAckingIds]         = useState<Set<number>>(new Set());

    // Bulk selection
    const [selectedIds, setSelectedIds]     = useState<Set<number>>(new Set());

    // Confirm dialogs
    const [confirmDelete, setConfirmDelete] = useState<number | null>(null);
    const [confirmClear, setConfirmClear]   = useState(false);
    const [confirmBulkAck, setConfirmBulkAck] = useState(false);

    // Filters
    const [searchQuery, setSearchQuery]     = useState('');
    const [dateFrom, setDateFrom]           = useState('');
    const [dateTo, setDateTo]               = useState('');
    const [severityFilter, setSeverityFilter] = useState<string>('all');

    // Pagination for history
    const [historyPage, setHistoryPage]     = useState(1);

    const loadData = useCallback(async (silent = false) => {
        if (!silent) setIsLoading(true);
        try {
            const tags = await tagsApi.getAllTags();
            const tMap: Record<number, Tag> = {};
            tags.forEach(t => { tMap[t.id] = t; });
            setTagsMap(tMap);

            const [active, hist] = await Promise.all([
                alarmsApi.getActiveAlarms(),
                alarmsApi.getAlarmHistory(500),
            ]);
            setActiveAlarms(active || []);
            setHistory(hist || []);
            setSelectedIds(new Set());
        } catch {
            toast.error('Errore nel caricamento allarmi');
        } finally {
            setIsLoading(false);
        }
    }, []);

    useEffect(() => {
        loadData();
        const interval = setInterval(() => loadData(true), 30_000);
        return () => clearInterval(interval);
    }, [loadData]);

    // Optimistic ACK — aggiorna lo stato localmente senza aspettare il reload
    const handleAcknowledge = async (id: number) => {
        setAckingIds(prev => new Set(prev).add(id));
        try {
            await alarmsApi.acknowledgeAlarm(id);
            // Aggiorna localmente: status ACTIVE → ACKNOWLEDGED
            setActiveAlarms(prev =>
                prev.map(a => a.id === id ? { ...a, status: 'ACKNOWLEDGED' } : a)
            );
            setHistory(prev =>
                prev.map(a => a.id === id ? { ...a, status: 'ACKNOWLEDGED' } : a)
            );
            setSelectedIds(prev => { const n = new Set(prev); n.delete(id); return n; });
            toast.success('Allarme riconosciuto');
        } catch {
            toast.error('Errore nel riconoscimento allarme');
        } finally {
            setAckingIds(prev => { const n = new Set(prev); n.delete(id); return n; });
        }
    };

    const handleBulkAcknowledge = async () => {
        const ids = Array.from(selectedIds);
        try {
            await Promise.all(ids.map(id => alarmsApi.acknowledgeAlarm(id)));
            // Optimistic bulk update
            setActiveAlarms(prev =>
                prev.map(a => ids.includes(a.id) ? { ...a, status: 'ACKNOWLEDGED' } : a)
            );
            setHistory(prev =>
                prev.map(a => ids.includes(a.id) ? { ...a, status: 'ACKNOWLEDGED' } : a)
            );
            setSelectedIds(new Set());
            toast.success(`${ids.length} allarmi riconosciuti`);
        } catch {
            toast.error('Errore nel riconoscimento bulk');
            loadData();
        }
    };

    const handleDeleteHistory = async (id: number) => {
        try {
            await alarmsApi.deleteAlarmHistory(id);
            setHistory(prev => prev.filter(a => a.id !== id));
            toast.success('Evento eliminato');
        } catch {
            toast.error("Errore durante l'eliminazione");
        }
    };

    const handleClearAllHistory = async () => {
        try {
            await alarmsApi.deleteAllAlarmHistory();
            setHistory([]);
            toast.success('Storico svuotato');
        } catch {
            toast.error("Errore durante lo svuotamento");
        }
    };

    const handleExportCSV = () => {
        try {
            const headers = ['Stato','Gravità','Tag','Tipo','Messaggio','Valore','Scatto','Rientro','Ack da','Ack time'];
            const rows = filteredHistory.map(e => {
                const tag = tagsMap[e.tag_id];
                const tagName = tag ? (tag.alias || tag.code) : `ID: ${e.tag_id}`;
                return [
                    `"${e.status}"`,`"${e.severity}"`,`"${tagName}"`,`"${e.alarm_type}"`,
                    `"${(e.message||'').replace(/"/g,'""')}"`,
                    `"${e.value_at_trigger?.toFixed(2) ?? ''}"`,
                    `"${absoluteTime(e.trigger_time)}"`,`"${absoluteTime(e.clear_time)}"`,
                    `"${e.bg_ack_user||''}"`,`"${absoluteTime(e.ack_time)}"`,
                ].join(',');
            });
            const csv = 'data:text/csv;charset=utf-8,﻿' + [headers.join(','), ...rows].join('\n');
            const link = document.createElement('a');
            link.href = encodeURI(csv);
            link.download = `allarmi_${new Date().toISOString().split('T')[0]}.csv`;
            document.body.appendChild(link);
            link.click();
            document.body.removeChild(link);
            toast.success('CSV esportato');
        } catch {
            toast.error("Errore nell'esportazione");
        }
    };

    // ── Filtered / paginated data ─────────────────────────────────────────────

    const filteredHistory = useMemo(() => {
        let f = history;
        if (searchQuery.trim()) {
            const q = searchQuery.toLowerCase();
            f = f.filter(e => {
                const tag = tagsMap[e.tag_id];
                const name = tag ? (tag.alias || tag.code) : '';
                return name.toLowerCase().includes(q)
                    || (e.message || '').toLowerCase().includes(q)
                    || (e.alarm_type || '').toLowerCase().includes(q)
                    || (e.severity || '').toLowerCase().includes(q);
            });
        }
        if (severityFilter !== 'all') {
            f = f.filter(e => e.severity.toLowerCase() === severityFilter);
        }
        if (dateFrom) {
            const from = new Date(dateFrom);
            f = f.filter(e => new Date(e.trigger_time) >= from);
        }
        if (dateTo) {
            const to = new Date(dateTo + 'T23:59:59');
            f = f.filter(e => new Date(e.trigger_time) <= to);
        }
        return f;
    }, [history, searchQuery, severityFilter, dateFrom, dateTo, tagsMap]);

    const pagedHistory = useMemo(
        () => filteredHistory.slice(0, historyPage * HISTORY_PAGE_SIZE),
        [filteredHistory, historyPage],
    );

    const ackableActive = activeAlarms.filter(e => e.status === 'ACTIVE');
    const allSelected   = ackableActive.length > 0 && ackableActive.every(e => selectedIds.has(e.id));
    const criticalCount = activeAlarms.filter(e => e.status === 'ACTIVE' && e.severity.toLowerCase() === 'critical').length;

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

    // ── Render ────────────────────────────────────────────────────────────────

    return (
        <div className="h-full flex flex-col p-6 overflow-hidden">
            {/* Confirm dialogs */}
            <ConfirmDialog
                open={confirmDelete !== null}
                title="Elimina evento"
                description="Vuoi eliminare definitivamente questo evento dallo storico? L'operazione non è reversibile."
                confirmLabel="Elimina"
                destructive
                onConfirm={() => confirmDelete !== null && handleDeleteHistory(confirmDelete)}
                onCancel={() => setConfirmDelete(null)}
            />
            <ConfirmDialog
                open={confirmClear}
                title="Svuota storico allarmi"
                description="Vuoi eliminare TUTTI gli eventi dallo storico? Questa operazione non è reversibile."
                confirmLabel="Svuota tutto"
                destructive
                onConfirm={handleClearAllHistory}
                onCancel={() => setConfirmClear(false)}
            />
            <ConfirmDialog
                open={confirmBulkAck}
                title={`Riconosci ${selectedIds.size} allarmi`}
                description="Tutti gli allarmi selezionati verranno marcati come riconosciuti."
                confirmLabel="Riconosci tutti"
                onConfirm={handleBulkAcknowledge}
                onCancel={() => setConfirmBulkAck(false)}
            />

            {/* Header */}
            <div className="flex items-center justify-between mb-6 flex-shrink-0">
                <div>
                    <div className="flex items-center gap-3">
                        <h1 className="text-2xl font-bold tracking-tight">Allarmi e Notifiche</h1>
                        {criticalCount > 0 && (
                            <Badge className="bg-red-500 text-white text-xs animate-pulse">
                                {criticalCount} CRITICAL
                            </Badge>
                        )}
                    </div>
                    <p className="text-sm text-muted-foreground mt-0.5">
                        Monitora gli allarmi in corso e lo storico degli eventi.
                    </p>
                </div>
                <Button variant="outline" size="icon" onClick={() => loadData()} disabled={isLoading} className="clip-chamfer-sm">
                    <RefreshCw className={cn('h-4 w-4', isLoading && 'animate-spin')} />
                </Button>
            </div>

            {/* Stats row */}
            <div className="flex items-center gap-4 mb-4 flex-shrink-0 text-sm">
                <div className="flex items-center gap-1.5">
                    <span className={cn('inline-block w-2.5 h-2.5 rounded-full', ackableActive.length > 0 ? 'bg-red-500 animate-pulse' : 'bg-green-500')} />
                    <span className="font-medium">{ackableActive.length}</span>
                    <span className="text-muted-foreground">attivi</span>
                </div>
                <div className="flex items-center gap-1.5 text-muted-foreground">
                    <span className="font-medium text-amber-600">{activeAlarms.filter(e => e.status === 'ACKNOWLEDGED').length}</span>
                    <span>riconosciuti</span>
                </div>
                <div className="flex items-center gap-1.5 text-muted-foreground">
                    <span className="font-medium">{history.length}</span>
                    <span>nello storico</span>
                </div>
            </div>

            <Card className="flex-1 overflow-hidden flex flex-col shadow-sm border-border/50">
                <Tabs defaultValue="active" className="h-full flex flex-col">
                    <CardHeader className="pb-0 pt-3 px-4 flex-shrink-0 border-b">
                        <TabsList className="bg-transparent gap-0 p-0 h-auto">
                            <TabsTrigger
                                value="active"
                                className="rounded-none border-b-2 border-transparent data-[state=active]:border-primary data-[state=active]:bg-transparent pb-3 gap-2 px-4"
                            >
                                <AlertTriangle size={15} className="text-red-500" />
                                Allarmi Attivi
                                {activeAlarms.length > 0 && (
                                    <Badge variant="destructive" className="ml-1 px-1.5 py-0 h-5 text-xs rounded-full">
                                        {activeAlarms.length}
                                    </Badge>
                                )}
                            </TabsTrigger>
                            <TabsTrigger
                                value="history"
                                className="rounded-none border-b-2 border-transparent data-[state=active]:border-primary data-[state=active]:bg-transparent pb-3 gap-2 px-4"
                            >
                                <Clock size={15} />
                                Storico
                                <Badge variant="secondary" className="ml-1 px-1.5 py-0 h-5 text-xs rounded-full">
                                    {history.length}
                                </Badge>
                            </TabsTrigger>
                        </TabsList>
                    </CardHeader>

                    <CardContent className="flex-1 overflow-auto p-0">

                        {/* ── Active tab ── */}
                        <TabsContent value="active" className="mt-0 h-full">
                            {/* Bulk action bar */}
                            {selectedIds.size > 0 && (
                                <div className="flex items-center gap-3 px-4 py-2 bg-primary/5 border-b sticky top-0 z-10">
                                    <span className="text-sm font-medium">{selectedIds.size} selezionati</span>
                                    <Button size="sm" className="h-7 gap-1.5 text-xs" onClick={() => setConfirmBulkAck(true)}>
                                        <CheckCircle2 size={13} /> Riconosci tutti
                                    </Button>
                                    <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setSelectedIds(new Set())}>
                                        Deseleziona
                                    </Button>
                                </div>
                            )}
                            <Table>
                                <TableHeader>
                                    <TableRow className="bg-muted/30">
                                        <TableHead className="w-8">
                                            <Checkbox
                                                checked={allSelected}
                                                onCheckedChange={toggleAll}
                                                disabled={ackableActive.length === 0}
                                            />
                                        </TableHead>
                                        <TableHead className="text-xs">Stato</TableHead>
                                        <TableHead className="text-xs">Gravità</TableHead>
                                        <TableHead className="text-xs">Tag</TableHead>
                                        <TableHead className="text-xs">Tipo</TableHead>
                                        <TableHead className="text-xs">Messaggio</TableHead>
                                        <TableHead className="text-xs">Valore</TableHead>
                                        <TableHead className="text-xs">Scatto</TableHead>
                                        <TableHead className="text-xs w-24"></TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {isLoading ? (
                                        <TableSkeletonRows cols={9} />
                                    ) : activeAlarms.length === 0 ? (
                                        <TableRow>
                                            <TableCell colSpan={9} className="text-center py-16">
                                                <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                                    <CheckCircle2 className="w-10 h-10 text-green-500 opacity-50" />
                                                    <p className="font-medium">Nessun allarme attivo</p>
                                                    <p className="text-xs">Il sistema è in stato normale</p>
                                                </div>
                                            </TableCell>
                                        </TableRow>
                                    ) : activeAlarms.map((event, idx) => {
                                        const tag     = tagsMap[event.tag_id];
                                        const tagName = tag ? (tag.alias || tag.code) : `ID: ${event.tag_id}`;
                                        const isACK   = event.status === 'ACTIVE';
                                        const isCrit  = event.severity.toLowerCase() === 'critical' && isACK;
                                        return (
                                            <TableRow
                                                key={`${event.id}-${idx}`}
                                                className={cn(
                                                    selectedIds.has(event.id) && 'bg-primary/5',
                                                    isCrit && 'border-l-2 border-red-500',
                                                )}
                                            >
                                                <TableCell className="py-2">
                                                    {isACK && (
                                                        <Checkbox
                                                            checked={selectedIds.has(event.id)}
                                                            onCheckedChange={v => toggleRow(event.id, !!v)}
                                                        />
                                                    )}
                                                </TableCell>
                                                <TableCell className="py-2">
                                                    <StatusBadge status={event.status} />
                                                </TableCell>
                                                <TableCell className="py-2">
                                                    <div className="flex items-center gap-1.5">
                                                        <SeverityIcon severity={event.severity} />
                                                        <SeverityBadge severity={event.severity} />
                                                    </div>
                                                </TableCell>
                                                <TableCell className="font-medium text-sm py-2">{tagName}</TableCell>
                                                <TableCell className="py-2">
                                                    <Badge variant="outline" className="text-xs">{event.alarm_type}</Badge>
                                                </TableCell>
                                                <TableCell className="text-sm py-2 max-w-[200px] truncate">{event.message}</TableCell>
                                                <TableCell className="font-mono text-xs py-2">
                                                    {event.value_at_trigger?.toFixed(2) ?? '—'}
                                                </TableCell>
                                                <TableCell className="py-2">
                                                    <div className="flex flex-col gap-0.5">
                                                        <span className="text-xs font-medium">{relativeTime(event.trigger_time)}</span>
                                                        <span className="text-[10px] text-muted-foreground">{absoluteTime(event.trigger_time)}</span>
                                                    </div>
                                                </TableCell>
                                                <TableCell className="py-2 text-right">
                                                    {isACK && (
                                                        <Button
                                                            size="sm" variant="outline"
                                                            onClick={() => handleAcknowledge(event.id)}
                                                            disabled={ackingIds.has(event.id)}
                                                            className="h-7 gap-1 text-xs clip-chamfer-sm"
                                                        >
                                                            {ackingIds.has(event.id)
                                                                ? <RefreshCw size={11} className="animate-spin" />
                                                                : <CheckCircle2 size={11} />}
                                                            ACK
                                                        </Button>
                                                    )}
                                                    {event.status === 'ACKNOWLEDGED' && event.bg_ack_user && (
                                                        <span className="text-[10px] text-muted-foreground flex items-center gap-1">
                                                            <BellOff size={10} />
                                                            {event.bg_ack_user}
                                                        </span>
                                                    )}
                                                </TableCell>
                                            </TableRow>
                                        );
                                    })}
                                </TableBody>
                            </Table>
                        </TabsContent>

                        {/* ── History tab ── */}
                        <TabsContent value="history" className="mt-0">
                            {/* Filter toolbar */}
                            <div className="flex items-center gap-3 p-3 border-b bg-muted/20 flex-wrap sticky top-0 z-10 backdrop-blur-sm">
                                <div className="relative min-w-[200px] flex-1 max-w-sm">
                                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                                    <Input
                                        placeholder="Cerca tag, messaggio..."
                                        value={searchQuery}
                                        onChange={e => setSearchQuery(e.target.value)}
                                        className="pl-8 h-8 text-sm"
                                    />
                                </div>

                                <Select value={severityFilter} onValueChange={v => { setSeverityFilter(v); setHistoryPage(1); }}>
                                    <SelectTrigger className="h-8 w-36 text-xs">
                                        <Filter size={12} className="mr-1 shrink-0" />
                                        <SelectValue placeholder="Gravità" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {SEVERITY_OPTIONS.map(s => (
                                            <SelectItem key={s} value={s} className="text-xs capitalize">
                                                {s === 'all' ? 'Tutte le gravità' : s}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>

                                <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                                    <Input type="date" value={dateFrom} onChange={e => { setDateFrom(e.target.value); setHistoryPage(1); }} className="h-8 w-36 text-xs" />
                                    <span>→</span>
                                    <Input type="date" value={dateTo} onChange={e => { setDateTo(e.target.value); setHistoryPage(1); }} className="h-8 w-36 text-xs" />
                                </div>

                                {(searchQuery || dateFrom || dateTo || severityFilter !== 'all') && (
                                    <Button variant="ghost" size="sm" className="h-8 text-xs"
                                        onClick={() => { setSearchQuery(''); setDateFrom(''); setDateTo(''); setSeverityFilter('all'); setHistoryPage(1); }}>
                                        Reset filtri
                                    </Button>
                                )}

                                <div className="ml-auto flex items-center gap-2">
                                    <span className="text-xs text-muted-foreground">{filteredHistory.length} eventi</span>
                                    <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs"
                                        onClick={handleExportCSV} disabled={filteredHistory.length === 0}>
                                        <Download size={13} /> CSV
                                    </Button>
                                    {isAdmin() && (
                                        <Button variant="outline" size="sm"
                                            className="h-8 gap-1.5 text-xs text-red-500 hover:text-red-600 hover:bg-red-50"
                                            onClick={() => setConfirmClear(true)} disabled={history.length === 0}>
                                            <Trash2 size={13} /> Elimina tutto
                                        </Button>
                                    )}
                                </div>
                            </div>

                            <Table>
                                <TableHeader>
                                    <TableRow className="bg-muted/30">
                                        <TableHead className="text-xs">Stato</TableHead>
                                        <TableHead className="text-xs">Gravità</TableHead>
                                        <TableHead className="text-xs">Tag</TableHead>
                                        <TableHead className="text-xs">Tipo</TableHead>
                                        <TableHead className="text-xs">Messaggio</TableHead>
                                        <TableHead className="text-xs">Valore</TableHead>
                                        <TableHead className="text-xs">Scatto</TableHead>
                                        <TableHead className="text-xs">Rientro</TableHead>
                                        <TableHead className="text-xs">Ack</TableHead>
                                        {isAdmin() && <TableHead className="text-xs w-10"></TableHead>}
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {isLoading ? (
                                        <TableSkeletonRows cols={isAdmin() ? 10 : 9} />
                                    ) : filteredHistory.length === 0 ? (
                                        <TableRow>
                                            <TableCell colSpan={10} className="text-center py-12">
                                                <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                                    <BellOff className="w-8 h-8 opacity-30" />
                                                    <p className="font-medium">Nessun evento trovato</p>
                                                    {(searchQuery || dateFrom || dateTo) && (
                                                        <p className="text-xs">Prova a modificare i filtri</p>
                                                    )}
                                                </div>
                                            </TableCell>
                                        </TableRow>
                                    ) : pagedHistory.map((event, idx) => {
                                        const tag     = tagsMap[event.tag_id];
                                        const tagName = tag ? (tag.alias || tag.code) : `ID: ${event.tag_id}`;
                                        return (
                                            <TableRow key={`${event.id}-${idx}`} className="text-sm">
                                                <TableCell className="py-2"><StatusBadge status={event.status} /></TableCell>
                                                <TableCell className="py-2">
                                                    <div className="flex items-center gap-1.5">
                                                        <SeverityIcon severity={event.severity} />
                                                        <SeverityBadge severity={event.severity} />
                                                    </div>
                                                </TableCell>
                                                <TableCell className="font-medium py-2">{tagName}</TableCell>
                                                <TableCell className="py-2">
                                                    <Badge variant="outline" className="text-xs">{event.alarm_type}</Badge>
                                                </TableCell>
                                                <TableCell className="py-2 max-w-[180px] truncate">{event.message}</TableCell>
                                                <TableCell className="font-mono text-xs py-2">
                                                    {event.value_at_trigger?.toFixed(2) ?? '—'}
                                                </TableCell>
                                                <TableCell className="py-2">
                                                    <div className="flex flex-col gap-0.5">
                                                        <span className="text-xs">{relativeTime(event.trigger_time)}</span>
                                                        <span className="text-[10px] text-muted-foreground">{absoluteTime(event.trigger_time)}</span>
                                                    </div>
                                                </TableCell>
                                                <TableCell className="py-2 text-xs text-muted-foreground">
                                                    {event.clear_time ? relativeTime(event.clear_time) : '—'}
                                                </TableCell>
                                                <TableCell className="py-2">
                                                    {event.bg_ack_user ? (
                                                        <div className="flex flex-col gap-0.5">
                                                            <span className="text-xs font-medium">{event.bg_ack_user}</span>
                                                            <span className="text-[10px] text-muted-foreground">{relativeTime(event.ack_time)}</span>
                                                        </div>
                                                    ) : <span className="text-xs text-muted-foreground">—</span>}
                                                </TableCell>
                                                {isAdmin() && (
                                                    <TableCell className="py-2">
                                                        <Button
                                                            size="icon" variant="ghost"
                                                            onClick={() => setConfirmDelete(event.id)}
                                                            className="h-7 w-7 text-muted-foreground hover:text-red-600 hover:bg-red-50"
                                                        >
                                                            <Trash2 size={13} />
                                                        </Button>
                                                    </TableCell>
                                                )}
                                            </TableRow>
                                        );
                                    })}
                                </TableBody>
                            </Table>

                            {/* Load more */}
                            {pagedHistory.length < filteredHistory.length && (
                                <div className="p-4 text-center border-t">
                                    <Button variant="outline" size="sm" className="clip-chamfer-sm"
                                        onClick={() => setHistoryPage(p => p + 1)}>
                                        Carica altri ({filteredHistory.length - pagedHistory.length} rimasti)
                                    </Button>
                                </div>
                            )}
                        </TabsContent>
                    </CardContent>
                </Tabs>
            </Card>
        </div>
    );
}
