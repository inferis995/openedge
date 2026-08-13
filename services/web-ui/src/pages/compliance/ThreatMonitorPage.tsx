import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { CheckCircle, Plus, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter
} from '@/components/ui/dialog';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue
} from '@/components/ui/select';
import { complianceApi, ThreatEvent, CreateThreatRequest } from '@/api/compliance';
import { useAuthStore } from '@/stores/useAuthStore';

const EVENT_TYPE_LABELS: Record<ThreatEvent['event_type'], string> = {
    new_device: 'Nuovo Dispositivo',
    protocol_anomaly: 'Anomalia Protocollo',
    config_change: 'Cambio Configurazione',
    vulnerability_alert: 'Alert Vulnerabilità',
    unauthorized_access: 'Accesso Non Autorizzato',
    cve_published: 'CVE Pubblicata',
    scan_completed: 'Scansione Completata',
};

const SEVERITY_STYLES: Record<ThreatEvent['severity'], { border: string; bg: string; badge: string }> = {
    critical: { border: 'border-l-red-500', bg: 'bg-red-500/5', badge: 'bg-red-500 text-white' },
    high: { border: 'border-l-orange-500', bg: 'bg-orange-500/5', badge: 'bg-orange-500 text-white' },
    medium: { border: 'border-l-yellow-500', bg: 'bg-yellow-500/5', badge: 'bg-yellow-500 text-white' },
    low: { border: 'border-l-blue-500', bg: 'bg-blue-500/5', badge: 'bg-blue-500 text-white' },
    info: { border: 'border-l-slate-400', bg: 'bg-slate-500/5', badge: 'bg-slate-400 text-white' },
};

const SEVERITY_LABELS: Record<ThreatEvent['severity'], string> = {
    critical: 'Critico',
    high: 'Alto',
    medium: 'Medio',
    low: 'Basso',
    info: 'Info',
};

function relativeTime(dateStr: string): string {
    try {
        const diff = Date.now() - new Date(dateStr).getTime();
        const mins = Math.floor(diff / 60000);
        if (mins < 1) return 'ora';
        if (mins < 60) return `${mins}m fa`;
        const hours = Math.floor(mins / 60);
        if (hours < 24) return `${hours}h fa`;
        const days = Math.floor(hours / 24);
        return `${days}g fa`;
    } catch {
        return dateStr;
    }
}

