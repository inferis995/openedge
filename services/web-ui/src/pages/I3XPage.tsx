import { useState, useEffect, useCallback, KeyboardEvent, useMemo } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import {
    Network,
    RefreshCw,
    ChevronRight,
    ChevronDown,
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
    History,
    Search,
    Building2,
    Layers,
    MapPin,
    Bell,
    BellOff,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import {
    AreaChart,
    Area,
    XAxis,
    YAxis,
    CartesianGrid,
    Tooltip,
    ResponsiveContainer,
} from 'recharts';
import {
    i3xApi,
    I3XEquipment,
    I3XProperty,
    I3XAlarm,
    I3XHistoryPoint,
} from '@/api/i3x';
import { useAuthStore } from '@/stores/useAuthStore';
import { toast } from 'sonner';
import { useTranslation } from 'react-i18next';
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';
import { startLoad } from '@/lib/startLoad';

// ─── Quality helpers ────────────────────────────────────────────────────────

function QualityBadge({ quality }: { quality?: number }) {
    const { t } = useTranslation();
    if (quality === undefined || quality === null)
        return <Badge variant="outline" className="text-muted-foreground text-xs">{t('i3xPage.no_data')}</Badge>;
    if (quality >= 192)
        return (
            <Badge className="bg-green-500/15 text-green-600 border-green-500/30 text-xs font-mono">
                <span className="mr-1 inline-block w-2 h-2 rounded-full bg-green-500 animate-pulse" />
                {t('i3xPage.quality_good')}
            </Badge>
        );
    if (quality >= 64)
        return (
            <Badge className="bg-yellow-500/15 text-yellow-600 border-yellow-500/30 text-xs font-mono">
                <span className="mr-1 inline-block w-2 h-2 rounded-full bg-yellow-500" />
                {t('i3xPage.quality_uncertain')}
            </Badge>
        );
    return (
        <Badge className="bg-red-500/15 text-red-600 border-red-500/30 text-xs font-mono">
            <span className="mr-1 inline-block w-2 h-2 rounded-full bg-red-500" />
            {t('i3xPage.quality_bad')}
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
    return new Date(ts).toLocaleTimeString(i18n.language);
}

function formatTsFull(ts?: string): string {
    if (!ts) return '—';
    return new Date(ts).toLocaleString(i18n.language);
}

// ─── Equipment Tree ──────────────────────────────────────────────────────────

type TreeNode = I3XEquipment & { children: TreeNode[]; depth: number };

function buildTree(items: I3XEquipment[]): TreeNode[] {
    const map = new Map<string, TreeNode>();
    for (const item of items) map.set(item.id, { ...item, children: [], depth: 0 });

    const roots: TreeNode[] = [];
    for (const node of map.values()) {
        if (node.parentId && map.has(node.parentId)) {
            map.get(node.parentId)!.children.push(node);
        } else {
            roots.push(node);
        }
    }

    const setDepth = (nodes: TreeNode[], depth: number) => {
        for (const n of nodes) { n.depth = depth; setDepth(n.children, depth + 1); }
    };
    setDepth(roots, 0);
    return roots;
}

const NODE_ICON: Record<string, React.ElementType> = {
    'org':  Building2,
    'site': MapPin,
    'area': Layers,
    'gw':   Cpu,
};

function nodeIcon(id: string): React.ElementType {
    const prefix = id.split('-')[0];
    return NODE_ICON[prefix] ?? Network;
}

function EquipmentTree({
    nodes,
    selected,
    onSelect,
    expanded,
    onToggle,
}: {
    nodes: TreeNode[];
    selected: I3XEquipment | null;
    onSelect: (eq: I3XEquipment) => void;
    expanded: Set<string>;
    onToggle: (id: string) => void;
}) {
    return (
        <>
            {nodes.map(node => {
                const Icon = nodeIcon(node.id);
                const isSelected = selected?.id === node.id;
                const isExpanded = expanded.has(node.id);
                const hasChildren = node.children.length > 0;
                const isEquipment = node.type === 'Equipment';

                return (
                    <div key={node.id}>
                        <button
                            onClick={() => {
                                onSelect(node);
                                if (hasChildren) onToggle(node.id);
                            }}
                            style={{ paddingLeft: `${node.depth * 16 + 8}px` }}
                            className={cn(
                                'w-full text-left flex items-center gap-2 py-2 pr-3 rounded-sm transition-all text-sm',
                                'hover:bg-primary/5 hover:text-primary',
                                isSelected ? 'bg-primary/10 text-primary font-semibold' : 'text-foreground',
                            )}
                        >
                            {hasChildren ? (
                                isExpanded
                                    ? <ChevronDown size={13} className="shrink-0 text-muted-foreground" />
                                    : <ChevronRight size={13} className="shrink-0 text-muted-foreground" />
                            ) : (
                                <span className="w-[13px] shrink-0" />
                            )}
                            <Icon
                                size={14}
                                className={cn(
                                    'shrink-0',
                                    isEquipment
                                        ? isSelected ? 'text-primary' : 'text-blue-500'
                                        : isSelected ? 'text-primary' : 'text-muted-foreground',
                                )}
                            />
                            <span className="truncate">{node.name}</span>
                            {isEquipment && node.attributes?.driver_type && (
                                <Badge variant="outline" className="ml-auto shrink-0 text-[10px] font-mono py-0 px-1 bg-blue-500/10 text-blue-600 border-blue-500/20">
                                    {node.attributes.driver_type}
                                </Badge>
                            )}
                            {isEquipment && node.attributes?.enabled === false && (
                                <Badge variant="outline" className="ml-1 shrink-0 text-[10px] py-0 px-1 text-muted-foreground">
                                    OFF
                                </Badge>
                            )}
                        </button>
                        {hasChildren && isExpanded && (
                            <EquipmentTree
                                nodes={node.children}
                                selected={selected}
                                onSelect={onSelect}
                                expanded={expanded}
                                onToggle={onToggle}
                            />
                        )}
                    </div>
                );
            })}
        </>
    );
}

// ─── Property History Chart ──────────────────────────────────────────────────

const RANGE_OPTIONS = [
    { label: '1h',  value: 1 },
    { label: '6h',  value: 6 },
    { label: '24h', value: 24 },
    { label: '7d',  value: 168 },
];

function PropertyHistoryPanel({ property }: { property: I3XProperty }) {
    const { t } = useTranslation();
    const [range, setRange]   = useState(1);
    const [points, setPoints] = useState<I3XHistoryPoint[]>([]);
    // Starts true: the effect below fetches on mount.
    const [loading, setLoading] = useState(true);
    const [loadedPropertyId, setLoadedPropertyId] = useState(property.id);

    // A different property refetches via the effect; flag it during render
    // instead of synchronously inside the effect.
    if (loadedPropertyId !== property.id) {
        setLoadedPropertyId(property.id);
        setLoading(true);
    }

    const fetchHistory = useCallback(async () => {
        try {
            const to   = new Date();
            const from = new Date(to.getTime() - range * 3600_000);
            const res  = await i3xApi.getPropertyHistory(
                property.id,
                from.toISOString(),
                to.toISOString(),
                500,
            );
            setPoints(res.items ?? []);
        } catch {
            toast.error(i18n.t('i3xPage.history_load_error'));
        } finally {
            setLoading(false);
        }
    }, [property.id, range]);

    const load = () => {
        setLoading(true);
        fetchHistory();
    };

    const changeRange = (next: number) => {
        if (next === range) return;
        setLoading(true);
        setRange(next);
    };

    useEffect(() => { startLoad(fetchHistory); }, [fetchHistory]);

    const chartData = useMemo(
        () =>
            points.map(p => ({
                t: new Date(p.timestamp).toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' }),
                v: p.value,
            })),
        [points],
    );

    return (
        <div className="border-t bg-muted/30 p-4 space-y-3">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 text-sm font-medium">
                    <History size={14} className="text-primary" />
                    <span>{t('i3xPage.history_of', { name: property.name })}</span>
                    <Badge variant="outline" className="text-xs font-mono">
                        {t('i3xPage.samples', { count: points.length })}
                    </Badge>
                </div>
                <div className="flex items-center gap-2">
                    <Select value={String(range)} onValueChange={v => changeRange(Number(v))}>
                        <SelectTrigger className="h-9 sm:h-7 text-xs w-20 clip-chamfer-sm">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {RANGE_OPTIONS.map(o => (
                                <SelectItem key={o.value} value={String(o.value)} className="text-xs">
                                    {o.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <Button variant="ghost" size="icon" className="h-9 sm:h-7 w-9 sm:w-7" onClick={load} disabled={loading}>
                        <RefreshCw size={12} className={loading ? 'animate-spin' : ''} />
                    </Button>
                </div>
            </div>

            {loading ? (
                <div className="h-40 bg-muted animate-pulse rounded" />
            ) : points.length === 0 ? (
                <div className="h-40 flex items-center justify-center text-xs text-muted-foreground">
                    {t('i3xPage.history_empty')}
                </div>
            ) : (
                <ResponsiveContainer width="100%" height={140}>
                    <AreaChart data={chartData} margin={{ top: 4, right: 8, left: -20, bottom: 0 }}>
                        <defs>
                            <linearGradient id="histGrad" x1="0" y1="0" x2="0" y2="1">
                                <stop offset="5%"  stopColor="hsl(var(--primary))" stopOpacity={0.3} />
                                <stop offset="95%" stopColor="hsl(var(--primary))" stopOpacity={0}   />
                            </linearGradient>
                        </defs>
                        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                        <XAxis dataKey="t" tick={{ fontSize: 10 }} interval="preserveStartEnd" />
                        <YAxis tick={{ fontSize: 10 }} />
                        <Tooltip
                            contentStyle={{ fontSize: 11 }}
                            formatter={(v: unknown) => [formatValue(v), property.name]}
                        />
                        <Area
                            type="monotone"
                            dataKey="v"
                            stroke="hsl(var(--primary))"
                            strokeWidth={1.5}
                            fill="url(#histGrad)"
                            connectNulls={false}
                            dot={false}
                        />
                    </AreaChart>
                </ResponsiveContainer>
            )}
        </div>
    );
}

// ─── API Reference ───────────────────────────────────────────────────────────

// desc = i18n key, resolved with t() at render.
const API_ENDPOINTS = [
    { method: 'GET',  path: '/api/i3x/v1/equipment',                            desc: 'i3xPage.ep_equipment_list' },
    { method: 'GET',  path: '/api/i3x/v1/equipment/:id',                        desc: 'i3xPage.ep_equipment_get' },
    { method: 'GET',  path: '/api/i3x/v1/equipment/:id/children',               desc: 'i3xPage.ep_equipment_children' },
    { method: 'GET',  path: '/api/i3x/v1/equipment/:id/properties',             desc: 'i3xPage.ep_equipment_properties' },
    { method: 'GET',  path: '/api/i3x/v1/equipment/:id/properties/:propId',     desc: 'i3xPage.ep_equipment_property' },
    { method: 'GET',  path: '/api/i3x/v1/properties',                           desc: 'i3xPage.ep_properties_list' },
    { method: 'GET',  path: '/api/i3x/v1/properties/:id',                       desc: 'i3xPage.ep_property_get' },
    { method: 'GET',  path: '/api/i3x/v1/properties/:id/history',               desc: 'i3xPage.ep_property_history' },
    { method: 'PUT',  path: '/api/i3x/v1/properties/:id/value',                 desc: 'i3xPage.ep_property_write' },
    { method: 'POST', path: '/api/i3x/v1/properties/values',                    desc: 'i3xPage.ep_properties_batch' },
    { method: 'GET',  path: '/api/i3x/v1/alarms',                               desc: 'i3xPage.ep_alarms_list' },
    { method: 'GET',  path: '/api/i3x/v1/alarms/history',                       desc: 'i3xPage.ep_alarms_history' },
    { method: 'POST', path: '/api/i3x/v1/alarms/:id/acknowledge',               desc: 'i3xPage.ep_alarm_ack' },
];

const METHOD_COLOR: Record<string, string> = {
    GET:  'bg-green-500/15 text-green-700 border-green-500/30',
    PUT:  'bg-orange-500/15 text-orange-700 border-orange-500/30',
    POST: 'bg-blue-500/15 text-blue-700 border-blue-500/30',
};

// ─── Main page ───────────────────────────────────────────────────────────────

export default function I3XPage() {
    const { t } = useTranslation();
    const { canI3xWrite } = useAuthStore();
    const writeAllowed = canI3xWrite();

    const [allEquipment, setAllEquipment]           = useState<I3XEquipment[]>([]);
    const [selectedEq, setSelectedEq]               = useState<I3XEquipment | null>(null);
    const [expanded, setExpanded]                   = useState<Set<string>>(new Set());
    const [properties, setProperties]               = useState<I3XProperty[]>([]);
    const [propSearch, setPropSearch]               = useState('');
    const [selectedProp, setSelectedProp]           = useState<I3XProperty | null>(null);
    const [alarms, setAlarms]                       = useState<I3XAlarm[]>([]);
    const [alarmHistory, setAlarmHistory]           = useState<I3XAlarm[]>([]);
    const [loadingEquipment, setLoadingEquipment]   = useState(true);
    const [loadingProps, setLoadingProps]           = useState(false);
    const [loadingAlarms, setLoadingAlarms]         = useState(true);
    const [lastRefresh, setLastRefresh]             = useState<Date>(new Date());
    const [ackingId, setAckingId]                   = useState<string | null>(null);

    // Write state
    const [editingPropId, setEditingPropId] = useState<string | null>(null);
    const [editValue, setEditValue]         = useState('');
    const [writing, setWriting]             = useState(false);

    const treeRoots = useMemo(() => buildTree(allEquipment), [allEquipment]);

    const filteredProps = useMemo(
        () =>
            propSearch.trim()
                ? properties.filter(p =>
                      p.name.toLowerCase().includes(propSearch.toLowerCase()) ||
                      p.id.toLowerCase().includes(propSearch.toLowerCase()),
                  )
                : properties,
        [properties, propSearch],
    );

    // Fetch without flipping the loading flags first: on mount they are already true.
    const fetchBase = useCallback(async () => {
        try {
            const [eqRes, alRes, histRes] = await Promise.all([
                i3xApi.listEquipment(),
                i3xApi.listAlarms(),
                i3xApi.listAlarmHistory(100),
            ]);
            const eqItems = eqRes.items ?? [];
            setAllEquipment(eqItems);
            setAlarms(alRes.items ?? []);
            setAlarmHistory(histRes.items ?? []);
            setLastRefresh(new Date());

            // Auto-expand root nodes on first load.
            setExpanded(prev => {
                if (prev.size > 0) return prev;
                const roots = new Set<string>();
                for (const eq of eqItems) if (!eq.parentId) roots.add(eq.id);
                return roots;
            });
        } catch {
            toast.error(i18n.t('i3xPage.load_error'));
        } finally {
            setLoadingEquipment(false);
            setLoadingAlarms(false);
        }
    }, []);

    const loadBase = useCallback(() => {
        setLoadingEquipment(true);
        setLoadingAlarms(true);
        return fetchBase();
    }, [fetchBase]);

    const loadProperties = useCallback(async (eq: I3XEquipment) => {
        setLoadingProps(true);
        setProperties([]);
        setSelectedProp(null);
        setPropSearch('');
        try {
            const res = await i3xApi.listEquipmentProperties(eq.id);
            setProperties(res.items ?? []);
        } catch {
            toast.error(i18n.t('i3xPage.properties_load_error'));
        } finally {
            setLoadingProps(false);
        }
    }, []);

    useEffect(() => {
        startLoad(fetchBase);
        const interval = setInterval(loadBase, 30_000);
        return () => clearInterval(interval);
    }, [fetchBase, loadBase]);

    const handleSelectEquipment = (eq: I3XEquipment) => {
        setSelectedEq(eq);
        setEditingPropId(null);
        setSelectedProp(null);
        if (eq.type === 'Equipment') {
            loadProperties(eq);
        } else {
            setProperties([]);
        }
    };

    const handleToggle = (id: string) => {
        setExpanded(prev => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id); else next.add(id);
            return next;
        });
    };

    const startEdit = (prop: I3XProperty) => {
        setEditingPropId(prop.id);
        const cur = prop.current?.value;
        setEditValue(cur !== null && cur !== undefined ? String(cur) : '');
    };

    const cancelEdit = () => { setEditingPropId(null); setEditValue(''); };

    const confirmWrite = async (prop: I3XProperty) => {
        // This writes to live industrial equipment. The field is pre-filled with
        // the current value for inspection, so a stray keystroke followed by
        // Enter — muscle memory — used to move a real actuator with no dialog,
        // no undo and no bounds, unlike the synoptic setpoint widget and the
        // recipe loader, which both confirm first.
        // The dialog focuses "Cancel" first, so the same stray Enter backs out.
        if (!(await confirmAction({
            title: i18n.t('ask.write_value', { value: editValue, name: prop.name }),
            description: i18n.t('ask.write_value_desc'),
            confirmLabel: i18n.t('ask.write'),
            destructive: true,
        }))) {
            return;
        }

        setWriting(true);
        try {
            let coerced: unknown = editValue;
            if (prop.dataType === 'Float' || prop.dataType === 'Int32') {
                const n = Number(editValue);
                if (isNaN(n)) { toast.error(t('i3xPage.invalid_value', { type: prop.dataType })); return; }
                coerced = n;
            } else if (prop.dataType === 'Boolean') {
                const lower = editValue.trim().toLowerCase();
                if (!['true','false','1','0'].includes(lower)) {
                    toast.error(t('i3xPage.invalid_boolean'));
                    return;
                }
                coerced = lower === 'true' || lower === '1';
            }
            await i3xApi.writePropertyValue(prop.id, coerced);
            toast.success(t('i3xPage.write_sent', { name: prop.name, value: editValue }));
            setEditingPropId(null);
            setEditValue('');
            setTimeout(() => selectedEq && loadProperties(selectedEq), 1500);
        } catch {
            toast.error(t('i3xPage.write_error', { name: prop.name }));
        } finally {
            setWriting(false);
        }
    };

    const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>, prop: I3XProperty) => {
        if (e.key === 'Enter') confirmWrite(prop);
        if (e.key === 'Escape') cancelEdit();
    };

    const acknowledgeAlarm = async (alarm: I3XAlarm) => {
        setAckingId(alarm.id);
        try {
            await i3xApi.acknowledgeAlarm(alarm.id);
            toast.success(t('i3xPage.alarm_acked', { id: alarm.id }));
            await loadBase();
        } catch {
            toast.error(t('i3xPage.ack_error'));
        } finally {
            setAckingId(null);
        }
    };

    const handleRefresh = () => {
        loadBase();
        if (selectedEq) loadProperties(selectedEq);
    };

    const activeAlarmCount = alarms.filter(a => a.status === 'Active').length;
    const equipmentCount   = allEquipment.filter(e => e.type === 'Equipment').length;

    return (
        <div className="space-y-6">
            {/* ── Header ── */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between gap-4">
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
                        {t('i3xPage.subtitle')}
                    </p>
                </div>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Clock size={12} />
                    <span>{lastRefresh.toLocaleTimeString(i18n.language)}</span>
                    <Button variant="outline" size="sm" onClick={handleRefresh} className="ml-2 clip-chamfer-sm">
                        <RefreshCw size={14} className="mr-1" />
                        {t('i3xPage.refresh')}
                    </Button>
                </div>
            </div>

            {/* ── Stats bar ── */}
            <div className="grid grid-cols-2 sm:grid-cols-2 lg:grid-cols-4 gap-4">
                <Card className="clip-chamfer-sm">
                    <CardContent className="p-4 flex items-center gap-3">
                        <div className="p-2 clip-hex bg-blue-500/10">
                            <Cpu size={18} className="text-blue-500" />
                        </div>
                        <div>
                            <p className="text-2xl font-bold">{equipmentCount}</p>
                            <p className="text-xs text-muted-foreground">{t('i3xPage.stat_equipment')}</p>
                        </div>
                    </CardContent>
                </Card>
                <Card className="clip-chamfer-sm">
                    <CardContent className="p-4 flex items-center gap-3">
                        <div className="p-2 clip-hex bg-slate-500/10">
                            <Network size={18} className="text-slate-500" />
                        </div>
                        <div>
                            <p className="text-2xl font-bold">{allEquipment.filter(e => e.type === 'Assembly').length}</p>
                            <p className="text-xs text-muted-foreground">{t('i3xPage.stat_assembly')}</p>
                        </div>
                    </CardContent>
                </Card>
                <Card className="clip-chamfer-sm">
                    <CardContent className="p-4 flex items-center gap-3">
                        <div className="p-2 clip-hex bg-purple-500/10">
                            <Tag size={18} className="text-purple-500" />
                        </div>
                        <div>
                            <p className="text-2xl font-bold">{selectedEq?.type === 'Equipment' ? properties.length : '—'}</p>
                            <p className="text-xs text-muted-foreground">
                                {selectedEq?.type === 'Equipment' ? t('i3xPage.stat_properties_of', { name: selectedEq.name }) : t('i3xPage.stat_properties_pick')}
                            </p>
                        </div>
                    </CardContent>
                </Card>
                <Card className="clip-chamfer-sm">
                    <CardContent className="p-4 flex items-center gap-3">
                        <div className={cn('p-2 clip-hex', activeAlarmCount > 0 ? 'bg-red-500/10' : 'bg-green-500/10')}>
                            <AlertTriangle size={18} className={activeAlarmCount > 0 ? 'text-red-500' : 'text-green-500'} />
                        </div>
                        <div>
                            <p className="text-2xl font-bold">{activeAlarmCount}</p>
                            <p className="text-xs text-muted-foreground">{t('i3xPage.stat_active_alarms')}</p>
                        </div>
                    </CardContent>
                </Card>
            </div>

            {/* ── Main tabs ── */}
            <Tabs defaultValue="browser">
                <TabsList className="clip-chamfer-sm">
                    <TabsTrigger value="browser" className="clip-chamfer-sm">
                        <Cpu size={14} className="mr-2" />
                        {t('i3xPage.tab_browser')}
                    </TabsTrigger>
                    <TabsTrigger value="alarms" className="clip-chamfer-sm">
                        <AlertTriangle size={14} className="mr-2" />
                        {t('i3xPage.tab_alarms')}
                        {activeAlarmCount > 0 && (
                            <Badge className="ml-2 bg-red-500/20 text-red-600 text-xs">{activeAlarmCount}</Badge>
                        )}
                    </TabsTrigger>
                    <TabsTrigger value="reference" className="clip-chamfer-sm">
                        <BookOpen size={14} className="mr-2" />
                        {t('i3xPage.tab_reference')}
                    </TabsTrigger>
                </TabsList>

                {/* ── Equipment Browser ── */}
                <TabsContent value="browser" className="mt-4">
                    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-4">
                        {/* Tree navigator */}
                        <Card className="col-span-2 clip-chamfer-sm">
                            <CardHeader className="pb-2 border-b">
                                <CardTitle className="text-sm flex items-center gap-2">
                                    <Network size={14} className="text-primary" />
                                    {t('i3xPage.hierarchy')}
                                    <Badge variant="outline" className="text-xs ml-auto">{allEquipment.length}</Badge>
                                </CardTitle>
                            </CardHeader>
                            <CardContent className="p-2 max-h-[600px] overflow-y-auto">
                                {loadingEquipment ? (
                                    <div className="space-y-1 p-2">
                                        {[1, 2, 3, 4, 5].map(i => (
                                            <div key={i} className="h-10 sm:h-8 bg-muted animate-pulse rounded" />
                                        ))}
                                    </div>
                                ) : allEquipment.length === 0 ? (
                                    <div className="p-8 text-center text-sm text-muted-foreground">
                                        {t('i3xPage.no_equipment')}
                                    </div>
                                ) : (
                                    <EquipmentTree
                                        nodes={treeRoots}
                                        selected={selectedEq}
                                        onSelect={handleSelectEquipment}
                                        expanded={expanded}
                                        onToggle={handleToggle}
                                    />
                                )}
                            </CardContent>
                        </Card>

                        {/* Properties panel */}
                        <div className="col-span-3 space-y-0">
                            {!selectedEq ? (
                                <Card className="clip-chamfer-sm h-full flex items-center justify-center min-h-[400px]">
                                    <CardContent className="text-center text-muted-foreground p-8">
                                        <Network size={40} className="mx-auto mb-3 opacity-20" />
                                        <p className="text-sm">{t('i3xPage.pick_equipment')}</p>
                                    </CardContent>
                                </Card>
                            ) : selectedEq.type === 'Assembly' ? (
                                <Card className="clip-chamfer-sm h-full min-h-[400px]">
                                    <CardContent className="pt-8 text-center text-muted-foreground p-8">
                                        <Network size={40} className="mx-auto mb-3 opacity-20" />
                                        <p className="text-sm font-medium">{selectedEq.name}</p>
                                        <p className="text-xs mt-2 max-w-xs mx-auto">
                                            {t('i3xPage.assembly_node')} <Badge variant="outline" className="text-xs mx-1">Assembly</Badge> — {t('i3xPage.assembly_hint')} <Badge variant="outline" className="text-xs mx-1">Equipment</Badge>.
                                        </p>
                                        <code className="text-[10px] mt-3 block text-muted-foreground/60 font-mono">{selectedEq.id}</code>
                                        <p className="text-xs mt-4 text-muted-foreground/60 italic">{selectedEq.path}</p>
                                    </CardContent>
                                </Card>
                            ) : (
                                <Card className="clip-chamfer-sm">
                                    <CardHeader className="pb-3 border-b">
                                        <div className="flex items-center justify-between gap-2">
                                            <div className="min-w-0">
                                                <CardTitle className="text-base flex items-center gap-2">
                                                    <Tag size={16} className="text-primary shrink-0" />
                                                    <span className="truncate">{selectedEq.name}</span>
                                                    <span className="text-muted-foreground font-normal text-sm shrink-0">— {t('i3xPage.properties')}</span>
                                                </CardTitle>
                                                <p className="text-[10px] text-muted-foreground mt-0.5 font-mono">{selectedEq.path}</p>
                                            </div>
                                            <Badge variant="outline" className="text-xs shrink-0">
                                                {loadingProps ? '…' : filteredProps.length} / {properties.length}
                                            </Badge>
                                        </div>
                                        <div className="mt-2 relative">
                                            <Search size={13} className="absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
                                            <Input
                                                placeholder={t('i3xPage.search_property')}
                                                value={propSearch}
                                                onChange={e => setPropSearch(e.target.value)}
                                                className="pl-7 h-10 sm:h-8 text-xs clip-chamfer-sm"
                                            />
                                        </div>
                                    </CardHeader>
                                    <CardContent className="p-0">
                                        {loadingProps ? (
                                            <div className="p-4 space-y-2">
                                                {[1, 2, 3, 4].map(i => (
                                                    <div key={i} className="h-10 bg-muted animate-pulse clip-chamfer-sm" />
                                                ))}
                                            </div>
                                        ) : properties.length === 0 ? (
                                            <div className="p-8 text-center text-sm text-muted-foreground">
                                                {t('i3xPage.no_properties')}
                                            </div>
                                        ) : (
                                            <>
                                                <div className="max-h-[480px] overflow-y-auto">
                                                    <Table>
                                                        <TableHeader>
                                                            <TableRow>
                                                                <TableHead className="text-xs">{t('i3xPage.col_id')}</TableHead>
                                                                <TableHead className="text-xs">{t('common.name')}</TableHead>
                                                                <TableHead className="text-xs">{t('i3xPage.col_type')}</TableHead>
                                                                <TableHead className="text-xs">{t('i3xPage.col_live_value')}</TableHead>
                                                                <TableHead className="text-xs">{t('i3xPage.col_quality')}</TableHead>
                                                                <TableHead className="text-xs">{t('i3xPage.col_timestamp')}</TableHead>
                                                                <TableHead className="text-xs w-16"></TableHead>
                                                            </TableRow>
                                                        </TableHeader>
                                                        <TableBody>
                                                            {filteredProps.map(prop => {
                                                                const isEditing  = editingPropId === prop.id;
                                                                const isHistory  = selectedProp?.id === prop.id;
                                                                return (
                                                                    <TableRow
                                                                        key={prop.id}
                                                                        className={cn(
                                                                            'group',
                                                                            isEditing  && 'bg-primary/5',
                                                                            isHistory  && 'bg-primary/5',
                                                                        )}
                                                                    >
                                                                        <TableCell className="font-mono text-xs text-muted-foreground py-2">
                                                                            {prop.id}
                                                                        </TableCell>
                                                                        <TableCell className="font-medium text-sm py-2">
                                                                            <div className="flex items-center gap-1.5">
                                                                                {prop.name}
                                                                                {prop.historize && (
                                                                                    <Badge variant="outline" className="text-[10px] py-0 px-1 text-violet-600 border-violet-400/40 bg-violet-500/10">
                                                                                        {t('i3xPage.hist_badge')}
                                                                                    </Badge>
                                                                                )}
                                                                            </div>
                                                                        </TableCell>
                                                                        <TableCell className="py-2">
                                                                            <DataTypeBadge type={prop.dataType} />
                                                                        </TableCell>
                                                                        <TableCell className="py-2">
                                                                            {isEditing && writeAllowed ? (
                                                                                <div className="flex items-center gap-1">
                                                                                    <Input
                                                                                        autoFocus
                                                                                        value={editValue}
                                                                                        onChange={e => setEditValue(e.target.value)}
                                                                                        onKeyDown={e => handleKeyDown(e, prop)}
                                                                                        placeholder={
                                                                                            prop.dataType === 'Boolean' ? 'true/false'
                                                                                            : prop.dataType === 'Float' ? '0.0'
                                                                                            : prop.dataType === 'Int32' ? '0'
                                                                                            : t('i3xPage.text_placeholder')
                                                                                        }
                                                                                        className="h-9 sm:h-7 text-xs font-mono w-28 clip-chamfer-sm"
                                                                                        disabled={writing}
                                                                                    />
                                                                                    <Button
                                                                                        size="icon" variant="ghost"
                                                                                        className="h-9 sm:h-7 w-9 sm:w-7 text-green-600 hover:bg-green-500/10"
                                                                                        onClick={() => confirmWrite(prop)} disabled={writing}
                                                                                    >
                                                                                        {writing ? <Send size={12} className="animate-pulse" /> : <Check size={12} />}
                                                                                    </Button>
                                                                                    <Button
                                                                                        size="icon" variant="ghost"
                                                                                        className="h-9 sm:h-7 w-9 sm:w-7 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                                                                        onClick={cancelEdit} disabled={writing}
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
                                                                        <TableCell className="py-2">
                                                                            <QualityBadge quality={prop.current?.quality} />
                                                                        </TableCell>
                                                                        <TableCell className="text-xs text-muted-foreground py-2">
                                                                            {formatTs(prop.current?.timestamp)}
                                                                        </TableCell>
                                                                        <TableCell className="py-2">
                                                                            <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                                                                                {prop.historize && (
                                                                                    <Button
                                                                                        size="icon" variant="ghost"
                                                                                        className={cn('h-7 w-7', isHistory ? 'text-primary bg-primary/10' : 'text-muted-foreground hover:text-primary hover:bg-primary/10')}
                                                                                        onClick={() => setSelectedProp(isHistory ? null : prop)}
                                                                                        title={t('i3xPage.history')}
                                                                                    >
                                                                                        <History size={12} />
                                                                                    </Button>
                                                                                )}
                                                                                {writeAllowed && !isEditing && (
                                                                                    <Button
                                                                                        size="icon" variant="ghost"
                                                                                        className="h-9 sm:h-7 w-9 sm:w-7 text-muted-foreground hover:bg-primary/10 hover:text-primary"
                                                                                        onClick={() => startEdit(prop)}
                                                                                        title={t('i3xPage.write_to', { name: prop.name })}
                                                                                    >
                                                                                        <Pencil size={12} />
                                                                                    </Button>
                                                                                )}
                                                                            </div>
                                                                        </TableCell>
                                                                    </TableRow>
                                                                );
                                                            })}
                                                        </TableBody>
                                                    </Table>
                                                </div>
                                                {selectedProp && (
                                                    <PropertyHistoryPanel property={selectedProp} />
                                                )}
                                            </>
                                        )}
                                    </CardContent>
                                </Card>
                            )}
                        </div>
                    </div>
                </TabsContent>

                {/* ── Alarms ── */}
                <TabsContent value="alarms" className="mt-4 space-y-4">
                    <Card className="clip-chamfer-sm">
                        <CardHeader className="pb-3 border-b">
                            <CardTitle className="text-sm flex items-center gap-2">
                                <Zap size={15} className="text-red-500" />
                                {t('i3xPage.active_alarms')}
                                <Badge className="bg-red-500/15 text-red-600 border-red-500/30 text-xs">{activeAlarmCount}</Badge>
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
                                    <p className="text-sm text-muted-foreground">{t('i3xPage.no_active_alarms')}</p>
                                </div>
                            ) : (
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead className="text-xs w-8"></TableHead>
                                            <TableHead className="text-xs">{t('i3xPage.col_id')}</TableHead>
                                            <TableHead className="text-xs">{t('i3xPage.col_equipment')}</TableHead>
                                            <TableHead className="text-xs">{t('i3xPage.col_property')}</TableHead>
                                            <TableHead className="text-xs">{t('i3xPage.col_type')}</TableHead>
                                            <TableHead className="text-xs">{t('i3xPage.col_message')}</TableHead>
                                            <TableHead className="text-xs">{t('i3xPage.col_status')}</TableHead>
                                            <TableHead className="text-xs">{t('i3xPage.col_trigger')}</TableHead>
                                            {writeAllowed && <TableHead className="text-xs w-24"></TableHead>}
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {alarms.map(alarm => (
                                            <TableRow key={alarm.id}>
                                                <TableCell><SeverityIcon severity={alarm.severity} /></TableCell>
                                                <TableCell className="font-mono text-xs text-muted-foreground">{alarm.id}</TableCell>
                                                <TableCell className="text-xs">{alarm.equipmentName || alarm.equipmentId}</TableCell>
                                                <TableCell className="text-xs">{alarm.propertyName || alarm.propertyId}</TableCell>
                                                <TableCell className="text-xs">{alarm.alarmType}</TableCell>
                                                <TableCell className="text-sm max-w-[200px] truncate">{alarm.message}</TableCell>
                                                <TableCell>
                                                    <Badge variant="outline" className={cn('text-xs', {
                                                        'text-red-600 border-red-500/30':       alarm.status === 'Active',
                                                        'text-yellow-600 border-yellow-500/30': alarm.status === 'Acknowledged',
                                                    })}>
                                                        {t(`i3xPage.status_${alarm.status}`, { defaultValue: alarm.status })}
                                                    </Badge>
                                                </TableCell>
                                                <TableCell className="text-xs text-muted-foreground">{formatTs(alarm.triggerTime)}</TableCell>
                                                {writeAllowed && (
                                                    <TableCell>
                                                        {alarm.status === 'Active' && (
                                                            <Button
                                                                variant="outline"
                                                                size="sm"
                                                                className="h-9 sm:h-7 text-xs clip-chamfer-sm"
                                                                onClick={() => acknowledgeAlarm(alarm)}
                                                                disabled={ackingId === alarm.id}
                                                            >
                                                                {ackingId === alarm.id ? (
                                                                    <RefreshCw size={11} className="mr-1 animate-spin" />
                                                                ) : (
                                                                    <Bell size={11} className="mr-1" />
                                                                )}
                                                                {t('i3xPage.ack')}
                                                            </Button>
                                                        )}
                                                        {alarm.status === 'Acknowledged' && (
                                                            <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
                                                                <BellOff size={10} />
                                                                {alarm.ackUser && <span>{alarm.ackUser}</span>}
                                                            </div>
                                                        )}
                                                    </TableCell>
                                                )}
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            )}
                        </CardContent>
                    </Card>

                    <Card className="clip-chamfer-sm">
                        <CardHeader className="pb-3 border-b">
                            <CardTitle className="text-sm flex items-center gap-2">
                                <Clock size={15} className="text-muted-foreground" />
                                {t('i3xPage.alarm_history')}
                                <Badge variant="outline" className="text-xs">{alarmHistory.length}</Badge>
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="p-0">
                            {alarmHistory.length === 0 ? (
                                <div className="p-6 text-center text-sm text-muted-foreground">{t('i3xPage.no_history_events')}</div>
                            ) : (
                                <div className="max-h-72 overflow-y-auto">
                                    <Table>
                                        <TableHeader>
                                            <TableRow>
                                                <TableHead className="text-xs w-8"></TableHead>
                                                <TableHead className="text-xs">{t('i3xPage.col_property')}</TableHead>
                                                <TableHead className="text-xs">{t('i3xPage.col_equipment')}</TableHead>
                                                <TableHead className="text-xs">{t('i3xPage.col_message')}</TableHead>
                                                <TableHead className="text-xs">{t('i3xPage.col_status')}</TableHead>
                                                <TableHead className="text-xs">{t('i3xPage.col_trigger')}</TableHead>
                                                <TableHead className="text-xs">{t('i3xPage.col_clear')}</TableHead>
                                            </TableRow>
                                        </TableHeader>
                                        <TableBody>
                                            {alarmHistory.map(alarm => (
                                                <TableRow key={alarm.id} className="opacity-80">
                                                    <TableCell><SeverityIcon severity={alarm.severity} /></TableCell>
                                                    <TableCell className="text-xs">{alarm.propertyName || alarm.propertyId}</TableCell>
                                                    <TableCell className="text-xs text-muted-foreground">{alarm.equipmentName}</TableCell>
                                                    <TableCell className="text-xs max-w-[180px] truncate">{alarm.message}</TableCell>
                                                    <TableCell>
                                                        <Badge variant="outline" className="text-xs text-muted-foreground">
                                                            {t(`i3xPage.status_${alarm.status}`, { defaultValue: alarm.status })}
                                                        </Badge>
                                                    </TableCell>
                                                    <TableCell className="text-xs text-muted-foreground">{formatTsFull(alarm.triggerTime)}</TableCell>
                                                    <TableCell className="text-xs text-muted-foreground">{alarm.clearTime ? formatTsFull(alarm.clearTime) : '—'}</TableCell>
                                                </TableRow>
                                            ))}
                                        </TableBody>
                                    </Table>
                                </div>
                            )}
                        </CardContent>
                    </Card>
                </TabsContent>

                {/* ── API Reference ── */}
                <TabsContent value="reference" className="mt-4 space-y-4">
                    <Card className="clip-chamfer-sm">
                        <CardHeader className="pb-3 border-b">
                            <CardTitle className="text-sm flex items-center gap-2">
                                <BookOpen size={15} className="text-primary" />
                                {t('i3xPage.endpoints_title')}
                            </CardTitle>
                            <p className="text-xs text-muted-foreground mt-1">
                                {t('i3xPage.auth')} <code className="bg-muted px-1 rounded text-xs">Authorization: Bearer &lt;token&gt;</code> — {t('i3xPage.multitenancy')} <code className="bg-muted px-1 rounded text-xs">X-Organization-ID: &lt;n&gt;</code>
                            </p>
                        </CardHeader>
                        <CardContent className="p-0">
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead className="text-xs w-16">{t('i3xPage.col_method')}</TableHead>
                                        <TableHead className="text-xs">{t('i3xPage.col_endpoint')}</TableHead>
                                        <TableHead className="text-xs">{t('i3xPage.col_description')}</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {API_ENDPOINTS.map(ep => (
                                        <TableRow key={ep.path + ep.method}>
                                            <TableCell>
                                                <Badge variant="outline" className={cn('text-xs font-mono', METHOD_COLOR[ep.method] ?? '')}>
                                                    {ep.method}
                                                </Badge>
                                            </TableCell>
                                            <TableCell className="font-mono text-xs">{ep.path}</TableCell>
                                            <TableCell className="text-xs text-muted-foreground">{t(ep.desc)}</TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        </CardContent>
                    </Card>

                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                        {/* ID mapping */}
                        <Card className="clip-chamfer-sm">
                            <CardHeader className="pb-3 border-b">
                                <CardTitle className="text-sm flex items-center gap-2">
                                    <ArrowRight size={15} className="text-primary" />
                                    {t('i3xPage.id_mapping')}
                                </CardTitle>
                            </CardHeader>
                            <CardContent className="p-4 space-y-2">
                                {[
                                    { from: 'Organization (id: 1)', to: 'Assembly "org-1"',  color: 'text-slate-600' },
                                    { from: 'Site (id: 5)',         to: 'Assembly "site-5"', color: 'text-slate-600' },
                                    { from: 'Area (id: 2)',         to: 'Assembly "area-2"', color: 'text-slate-600' },
                                    { from: 'Gateway (id: 3)',      to: 'Equipment "gw-3"',  color: 'text-blue-600'  },
                                    { from: 'Tag (id: 42)',         to: 'Property "tag-42"', color: 'text-purple-600'},
                                    { from: 'AlarmEvent (id: 7)',   to: 'Alarm "alarm-7"',   color: 'text-red-600'   },
                                ].map(m => (
                                    <div key={m.from} className="flex items-center gap-2 text-xs p-2 bg-muted/40 clip-chamfer-sm">
                                        <code className="text-muted-foreground shrink-0">{m.from}</code>
                                        <ArrowRight size={10} className="text-muted-foreground shrink-0" />
                                        <code className={cn('font-semibold', m.color)}>{m.to}</code>
                                    </div>
                                ))}
                            </CardContent>
                        </Card>

                        {/* Quality codes + auth notes */}
                        <div className="space-y-4">
                            <Card className="clip-chamfer-sm">
                                <CardHeader className="pb-3 border-b">
                                    <CardTitle className="text-sm flex items-center gap-2">
                                        <Tag size={15} className="text-primary" />
                                        {t('i3xPage.quality_codes')}
                                    </CardTitle>
                                </CardHeader>
                                <CardContent className="p-4 space-y-2">
                                    {[
                                        { q: 192, label: 'i3xPage.quality_192' },
                                        { q: 64,  label: 'i3xPage.quality_64' },
                                        { q: 0,   label: 'i3xPage.quality_0' },
                                    ].map(({ q, label }) => (
                                        <div key={q} className="flex items-center gap-3 text-xs">
                                            <QualityBadge quality={q} />
                                            <span className="text-muted-foreground">{t(label)}</span>
                                        </div>
                                    ))}
                                </CardContent>
                            </Card>

                            <Card className="clip-chamfer-sm">
                                <CardHeader className="pb-3 border-b">
                                    <CardTitle className="text-sm flex items-center gap-2">
                                        <CheckCircle2 size={15} className="text-green-500" />
                                        {t('i3xPage.permissions')}
                                    </CardTitle>
                                </CardHeader>
                                <CardContent className="p-4 space-y-2 text-xs text-muted-foreground">
                                    <p><Badge variant="outline" className="text-xs mr-1">{t('i3xPage.perm_any_role')}</Badge> {t('i3xPage.perm_read')}</p>
                                    <p><Badge variant="outline" className="text-xs mr-1 text-orange-600 border-orange-400/40">i3x_write</Badge> {t('i3xPage.perm_write')}</p>
                                    <p><Badge variant="outline" className="text-xs mr-1 text-blue-600 border-blue-400/40">admin</Badge> {t('i3xPage.perm_admin')}</p>
                                </CardContent>
                            </Card>
                        </div>
                    </div>
                </TabsContent>
            </Tabs>
        </div>
    );
}
