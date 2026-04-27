import { useState, useEffect, useCallback, KeyboardEvent } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import {
    Network,
    RefreshCw,
    ChevronRight,
    Cpu,
    Tag,
    AlertTriangle,
    AlertCircle,
    Info,
    CheckCircle2,
    Clock,
    Zap,
    BookOpen,
    ArrowRight,
    Pencil,
    Check,
    X,
    Send,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { i3xApi, I3XEquipment, I3XProperty, I3XAlarm } from '@/api/i3x';
import { useAuthStore } from '@/stores/useAuthStore';
import { toast } from 'sonner';

// ─── Quality helpers ────────────────────────────────────────────────────────

function QualityBadge({ quality }: { quality?: number }) {
    if (quality === undefined || quality === null) {
        return <Badge variant="outline" className="text-muted-foreground text-xs">No data</Badge>;
    }
    if (quality >= 192) {
        return (
            <Badge className="bg-green-500/15 text-green-600 border-green-500/30 text-xs font-mono">
                <span className="mr-1 inline-block w-2 h-2 rounded-full bg-green-500 animate-pulse" />
                Good
            </Badge>
        );
    }
    if (quality >= 64) {
        return (
            <Badge className="bg-yellow-500/15 text-yellow-600 border-yellow-500/30 text-xs font-mono">
                <span className="mr-1 inline-block w-2 h-2 rounded-full bg-yellow-500" />
                Uncertain
            </Badge>
        );
    }
    return (
        <Badge className="bg-red-500/15 text-red-600 border-red-500/30 text-xs font-mono">
            <span className="mr-1 inline-block w-2 h-2 rounded-full bg-red-500" />
            Bad
        </Badge>
    );
}

function DataTypeBadge({ type }: { type: string }) {
    const map: Record<string, string> = {
        Float:   'bg-blue-500/10 text-blue-600 border-blue-500/20',
        Int32:   'bg-purple-500/10 text-purple-600 border-purple-500/20',
        Boolean: 'bg-orange-500/10 text-orange-600 border-orange-500/20',
        String:  'bg-slate-500/10 text-slate-600 border-slate-500/20',
    };
    return (
        <Badge variant="outline" className={cn('text-xs font-mono', map[type] ?? '')}>
            {type}
        </Badge>
    );
}

function SeverityIcon({ severity }: { severity: string }) {
    if (severity === 'Critical') return <AlertCircle size={16} className="text-red-500" />;
    if (severity === 'Warning')  return <AlertTriangle size={16} className="text-yellow-500" />;
    return <Info size={16} className="text-blue-500" />;
}

function formatValue(v: unknown): string {
    if (v === null || v === undefined) return '—';
    if (typeof v === 'boolean') return v ? 'TRUE' : 'FALSE';
    if (typeof v === 'number') return v.toFixed(4).replace(/\.?0+$/, '');
    return String(v);
}

function formatTs(ts?: string): string {
    if (!ts) return '—';
    return new Date(ts).toLocaleTimeString();
}

// ─── API Reference entries ───────────────────────────────────────────────────

const API_ENDPOINTS = [
    { method: 'GET',  path: '/api/i3x/v1/equipment',                          desc: 'Lista di tutti i gateway (Equipment)' },
    { method: 'GET',  path: '/api/i3x/v1/equipment/:id',                      desc: 'Dettaglio singolo gateway' },
    { method: 'GET',  path: '/api/i3x/v1/equipment/:id/properties',           desc: 'Tag del gateway con valore live' },
    { method: 'GET',  path: '/api/i3x/v1/equipment/:id/properties/:propId',   desc: 'Singola property del gateway' },
    { method: 'GET',  path: '/api/i3x/v1/properties',                         desc: 'Tutti i tag (cross-gateway)' },
    { method: 'GET',  path: '/api/i3x/v1/properties/:id',                     desc: 'Singola property per ID i3X' },
    { method: 'PUT',  path: '/api/i3x/v1/properties/:id/value',               desc: 'Scrittura valore via MQTT' },
    { method: 'GET',  path: '/api/i3x/v1/alarms',                             desc: 'Allarmi attivi' },
    { method: 'GET',  path: '/api/i3x/v1/alarms/history',                     desc: 'Storico allarmi (limit & offset)' },
];

const METHOD_COLOR: Record<string, string> = {
    GET: 'bg-green-500/15 text-green-700 border-green-500/30',
    PUT: 'bg-orange-500/15 text-orange-700 border-orange-500/30',
};

// ─── Main page ───────────────────────────────────────────────────────────────

export default function I3XPage() {
    const { canI3xWrite } = useAuthStore();
    const writeAllowed = canI3xWrite();

    const [equipment, setEquipment]               = useState<I3XEquipment[]>([]);
    const [selectedEq, setSelectedEq]             = useState<I3XEquipment | null>(null);
    const [properties, setProperties]             = useState<I3XProperty[]>([]);
    const [alarms, setAlarms]                     = useState<I3XAlarm[]>([]);
    const [alarmHistory, setAlarmHistory]         = useState<I3XAlarm[]>([]);
    const [loadingEquipment, setLoadingEquipment] = useState(true);
    const [loadingProps, setLoadingProps]         = useState(false);
    const [loadingAlarms, setLoadingAlarms]       = useState(true);
    const [lastRefresh, setLastRefresh]           = useState<Date>(new Date());

    // Write state
    const [editingPropId, setEditingPropId] = useState<string | null>(null);
    const [editValue, setEditValue]         = useState('');
    const [writing, setWriting]             = useState(false);

    // Load equipment list + active alarms
    const loadBase = useCallback(async () => {
        setLoadingEquipment(true);
        setLoadingAlarms(true);
        try {
            const [eqRes, alRes, histRes] = await Promise.all([
                i3xApi.listEquipment(),
                i3xApi.listAlarms(),
                i3xApi.listAlarmHistory(50),
            ]);
            setEquipment(eqRes.items ?? []);
            setAlarms(alRes.items ?? []);
            setAlarmHistory(histRes.items ?? []);
            setLastRefresh(new Date());
        } catch {
            toast.error('Errore nel caricamento dati i3X');
        } finally {
            setLoadingEquipment(false);
            setLoadingAlarms(false);
        }
    }, []);

    // Load properties when equipment is selected
    const loadProperties = useCallback(async (eq: I3XEquipment) => {
        setLoadingProps(true);
        setProperties([]);
        try {
            const res = await i3xApi.listEquipmentProperties(eq.id);
            setProperties(res.items ?? []);
        } catch {
            toast.error('Errore nel caricamento properties');
        } finally {
            setLoadingProps(false);
        }
    }, []);

    useEffect(() => {
        loadBase();
        const interval = setInterval(loadBase, 30_000);
        return () => clearInterval(interval);
    }, [loadBase]);

    const handleSelectEquipment = (eq: I3XEquipment) => {
        setSelectedEq(eq);
        setEditingPropId(null);
        loadProperties(eq);
    };

    const startEdit = (prop: I3XProperty) => {
        setEditingPropId(prop.id);
        // Pre-fill with current value if available
        const cur = prop.current?.value;
        setEditValue(cur !== null && cur !== undefined ? String(cur) : '');
    };

    const cancelEdit = () => {
        setEditingPropId(null);
        setEditValue('');
    };

    const confirmWrite = async (prop: I3XProperty) => {
        setWriting(true);
        try {
            // Coerce value to the correct type before sending
            let coerced: unknown = editValue;
            if (prop.dataType === 'Float' || prop.dataType === 'Int32') {
                const n = Number(editValue);
                if (isNaN(n)) {
                    toast.error(`Valore non valido per tipo ${prop.dataType}`);
                    return;
                }
                coerced = n;
            } else if (prop.dataType === 'Boolean') {
                const lower = editValue.trim().toLowerCase();
                if (lower !== 'true' && lower !== 'false' && lower !== '1' && lower !== '0') {
                    toast.error('Per Boolean inserisci: true / false / 1 / 0');
                    return;
                }
                coerced = lower === 'true' || lower === '1';
            }

            await i3xApi.writePropertyValue(prop.id, coerced);
            toast.success(`Scrittura inviata a ${prop.name} → ${editValue}`);
            setEditingPropId(null);
            setEditValue('');
            // Refresh properties after a short delay to pick up the new value
            setTimeout(() => selectedEq && loadProperties(selectedEq), 1500);
        } catch {
            toast.error(`Errore nella scrittura di ${prop.name}`);
        } finally {
            setWriting(false);
        }
    };

    const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>, prop: I3XProperty) => {
        if (e.key === 'Enter') confirmWrite(prop);
        if (e.key === 'Escape') cancelEdit();
    };

    const handleRefresh = () => {
        loadBase();
        if (selectedEq) loadProperties(selectedEq);
    };

    const totalProps = equipment.length > 0 ? '—' : '0';

    return (
        <div className="space-y-6">
            {/* ── Header ── */}
            <div className="flex items-start justify-between gap-4">
                <div className="space-y-1">
                    <div className="flex items-center gap-3">
                        <div className="p-2 clip-hex bg-primary/10">
                            <Network size={22} className="text-primary" />
                        </div>
                        <h1 className="text-2xl font-bold tracking-tight">i3X Access API</h1>
                        <Badge className="bg-primary/10 text-primary border-primary/30 text-xs font-mono">
                            CESMII v1
                        </Badge>
                    </div>
                    <p className="text-sm text-muted-foreground ml-14">
                        Interfaccia vendor-neutral per l'accesso ai dati industriali — compatibile con sistemi CESMII i3X.
                    </p>
                </div>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Clock size={12} />
                    <span>Aggiornato {lastRefresh.toLocaleTimeString()}</span>
                    <Button variant="outline" size="sm" onClick={handleRefresh} className="ml-2 clip-chamfer-sm">
                        <RefreshCw size={14} className="mr-1" />
                        Aggiorna
                    </Button>
                </div>
            </div>

            {/* ── Stats bar ── */}
            <div className="grid grid-cols-3 gap-4">
                <Card className="clip-chamfer-sm">
                    <CardContent className="p-4 flex items-center gap-3">
                        <div className="p-2 clip-hex bg-blue-500/10">
                            <Cpu size={18} className="text-blue-500" />
                        </div>
                        <div>
                            <p className="text-2xl font-bold">{equipment.length}</p>
                            <p className="text-xs text-muted-foreground">Equipment</p>
                        </div>
                    </CardContent>
                </Card>
                <Card className="clip-chamfer-sm">
                    <CardContent className="p-4 flex items-center gap-3">
                        <div className="p-2 clip-hex bg-purple-500/10">
                            <Tag size={18} className="text-purple-500" />
                        </div>
                        <div>
                            <p className="text-2xl font-bold">
                                {selectedEq ? properties.length : totalProps}
                            </p>
                            <p className="text-xs text-muted-foreground">
                                Properties {selectedEq ? `(${selectedEq.name})` : '(seleziona equipment)'}
                            </p>
                        </div>
                    </CardContent>
                </Card>
                <Card className="clip-chamfer-sm">
                    <CardContent className="p-4 flex items-center gap-3">
                        <div className="p-2 clip-hex bg-red-500/10">
                            <AlertTriangle size={18} className="text-red-500" />
                        </div>
                        <div>
                            <p className="text-2xl font-bold">{alarms.length}</p>
                            <p className="text-xs text-muted-foreground">Allarmi attivi</p>
                        </div>
                    </CardContent>
                </Card>
            </div>

            {/* ── Main tabs ── */}
            <Tabs defaultValue="browser">
                <TabsList className="clip-chamfer-sm">
                    <TabsTrigger value="browser" className="clip-chamfer-sm">
                        <Cpu size={14} className="mr-2" />
                        Equipment Browser
                    </TabsTrigger>
                    <TabsTrigger value="alarms" className="clip-chamfer-sm">
                        <AlertTriangle size={14} className="mr-2" />
                        Alarms
                        {alarms.length > 0 && (
                            <Badge className="ml-2 bg-red-500/20 text-red-600 text-xs">{alarms.length}</Badge>
                        )}
                    </TabsTrigger>
                    <TabsTrigger value="reference" className="clip-chamfer-sm">
                        <BookOpen size={14} className="mr-2" />
                        API Reference
                    </TabsTrigger>
                </TabsList>

                {/* ── Equipment Browser ── */}
                <TabsContent value="browser" className="mt-4">
                    <div className="grid grid-cols-5 gap-4">
                        {/* Equipment list */}
                        <div className="col-span-2 space-y-2">
                            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">
                                Equipment ({equipment.length})
                            </p>
                            {loadingEquipment ? (
                                <div className="space-y-2">
                                    {[1, 2, 3].map(i => (
                                        <div key={i} className="h-20 bg-muted animate-pulse clip-chamfer-sm" />
                                    ))}
                                </div>
                            ) : equipment.length === 0 ? (
                                <Card className="clip-chamfer-sm">
                                    <CardContent className="p-6 text-center text-sm text-muted-foreground">
                                        Nessun equipment trovato
                                    </CardContent>
                                </Card>
                            ) : (
                                equipment.map(eq => (
                                    <button
                                        key={eq.id}
                                        onClick={() => handleSelectEquipment(eq)}
                                        className={cn(
                                            'w-full text-left p-4 clip-chamfer-sm border transition-all hover:border-primary/50 hover:bg-primary/5',
                                            selectedEq?.id === eq.id
                                                ? 'border-primary bg-primary/10 shadow-sm'
                                                : 'border-border bg-card'
                                        )}
                                    >
                                        <div className="flex items-center justify-between">
                                            <div className="flex items-center gap-2 min-w-0">
                                                <Cpu size={15} className={cn(
                                                    'shrink-0',
                                                    selectedEq?.id === eq.id ? 'text-primary' : 'text-muted-foreground'
                                                )} />
                                                <span className="font-medium text-sm truncate">{eq.name}</span>
                                            </div>
                                            <ChevronRight size={14} className="shrink-0 text-muted-foreground" />
                                        </div>
                                        <p className="text-xs text-muted-foreground mt-1 ml-[23px] truncate">
                                            {eq.path ?? eq.id}
                                        </p>
                                        <div className="flex items-center gap-2 mt-2 ml-[23px]">
                                            <Badge variant="outline" className="text-xs font-mono">
                                                {eq.attributes?.driver_type ?? eq.description ?? 'Equipment'}
                                            </Badge>
                                            {eq.attributes?.enabled === false && (
                                                <Badge variant="outline" className="text-xs text-muted-foreground">Disabilitato</Badge>
                                            )}
                                        </div>
                                        <p className="text-[10px] text-muted-foreground/60 mt-1 ml-[23px] font-mono">
                                            {eq.id}
                                        </p>
                                    </button>
                                ))
                            )}
                        </div>

                        {/* Properties panel */}
                        <div className="col-span-3">
                            {!selectedEq ? (
                                <Card className="clip-chamfer-sm h-full flex items-center justify-center min-h-[300px]">
                                    <CardContent className="text-center text-muted-foreground p-8">
                                        <Cpu size={40} className="mx-auto mb-3 opacity-20" />
                                        <p className="text-sm">Seleziona un Equipment per vedere le Properties</p>
                                    </CardContent>
                                </Card>
                            ) : (
                                <Card className="clip-chamfer-sm">
                                    <CardHeader className="pb-3 border-b">
                                        <div className="flex items-center justify-between">
                                            <div>
                                                <CardTitle className="text-base flex items-center gap-2">
                                                    <Tag size={16} className="text-primary" />
                                                    {selectedEq.name}
                                                    <span className="text-muted-foreground font-normal text-sm">— Properties</span>
                                                </CardTitle>
                                                <p className="text-xs text-muted-foreground mt-1 font-mono">{selectedEq.id}</p>
                                            </div>
                                            <Badge variant="outline" className="text-xs">
                                                {loadingProps ? '…' : properties.length} properties
                                            </Badge>
                                        </div>
                                    </CardHeader>
                                    <CardContent className="p-0">
                                        {loadingProps ? (
                                            <div className="p-6 space-y-2">
                                                {[1, 2, 3, 4].map(i => (
                                                    <div key={i} className="h-10 bg-muted animate-pulse clip-chamfer-sm" />
                                                ))}
                                            </div>
                                        ) : properties.length === 0 ? (
                                            <div className="p-8 text-center text-sm text-muted-foreground">
                                                Nessuna property configurata per questo equipment
                                            </div>
                                        ) : (
                                            <Table>
                                                <TableHeader>
                                                    <TableRow>
                                                        <TableHead className="text-xs">ID i3X</TableHead>
                                                        <TableHead className="text-xs">Nome</TableHead>
                                                        <TableHead className="text-xs">Tipo</TableHead>
                                                        <TableHead className="text-xs">Valore live</TableHead>
                                                        <TableHead className="text-xs">Quality</TableHead>
                                                        <TableHead className="text-xs">Timestamp</TableHead>
                                                        {writeAllowed && <TableHead className="text-xs w-8"></TableHead>}
                                                    </TableRow>
                                                </TableHeader>
                                                <TableBody>
                                                    {properties.map(prop => {
                                                        const isEditing = editingPropId === prop.id;
                                                        return (
                                                            <TableRow
                                                                key={prop.id}
                                                                className={cn('group', isEditing && 'bg-primary/5')}
                                                            >
                                                                <TableCell className="font-mono text-xs text-muted-foreground">
                                                                    {prop.id}
                                                                </TableCell>
                                                                <TableCell className="font-medium text-sm">
                                                                    {prop.name}
                                                                </TableCell>
                                                                <TableCell>
                                                                    <DataTypeBadge type={prop.dataType} />
                                                                </TableCell>

                                                                {/* Value cell: read-only OR inline write input (only if permitted) */}
                                                                <TableCell>
                                                                    {isEditing && writeAllowed ? (
                                                                        <div className="flex items-center gap-1">
                                                                            <Input
                                                                                autoFocus
                                                                                value={editValue}
                                                                                onChange={e => setEditValue(e.target.value)}
                                                                                onKeyDown={e => handleKeyDown(e, prop)}
                                                                                placeholder={
                                                                                    prop.dataType === 'Boolean'
                                                                                        ? 'true / false'
                                                                                        : prop.dataType === 'Float'
                                                                                        ? '0.0'
                                                                                        : prop.dataType === 'Int32'
                                                                                        ? '0'
                                                                                        : 'testo'
                                                                                }
                                                                                className="h-7 text-xs font-mono w-28 clip-chamfer-sm"
                                                                                disabled={writing}
                                                                            />
                                                                            <Button
                                                                                size="icon"
                                                                                variant="ghost"
                                                                                className="h-7 w-7 text-green-600 hover:bg-green-500/10"
                                                                                onClick={() => confirmWrite(prop)}
                                                                                disabled={writing}
                                                                                title="Conferma scrittura (Enter)"
                                                                            >
                                                                                {writing ? (
                                                                                    <Send size={12} className="animate-pulse" />
                                                                                ) : (
                                                                                    <Check size={12} />
                                                                                )}
                                                                            </Button>
                                                                            <Button
                                                                                size="icon"
                                                                                variant="ghost"
                                                                                className="h-7 w-7 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                                                                onClick={cancelEdit}
                                                                                disabled={writing}
                                                                                title="Annulla (Esc)"
                                                                            >
                                                                                <X size={12} />
                                                                            </Button>
                                                                        </div>
                                                                    ) : (
                                                                        <span className="font-mono text-sm font-semibold">
                                                                            {formatValue(prop.current?.value)}
                                                                        </span>
                                                                    )}
                                                                </TableCell>

                                                                <TableCell>
                                                                    <QualityBadge quality={prop.current?.quality} />
                                                                </TableCell>
                                                                <TableCell className="text-xs text-muted-foreground">
                                                                    {formatTs(prop.current?.timestamp)}
                                                                </TableCell>

                                                                {/* Write trigger button — shown only if caller has i3x_write permission */}
                                                                {writeAllowed && (
                                                                    <TableCell>
                                                                        {!isEditing && (
                                                                            <Button
                                                                                size="icon"
                                                                                variant="ghost"
                                                                                className="h-7 w-7 opacity-0 group-hover:opacity-100 hover:bg-primary/10 hover:text-primary"
                                                                                onClick={() => startEdit(prop)}
                                                                                title={`Scrivi su ${prop.name}`}
                                                                            >
                                                                                <Pencil size={12} />
                                                                            </Button>
                                                                        )}
                                                                    </TableCell>
                                                                )}
                                                            </TableRow>
                                                        );
                                                    })}
                                                </TableBody>
                                            </Table>
                                        )}
                                    </CardContent>
                                </Card>
                            )}
                        </div>
                    </div>
                </TabsContent>

                {/* ── Alarms tab ── */}
                <TabsContent value="alarms" className="mt-4 space-y-4">
                    {/* Active alarms */}
                    <Card className="clip-chamfer-sm">
                        <CardHeader className="pb-3 border-b">
                            <CardTitle className="text-sm flex items-center gap-2">
                                <Zap size={15} className="text-red-500" />
                                Allarmi Attivi
                                <Badge className="bg-red-500/15 text-red-600 border-red-500/30 text-xs">{alarms.length}</Badge>
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="p-0">
                            {loadingAlarms ? (
                                <div className="p-4 space-y-2">
                                    {[1, 2].map(i => <div key={i} className="h-10 bg-muted animate-pulse clip-chamfer-sm" />)}
                                </div>
                            ) : alarms.length === 0 ? (
                                <div className="p-8 text-center flex flex-col items-center gap-2">
                                    <CheckCircle2 size={32} className="text-green-500 opacity-60" />
                                    <p className="text-sm text-muted-foreground">Nessun allarme attivo</p>
                                </div>
                            ) : (
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead className="text-xs w-8"></TableHead>
                                            <TableHead className="text-xs">ID i3X</TableHead>
                                            <TableHead className="text-xs">Equipment</TableHead>
                                            <TableHead className="text-xs">Property</TableHead>
                                            <TableHead className="text-xs">Tipo</TableHead>
                                            <TableHead className="text-xs">Messaggio</TableHead>
                                            <TableHead className="text-xs">Status</TableHead>
                                            <TableHead className="text-xs">Trigger</TableHead>
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {alarms.map(alarm => (
                                            <TableRow key={alarm.id}>
                                                <TableCell>
                                                    <SeverityIcon severity={alarm.severity} />
                                                </TableCell>
                                                <TableCell className="font-mono text-xs text-muted-foreground">{alarm.id}</TableCell>
                                                <TableCell className="text-xs">{alarm.equipmentName || alarm.equipmentId}</TableCell>
                                                <TableCell className="text-xs">{alarm.propertyName || alarm.propertyId}</TableCell>
                                                <TableCell className="text-xs">{alarm.alarmType}</TableCell>
                                                <TableCell className="text-sm">{alarm.message}</TableCell>
                                                <TableCell>
                                                    <Badge variant="outline" className={cn('text-xs', {
                                                        'text-red-600 border-red-500/30':    alarm.status === 'Active',
                                                        'text-yellow-600 border-yellow-500/30': alarm.status === 'Acknowledged',
                                                    })}>
                                                        {alarm.status}
                                                    </Badge>
                                                </TableCell>
                                                <TableCell className="text-xs text-muted-foreground">
                                                    {formatTs(alarm.triggerTime)}
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            )}
                        </CardContent>
                    </Card>

                    {/* Alarm history */}
                    <Card className="clip-chamfer-sm">
                        <CardHeader className="pb-3 border-b">
                            <CardTitle className="text-sm flex items-center gap-2">
                                <Clock size={15} className="text-muted-foreground" />
                                Storico Allarmi (ultimi 50)
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="p-0">
                            {alarmHistory.length === 0 ? (
                                <div className="p-6 text-center text-sm text-muted-foreground">Nessun evento nello storico</div>
                            ) : (
                                <div className="max-h-64 overflow-y-auto">
                                    <Table>
                                        <TableHeader>
                                            <TableRow>
                                                <TableHead className="text-xs w-8"></TableHead>
                                                <TableHead className="text-xs">Property</TableHead>
                                                <TableHead className="text-xs">Tipo</TableHead>
                                                <TableHead className="text-xs">Messaggio</TableHead>
                                                <TableHead className="text-xs">Status</TableHead>
                                                <TableHead className="text-xs">Trigger</TableHead>
                                            </TableRow>
                                        </TableHeader>
                                        <TableBody>
                                            {alarmHistory.map(alarm => (
                                                <TableRow key={alarm.id} className="opacity-75">
                                                    <TableCell>
                                                        <SeverityIcon severity={alarm.severity} />
                                                    </TableCell>
                                                    <TableCell className="text-xs">{alarm.propertyName || alarm.propertyId}</TableCell>
                                                    <TableCell className="text-xs">{alarm.alarmType}</TableCell>
                                                    <TableCell className="text-sm">{alarm.message}</TableCell>
                                                    <TableCell>
                                                        <Badge variant="outline" className="text-xs text-muted-foreground">
                                                            {alarm.status}
                                                        </Badge>
                                                    </TableCell>
                                                    <TableCell className="text-xs text-muted-foreground">
                                                        {new Date(alarm.triggerTime).toLocaleString()}
                                                    </TableCell>
                                                </TableRow>
                                            ))}
                                        </TableBody>
                                    </Table>
                                </div>
                            )}
                        </CardContent>
                    </Card>
                </TabsContent>

                {/* ── API Reference tab ── */}
                <TabsContent value="reference" className="mt-4">
                    <Card className="clip-chamfer-sm">
                        <CardHeader className="pb-3 border-b">
                            <CardTitle className="text-sm flex items-center gap-2">
                                <BookOpen size={15} className="text-primary" />
                                Endpoint i3X Access API
                            </CardTitle>
                            <p className="text-xs text-muted-foreground mt-1">
                                Tutti gli endpoint richiedono <code className="bg-muted px-1 rounded text-xs">Authorization: Bearer &lt;token&gt;</code> e <code className="bg-muted px-1 rounded text-xs">X-Organization-ID</code>.
                            </p>
                        </CardHeader>
                        <CardContent className="p-0">
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead className="text-xs w-16">Metodo</TableHead>
                                        <TableHead className="text-xs">Endpoint</TableHead>
                                        <TableHead className="text-xs">Descrizione</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {API_ENDPOINTS.map(ep => (
                                        <TableRow key={ep.path}>
                                            <TableCell>
                                                <Badge variant="outline" className={cn('text-xs font-mono', METHOD_COLOR[ep.method] ?? '')}>
                                                    {ep.method}
                                                </Badge>
                                            </TableCell>
                                            <TableCell className="font-mono text-xs">{ep.path}</TableCell>
                                            <TableCell className="text-sm text-muted-foreground">{ep.desc}</TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        </CardContent>
                    </Card>

                    {/* ID mapping reference */}
                    <Card className="clip-chamfer-sm mt-4">
                        <CardHeader className="pb-3 border-b">
                            <CardTitle className="text-sm flex items-center gap-2">
                                <ArrowRight size={15} className="text-primary" />
                                Mapping ID OpenEdge → i3X
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="p-4">
                            <div className="grid grid-cols-3 gap-4 text-sm">
                                {[
                                    { from: 'Gateway (id: 3)',       to: 'Equipment (id: "gw-3")',   color: 'text-blue-600' },
                                    { from: 'Tag (id: 42)',          to: 'Property (id: "tag-42")',  color: 'text-purple-600' },
                                    { from: 'AlarmEvent (id: 7)',    to: 'Alarm (id: "alarm-7")',    color: 'text-red-600' },
                                ].map(m => (
                                    <div key={m.from} className="flex items-center gap-2 p-3 bg-muted/50 clip-chamfer-sm">
                                        <code className="text-xs text-muted-foreground">{m.from}</code>
                                        <ArrowRight size={12} className="text-muted-foreground shrink-0" />
                                        <code className={cn('text-xs font-semibold', m.color)}>{m.to}</code>
                                    </div>
                                ))}
                            </div>
                            <div className="mt-4 p-3 bg-muted/30 clip-chamfer-sm space-y-2">
                                <p className="text-xs font-semibold text-muted-foreground">Quality codes (all protocols)</p>
                                <div className="flex gap-4">
                                    <QualityBadge quality={192} />
                                    <span className="text-xs text-muted-foreground">= 192</span>
                                    <QualityBadge quality={64} />
                                    <span className="text-xs text-muted-foreground">= 64</span>
                                    <QualityBadge quality={0} />
                                    <span className="text-xs text-muted-foreground">= 0</span>
                                </div>
                            </div>
                        </CardContent>
                    </Card>
                </TabsContent>
            </Tabs>
        </div>
    );
}
