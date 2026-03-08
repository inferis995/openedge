import { useState, useEffect, useMemo } from 'react';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { CheckCircle2, AlertTriangle, AlertCircle, Info, RefreshCw, Search, Download, Trash2 } from 'lucide-react';
import { alarmsApi, AlarmEvent } from '@/api/alarms';
import { tagsApi } from '@/api/tags';
import { Tag } from '@/types';
import { toast } from 'sonner';
import { useAuthStore } from '@/stores/useAuthStore';

export default function AlarmsPage() {
    const { isAdmin } = useAuthStore();
    const [activeAlarms, setActiveAlarms] = useState<AlarmEvent[]>([]);
    const [history, setHistory] = useState<AlarmEvent[]>([]);
    const [tagsMap, setTagsMap] = useState<Record<number, Tag>>({});
    const [isLoading, setIsLoading] = useState(true);

    // Search and filter state
    const [searchQuery, setSearchQuery] = useState('');
    const [dateFrom, setDateFrom] = useState('');
    const [dateTo, setDateTo] = useState('');

    const loadData = async () => {
        setIsLoading(true);
        try {
            // Load tags first to create lookup map
            const tags = await tagsApi.getAllTags();
            const tMap: Record<number, Tag> = {};
            tags.forEach(t => tMap[t.id] = t);
            setTagsMap(tMap);

            // Fetch alarms
            const [active, hist] = await Promise.all([
                alarmsApi.getActiveAlarms(),
                alarmsApi.getAlarmHistory(500)
            ]);

            setActiveAlarms(active || []);
            setHistory(hist || []);
        } catch (error) {
            console.error('Failed to load alarms:', error);
            toast.error('Failed to load alarms');
        } finally {
            setIsLoading(false);
        }
    };

    useEffect(() => {
        loadData();
        // Setup polling every 30 seconds
        const interval = setInterval(loadData, 30000);
        return () => clearInterval(interval);
    }, []);

    // Filtered history based on search and date
    const filteredHistory = useMemo(() => {
        let filtered = history;

        // Filter by search query (tag name or message)
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

        // Filter by date range
        if (dateFrom) {
            const from = new Date(dateFrom);
            filtered = filtered.filter(event => new Date(event.trigger_time) >= from);
        }
        if (dateTo) {
            const to = new Date(dateTo + 'T23:59:59');
            filtered = filtered.filter(event => new Date(event.trigger_time) <= to);
        }

        return filtered;
    }, [history, searchQuery, dateFrom, dateTo, tagsMap]);

    const handleAcknowledge = async (id: number) => {
        try {
            await alarmsApi.acknowledgeAlarm(id);
            toast.success('Allarme riconosciuto');
            loadData(); // Reload to update status
        } catch (error) {
            console.error('Failed to ack alarm:', error);
            toast.error('Errore nel riconoscimento allarme');
        }
    };

    const handleDeleteHistory = async (id: number) => {
        if (!window.confirm("Sei sicuro di voler eliminare definitivamente questo evento dallo storico?")) return;
        try {
            await alarmsApi.deleteAlarmHistory(id);
            toast.success("Evento eliminato dallo storico");
            loadData();
        } catch (error) {
            console.error('Failed to delete history event:', error);
            toast.error("Errore durante l'eliminazione");
        }
    };

    const handleClearAllHistory = async () => {
        if (!window.confirm("Sei sicuro di voler SVUOTARE COMPLETAMENTE lo storico allarmi? Questa operazione non è reversibile.")) return;
        try {
            await alarmsApi.deleteAllAlarmHistory();
            toast.success("Storico allarmi svuotato");
            loadData();
        } catch (error) {
            console.error('Failed to clear history:', error);
            toast.error("Errore durante lo svuotamento");
        }
    };

    const handleExportCSV = () => {
        try {
            const headers = ['Stato', 'Gravità', 'Tag', 'Tipo', 'Messaggio', 'Valore', 'Scatto', 'Rientro', 'Riconosciuto da', 'Ack Time'];
            const rows = filteredHistory.map(e => {
                const tag = tagsMap[e.tag_id];
                const tagName = tag ? (tag.alias || tag.code) : `ID: ${e.tag_id}`;
                return [
                    `"${e.status}"`,
                    `"${e.severity}"`,
                    `"${tagName}"`,
                    `"${e.alarm_type}"`,
                    `"${(e.message || '').replace(/"/g, '""')}"`,
                    `"${e.value_at_trigger?.toFixed(2) ?? ''}"`,
                    `"${formatTime(e.trigger_time)}"`,
                    `"${formatTime(e.clear_time)}"`,
                    `"${e.bg_ack_user || ''}"`,
                    `"${formatTime(e.ack_time)}"`
                ].join(',');
            });

            const csvContent = "data:text/csv;charset=utf-8,\uFEFF" + [headers.join(','), ...rows].join('\n');
            const encodedUri = encodeURI(csvContent);
            const link = document.createElement("a");
            link.setAttribute("href", encodedUri);
            link.setAttribute("download", `allarmi_storico_${new Date().toISOString().split('T')[0]}.csv`);
            document.body.appendChild(link);
            link.click();
            document.body.removeChild(link);
            toast.success("Esportazione CSV completata");
        } catch (err) {
            console.error("Export error", err);
            toast.error("Errore nell'esportazione CSV");
        }
    };

    const formatTime = (ts: string | null) => {
        if (!ts) return '-';
        return new Date(ts).toLocaleString('it-IT');
    };

    const getSeverityIcon = (severity: string) => {
        switch (severity.toLowerCase()) {
            case 'critical': return <AlertTriangle className="h-4 w-4 text-red-500" />;
            case 'warning': return <AlertCircle className="h-4 w-4 text-orange-500" />;
            case 'info': return <Info className="h-4 w-4 text-blue-500" />;
            default: return <AlertCircle className="h-4 w-4" />;
        }
    };

    const getStatusBadge = (status: string) => {
        switch (status) {
            case 'ACTIVE': return <Badge variant="destructive">ATTIVO</Badge>;
            case 'ACKNOWLEDGED': return <Badge variant="secondary" className="bg-orange-500/20 text-orange-500">RICONOSCIUTO</Badge>;
            case 'CLEARED': return <Badge variant="outline" className="text-green-500 border-green-500">RIENTRATO</Badge>;
            default: return <Badge variant="outline">{status}</Badge>;
        }
    };

    const renderEventsTable = (events: AlarmEvent[], tabType: 'active' | 'history') => {
        const showActions = tabType === 'active';
        const showHistoryActions = tabType === 'history' && isAdmin();

        return (
            <Table>
                <TableHeader>
                    <TableRow>
                        <TableHead>Stato</TableHead>
                        <TableHead>Gravità</TableHead>
                        <TableHead>Tag</TableHead>
                        <TableHead>Tipo</TableHead>
                        <TableHead>Messaggio</TableHead>
                        <TableHead>Valore</TableHead>
                        <TableHead>Data/Ora Scatto</TableHead>
                        {tabType === 'history' && <TableHead>Data/Ora Rientro</TableHead>}
                        {tabType === 'history' && <TableHead>Riconosciuto da</TableHead>}
                        {(showActions || showHistoryActions) && <TableHead className="text-right">Azioni</TableHead>}
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {events.length === 0 ? (
                        <TableRow>
                            <TableCell colSpan={10} className="text-center py-8 text-muted-foreground">
                                Nessun allarme presente
                            </TableCell>
                        </TableRow>
                    ) : (
                        events.map((event, idx) => {
                            const tag = tagsMap[event.tag_id];
                            const tagName = tag ? (tag.alias || tag.code) : `ID: ${event.tag_id}`;

                            return (
                                <TableRow key={`${event.id}-${idx}`}>
                                    <TableCell>{getStatusBadge(event.status)}</TableCell>
                                    <TableCell>
                                        <div className="flex items-center gap-2">
                                            {getSeverityIcon(event.severity)}
                                            <span className="capitalize">{event.severity}</span>
                                        </div>
                                    </TableCell>
                                    <TableCell className="font-medium">{tagName}</TableCell>
                                    <TableCell>
                                        <Badge variant="outline" className="text-xs">{event.alarm_type}</Badge>
                                    </TableCell>
                                    <TableCell>{event.message}</TableCell>
                                    <TableCell>{event.value_at_trigger?.toFixed(2) ?? '-'}</TableCell>
                                    <TableCell>{formatTime(event.trigger_time)}</TableCell>
                                    {tabType === 'history' && <TableCell>{formatTime(event.clear_time)}</TableCell>}
                                    {tabType === 'history' && (
                                        <TableCell>
                                            <div className="text-xs">
                                                {event.bg_ack_user || '-'}
                                                {event.bg_ack_user && <br />}
                                                {formatTime(event.ack_time)}
                                            </div>
                                        </TableCell>
                                    )}
                                    {(showActions || showHistoryActions) && (
                                        <TableCell className="text-right">
                                            {showActions && event.status === 'ACTIVE' && (
                                                <Button
                                                    size="sm"
                                                    variant="outline"
                                                    onClick={() => handleAcknowledge(event.id)}
                                                    className="h-8 gap-1"
                                                >
                                                    <CheckCircle2 size={14} /> Ack
                                                </Button>
                                            )}
                                            {showHistoryActions && (
                                                <Button
                                                    size="sm"
                                                    variant="outline"
                                                    onClick={() => handleDeleteHistory(event.id)}
                                                    className="h-8 w-8 p-0 text-red-500 hover:text-red-600 hover:bg-red-50"
                                                    title="Elimina"
                                                >
                                                    <Trash2 size={14} />
                                                </Button>
                                            )}
                                        </TableCell>
                                    )}
                                </TableRow>
                            );
                        })
                    )}
                </TableBody>
            </Table>
        );
    };

    return (
        <div className="h-full flex flex-col p-6 overflow-hidden">
            <div className="flex items-center justify-between mb-6">
                <div>
                    <h1 className="text-3xl font-bold tracking-tight">Allarmi e Notifiche</h1>
                    <p className="text-muted-foreground mt-1">
                        Monitora gli allarmi in corso e lo storico degli eventi.
                    </p>
                </div>
                <Button variant="outline" size="icon" onClick={loadData} disabled={isLoading}>
                    <RefreshCw className={`h-4 w-4 ${isLoading ? 'animate-spin' : ''}`} />
                </Button>
            </div>

            <Card className="flex-1 overflow-hidden flex flex-col shadow-md border-border/50">
                <Tabs defaultValue="active" className="h-full flex flex-col">
                    <CardHeader className="pb-3 flex flex-row items-center justify-between space-y-0">
                        <TabsList className="bg-muted">
                            <TabsTrigger value="active" className="gap-2 px-6">
                                <AlertTriangle size={16} className="text-red-500" />
                                Allarmi Attivi
                                {activeAlarms.length > 0 && (
                                    <Badge variant="destructive" className="ml-1 px-1.5 py-0 h-5 text-xs rounded-full">
                                        {activeAlarms.length}
                                    </Badge>
                                )}
                            </TabsTrigger>
                            <TabsTrigger value="history" className="gap-2 px-6">
                                <Info size={16} />
                                Storico Allarmi
                                {history.length > 0 && (
                                    <Badge variant="secondary" className="ml-1 px-1.5 py-0 h-5 text-xs rounded-full">
                                        {history.length}
                                    </Badge>
                                )}
                            </TabsTrigger>
                        </TabsList>
                    </CardHeader>

                    <CardContent className="flex-1 overflow-auto p-0">
                        <TabsContent value="active" className="mt-0">
                            {renderEventsTable(activeAlarms, 'active')}
                        </TabsContent>

                        <TabsContent value="history" className="mt-0">
                            {/* Search and Date Filter Bar */}
                            <div className="flex items-center gap-3 p-4 border-b bg-muted/30">
                                <div className="relative flex-1 max-w-sm">
                                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                                    <Input
                                        placeholder="Cerca per nome tag, messaggio..."
                                        value={searchQuery}
                                        onChange={(e) => setSearchQuery(e.target.value)}
                                        className="pl-9 h-9"
                                    />
                                </div>
                                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                                    <span>Dal:</span>
                                    <Input
                                        type="date"
                                        value={dateFrom}
                                        onChange={(e) => setDateFrom(e.target.value)}
                                        className="h-9 w-40"
                                    />
                                    <span>Al:</span>
                                    <Input
                                        type="date"
                                        value={dateTo}
                                        onChange={(e) => setDateTo(e.target.value)}
                                        className="h-9 w-40"
                                    />
                                    {(searchQuery || dateFrom || dateTo) && (
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            className="h-9 px-2"
                                            onClick={() => { setSearchQuery(''); setDateFrom(''); setDateTo(''); }}
                                        >
                                            Reset
                                        </Button>
                                    )}
                                </div>
                                <div className="flex items-center gap-4 ml-auto">
                                    <span className="text-xs text-muted-foreground">
                                        {filteredHistory.length} risultati
                                    </span>
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        className="h-9 gap-2"
                                        onClick={handleExportCSV}
                                        disabled={filteredHistory.length === 0}
                                    >
                                        <Download size={16} /> Scarica CSV
                                    </Button>
                                    {isAdmin() && (
                                        <Button
                                            variant="outline"
                                            size="sm"
                                            className="h-9 gap-2 text-red-500 hover:text-red-600 hover:bg-red-50"
                                            onClick={handleClearAllHistory}
                                            disabled={history.length === 0}
                                        >
                                            <Trash2 size={16} /> Elimina Tutto
                                        </Button>
                                    )}
                                </div>
                            </div>
                            {renderEventsTable(filteredHistory, 'history')}
                        </TabsContent>
                    </CardContent>
                </Tabs>
            </Card>
        </div>
    );
}