export default function ThreatMonitorPage() {
    const qc = useQueryClient();
    const { isAdmin } = useAuthStore();

    const [severityFilter, setSeverityFilter] = useState<string>('all');
    const [statusFilter, setStatusFilter] = useState<string>('all');
    const [search, setSearch] = useState('');
    const [addOpen, setAddOpen] = useState(false);
    const [addForm, setAddForm] = useState<Partial<CreateThreatRequest>>({
        event_type: 'new_device',
        severity: 'medium',
        title: '',
        description: '',
        ip_address: '',
        source: '',
    });

    const { data: events = [], isLoading, isFetching } = useQuery({
        queryKey: ['threat-events'],
        queryFn: () => complianceApi.listThreats({ limit: 200 }),
        refetchInterval: 30000,
    });

    const { data: summary } = useQuery({
        queryKey: ['threat-summary'],
        queryFn: () => complianceApi.getThreatSummary(),
        refetchInterval: 30000,
    });

    const resolveEvent = useMutation({
        mutationFn: (id: number) => complianceApi.resolveThreat(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['threat-events'] });
            qc.invalidateQueries({ queryKey: ['threat-summary'] });
            toast.success('Evento risolto');
        },
        onError: () => toast.error('Errore durante la risoluzione'),
    });

    const createEvent = useMutation({
        mutationFn: (data: CreateThreatRequest) => complianceApi.createThreat(data),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['threat-events'] });
            qc.invalidateQueries({ queryKey: ['threat-summary'] });
            setAddOpen(false);
            toast.success('Evento aggiunto');
        },
        onError: () => toast.error('Errore durante la creazione'),
    });

    const filtered = events.filter(e => {
        if (severityFilter !== 'all' && e.severity !== severityFilter) return false;
        if (statusFilter === 'unresolved' && e.resolved) return false;
        if (statusFilter === 'resolved' && !e.resolved) return false;
        if (search && !e.title.toLowerCase().includes(search.toLowerCase()) &&
            !e.description.toLowerCase().includes(search.toLowerCase()) &&
            !e.ip_address.includes(search)) return false;
        return true;
    });

    const criticalCount = summary?.by_severity?.['critical'] ?? 0;
    const unresolved = summary?.unresolved ?? 0;

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Monitoraggio Minacce</h2>
                    <p className="text-muted-foreground">Rilevamento anomalie e accessi non autorizzati in tempo reale</p>
                </div>
                <div className="flex items-center gap-2">
                    {isFetching && <RefreshCw size={14} className="animate-spin text-muted-foreground" />}
                    {isAdmin() && (
                        <Button className="gap-2" onClick={() => setAddOpen(true)}>
                            <Plus size={16} /> Aggiungi Evento
                        </Button>
                    )}
                </div>
            </div>

            {/* Stat Cards */}
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Totale Eventi</p>
                    <p className="text-3xl font-bold mt-1">{summary?.total ?? 0}</p>
                </div>
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Non Risolti</p>
                    <p className={`text-3xl font-bold mt-1 ${unresolved > 0 ? 'text-red-500' : ''}`}>{unresolved}</p>
                </div>
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Ultime 24h</p>
                    <p className="text-3xl font-bold mt-1">{summary?.last_24h ?? 0}</p>
                </div>
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Critici</p>
                    <p className={`text-3xl font-bold mt-1 ${criticalCount > 0 ? 'text-red-600' : ''}`}>{criticalCount}</p>
                </div>
            </div>

            {/* Filter Bar */}
            <div className="flex flex-wrap items-center gap-3">
                <Select value={severityFilter} onValueChange={setSeverityFilter}>
                    <SelectTrigger className="w-36 h-8 text-sm">
                        <SelectValue placeholder="Severity" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">Tutti</SelectItem>
                        <SelectItem value="critical">Critico</SelectItem>
                        <SelectItem value="high">Alto</SelectItem>
                        <SelectItem value="medium">Medio</SelectItem>
                        <SelectItem value="low">Basso</SelectItem>
                        <SelectItem value="info">Info</SelectItem>
                    </SelectContent>
                </Select>

                <Select value={statusFilter} onValueChange={setStatusFilter}>
                    <SelectTrigger className="w-36 h-8 text-sm">
                        <SelectValue placeholder="Stato" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">Tutti</SelectItem>
                        <SelectItem value="unresolved">Non Risolti</SelectItem>
                        <SelectItem value="resolved">Risolti</SelectItem>
                    </SelectContent>
                </Select>

                <div className="relative flex-1 max-w-sm">
                    <Input
                        className="h-8 text-sm"
                        placeholder="Cerca per titolo, IP..."
                        value={search}
                        onChange={e => setSearch(e.target.value)}
                    />
                </div>

                <p className="text-xs text-muted-foreground ml-auto">
                    {filtered.length} eventi · aggiornamento auto ogni 30s
                </p>
            </div>

            {/* Event Cards */}
            {isLoading ? (
                <div className="text-center py-12 text-muted-foreground">Caricamento eventi...</div>
            ) : filtered.length === 0 ? (
                <div className="flex flex-col items-center gap-3 py-16 text-muted-foreground">
                    <CheckCircle size={48} className="text-green-500 opacity-60" />
                    <p>Nessuna minaccia rilevata</p>
                </div>
            ) : (
                <div className="space-y-3">
                    {filtered.map(event => {
                        const style = SEVERITY_STYLES[event.severity];
                        return (
                            <div
                                key={event.id}
                                className={`border-l-4 ${style.border} ${style.bg} border border-border clip-chamfer p-4 space-y-2`}
                            >
                                <div className="flex items-start gap-3">
                                    <div className="flex flex-wrap items-center gap-2 flex-1 min-w-0">
                                        <span className={`inline-flex items-center rounded-none clip-chamfer-sm px-2 py-0.5 text-xs font-bold uppercase ${style.badge}`}>
                                            {SEVERITY_LABELS[event.severity]}
                                        </span>
                                        <Badge variant="outline" className="text-xs">{EVENT_TYPE_LABELS[event.event_type]}</Badge>
                                        <span className="font-semibold text-sm truncate">{event.title}</span>
                                    </div>
                                    <div className="flex items-center gap-2 shrink-0">
                                        {event.resolved ? (
                                            <Badge variant="success" className="gap-1 text-xs">
                                                <CheckCircle size={10} /> Risolto
                                            </Badge>
                                        ) : (
                                            <Button
                                                variant="outline"
                                                size="sm"
                                                className="h-7 text-xs"
                                                onClick={() => resolveEvent.mutate(event.id)}
                                                disabled={resolveEvent.isPending}
                                            >
                                                Risolvi
                                            </Button>
                                        )}
                                    </div>
                                </div>
                                {event.description && (
                                    <p className="text-sm text-muted-foreground">{event.description}</p>
                                )}
                                <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
                                    {event.source && <span>Fonte: <span className="text-foreground">{event.source}</span></span>}
                                    {event.ip_address && <span>IP: <span className="font-mono text-foreground">{event.ip_address}</span></span>}
                                    <span className="ml-auto">{relativeTime(event.created_at)}</span>
                                </div>
                            </div>
                        );
                    })}
                </div>
            )}

            {/* Add Event Dialog */}
            <Dialog open={addOpen} onOpenChange={setAddOpen}>
                <DialogContent className="max-w-md">
                    <DialogHeader>
                        <DialogTitle>Aggiungi Evento di Minaccia</DialogTitle>
                    </DialogHeader>
                    <div className="grid gap-4 py-2">
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="grid gap-2">
                                <Label>Tipo Evento</Label>
                                <Select
                                    value={addForm.event_type}
                                    onValueChange={v => setAddForm(p => ({ ...p, event_type: v as ThreatEvent['event_type'] }))}
                                >
                                    <SelectTrigger><SelectValue /></SelectTrigger>
                                    <SelectContent>
                                        {(Object.entries(EVENT_TYPE_LABELS) as [ThreatEvent['event_type'], string][]).map(([k, v]) => (
                                            <SelectItem key={k} value={k}>{v}</SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>
                            <div className="grid gap-2">
                                <Label>Severity</Label>
                                <Select
                                    value={addForm.severity}
                                    onValueChange={v => setAddForm(p => ({ ...p, severity: v as ThreatEvent['severity'] }))}
                                >
                                    <SelectTrigger><SelectValue /></SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="critical">Critico</SelectItem>
                                        <SelectItem value="high">Alto</SelectItem>
                                        <SelectItem value="medium">Medio</SelectItem>
                                        <SelectItem value="low">Basso</SelectItem>
                                        <SelectItem value="info">Info</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>
                        </div>
                        <div className="grid gap-2">
                            <Label>Titolo</Label>
                            <Input
                                value={addForm.title}
                                onChange={e => setAddForm(p => ({ ...p, title: e.target.value }))}
                                placeholder="Descrizione breve dell'evento..."
                            />
                        </div>
                        <div className="grid gap-2">
                            <Label>Descrizione</Label>
                            <textarea
                                className="flex min-h-[70px] w-full rounded-none clip-chamfer-sm border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 resize-none"
                                value={addForm.description}
                                onChange={e => setAddForm(p => ({ ...p, description: e.target.value }))}
                                placeholder="Dettagli dell'evento..."
                            />
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="grid gap-2">
                                <Label>Indirizzo IP</Label>
                                <Input
                                    value={addForm.ip_address}
                                    onChange={e => setAddForm(p => ({ ...p, ip_address: e.target.value }))}
                                    placeholder="192.168.1.50"
                                />
                            </div>
                            <div className="grid gap-2">
                                <Label>Fonte</Label>
                                <Input
                                    value={addForm.source}
                                    onChange={e => setAddForm(p => ({ ...p, source: e.target.value }))}
                                    placeholder="IDS, SIEM, manuale..."
                                />
                            </div>
                        </div>
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setAddOpen(false)}>Annulla</Button>
                        <Button
                            onClick={() => {
                                if (addForm.event_type && addForm.title) {
                                    createEvent.mutate(addForm as CreateThreatRequest);
                                }
                            }}
                            disabled={!addForm.title || createEvent.isPending}
                        >
                            Aggiungi
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
