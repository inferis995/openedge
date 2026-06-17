import { useEffect, useState, useMemo } from 'react';
import { Server, Wifi, WifiOff, ShieldCheck, ShieldAlert, Search } from 'lucide-react';
import { infrastructureApi, GatewayInfraEntry, InfrastructureResponse } from '@/api/infrastructure';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';

type ViewMode = 'table' | 'tree';
type FilterStatus = 'all' | 'online' | 'offline';

const InfrastructurePage = () => {
    const [data, setData] = useState<InfrastructureResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [search, setSearch] = useState('');
    const [driverFilter, setDriverFilter] = useState('');
    const [statusFilter, setStatusFilter] = useState<FilterStatus>('all');
    const [viewMode, setViewMode] = useState<ViewMode>('table');

    useEffect(() => {
        infrastructureApi.list().then(setData).catch((err: unknown) => {
            console.error('Failed to load infrastructure data', err);
        }).finally(() => setLoading(false));
    }, []);

    const driverTypes = useMemo(() => {
        if (!data) return [];
        return [...new Set(data.gateways.map(g => g.driver_type))].sort();
    }, [data]);

    const filtered = useMemo(() => {
        if (!data) return [];
        return data.gateways.filter(g => {
            const searchLower = search.toLowerCase();
            const matchesSearch = !search ||
                g.name.toLowerCase().includes(searchLower) ||
                g.org_name.toLowerCase().includes(searchLower) ||
                g.host.toLowerCase().includes(searchLower);
            const matchesDriver = !driverFilter || g.driver_type === driverFilter;
            const matchesStatus = statusFilter === 'all' ||
                (statusFilter === 'online' && g.online) ||
                (statusFilter === 'offline' && !g.online);
            return matchesSearch && matchesDriver && matchesStatus;
        });
    }, [data, search, driverFilter, statusFilter]);

    const groupedByOrg = useMemo(() => {
        const groups = new Map<string, GatewayInfraEntry[]>();
        for (const g of filtered) {
            const key = g.org_name;
            if (!groups.has(key)) groups.set(key, []);
            groups.get(key)!.push(g);
        }
        return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b));
    }, [filtered]);

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="text-muted-foreground">Caricamento...</div>
            </div>
        );
    }

    return (
        <div className="p-6 space-y-6">
            <div className="flex items-center gap-3">
                <Server className="h-8 w-8 text-primary" />
                <div>
                    <h1 className="text-2xl font-bold">Infrastruttura</h1>
                    <p className="text-muted-foreground text-sm">Gateway e dispositivi edge</p>
                </div>
            </div>

            {/* Summary */}
            {data && (
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                    <Card>
                        <CardContent className="pt-6 text-center">
                            <div className="text-3xl font-bold">{data.summary.total}</div>
                            <div className="text-sm text-muted-foreground mt-1">Totale gateway</div>
                        </CardContent>
                    </Card>
                    <Card>
                        <CardContent className="pt-6 text-center">
                            <div className="text-3xl font-bold text-green-600">{data.summary.online}</div>
                            <div className="text-sm text-muted-foreground mt-1">Online</div>
                        </CardContent>
                    </Card>
                    <Card>
                        <CardContent className="pt-6 text-center">
                            <div className="text-3xl font-bold text-red-600">{data.summary.offline}</div>
                            <div className="text-sm text-muted-foreground mt-1">Offline</div>
                        </CardContent>
                    </Card>
                    <Card>
                        <CardContent className="pt-6 text-center">
                            <div className="text-3xl font-bold text-yellow-600">{data.summary.tls_missing}</div>
                            <div className="text-sm text-muted-foreground mt-1">TLS mancante</div>
                        </CardContent>
                    </Card>
                </div>
            )}

            {/* Filters */}
            <Card>
                <CardHeader>
                    <div className="flex flex-wrap gap-3 items-center">
                        <div className="relative flex-1 min-w-48">
                            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                            <Input
                                placeholder="Cerca per nome, org, IP..."
                                value={search}
                                onChange={e => setSearch(e.target.value)}
                                className="pl-8"
                            />
                        </div>
                        <select
                            className="border rounded-md px-3 py-2 text-sm bg-background"
                            value={driverFilter}
                            onChange={e => setDriverFilter(e.target.value)}
                        >
                            <option value="">Tutti i driver</option>
                            {driverTypes.map(d => (
                                <option key={d} value={d}>{d}</option>
                            ))}
                        </select>
                        <select
                            className="border rounded-md px-3 py-2 text-sm bg-background"
                            value={statusFilter}
                            onChange={e => setStatusFilter(e.target.value as FilterStatus)}
                        >
                            <option value="all">Tutti</option>
                            <option value="online">Online</option>
                            <option value="offline">Offline</option>
                        </select>
                        <div className="flex border rounded-md overflow-hidden">
                            <button
                                className={cn('px-3 py-2 text-sm', viewMode === 'table' ? 'bg-primary text-primary-foreground' : 'bg-background')}
                                onClick={() => setViewMode('table')}
                            >
                                Tabella
                            </button>
                            <button
                                className={cn('px-3 py-2 text-sm border-l', viewMode === 'tree' ? 'bg-primary text-primary-foreground' : 'bg-background')}
                                onClick={() => setViewMode('tree')}
                            >
                                Struttura
                            </button>
                        </div>
                    </div>
                </CardHeader>
                <CardContent className="p-0">
                    {viewMode === 'table' ? (
                        <div className="overflow-x-auto">
                            <table className="w-full text-sm">
                                <thead>
                                    <tr className="border-b bg-muted/50">
                                        <th className="text-left p-3 font-medium">Nome</th>
                                        <th className="text-left p-3 font-medium">Organizzazione</th>
                                        <th className="text-left p-3 font-medium">Host/IP</th>
                                        <th className="text-left p-3 font-medium">Porta</th>
                                        <th className="text-left p-3 font-medium">Driver</th>
                                        <th className="text-left p-3 font-medium">Stato</th>
                                        <th className="text-left p-3 font-medium">TLS</th>
                                        <th className="text-left p-3 font-medium">Auth</th>
                                        <th className="text-left p-3 font-medium">Last seen</th>
                                        <th className="text-left p-3 font-medium">Versione</th>
                                        <th className="text-left p-3 font-medium">Tag</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {filtered.map(g => (
                                        <tr key={g.id} className="border-b hover:bg-muted/30">
                                            <td className="p-3 font-medium">{g.name}</td>
                                            <td className="p-3 text-muted-foreground">{g.org_name}</td>
                                            <td className="p-3 font-mono text-xs">{g.host || '-'}</td>
                                            <td className="p-3 font-mono text-xs">{g.port || '-'}</td>
                                            <td className="p-3">
                                                <Badge variant="outline" className="text-xs">{g.driver_type}</Badge>
                                            </td>
                                            <td className="p-3">
                                                {g.online ? (
                                                    <span className="flex items-center gap-1 text-green-600">
                                                        <Wifi className="h-3 w-3" /> Online
                                                    </span>
                                                ) : (
                                                    <span className="flex items-center gap-1 text-red-600">
                                                        <WifiOff className="h-3 w-3" /> Offline
                                                    </span>
                                                )}
                                            </td>
                                            <td className="p-3">
                                                {g.tls_enabled
                                                    ? <ShieldCheck className="h-4 w-4 text-green-600" />
                                                    : <ShieldAlert className="h-4 w-4 text-red-500" />
                                                }
                                            </td>
                                            <td className="p-3">
                                                {g.mqtt_auth
                                                    ? <ShieldCheck className="h-4 w-4 text-green-600" />
                                                    : <ShieldAlert className="h-4 w-4 text-red-500" />
                                                }
                                            </td>
                                            <td className="p-3 text-xs text-muted-foreground">
                                                {g.last_seen_at
                                                    ? new Date(g.last_seen_at).toLocaleString('it-IT')
                                                    : '-'}
                                            </td>
                                            <td className="p-3 font-mono text-xs">{g.agent_version ?? '-'}</td>
                                            <td className="p-3 text-center">{g.tag_count}</td>
                                        </tr>
                                    ))}
                                    {filtered.length === 0 && (
                                        <tr>
                                            <td colSpan={11} className="p-8 text-center text-muted-foreground">
                                                Nessun gateway trovato
                                            </td>
                                        </tr>
                                    )}
                                </tbody>
                            </table>
                        </div>
                    ) : (
                        <div className="divide-y">
                            {groupedByOrg.map(([orgName, gateways]) => (
                                <div key={orgName}>
                                    <div className="px-4 py-2 bg-muted/30 font-semibold text-sm flex items-center gap-2">
                                        <Server className="h-4 w-4" />
                                        {orgName}
                                        <Badge variant="outline" className="ml-auto">{gateways.length} gateway</Badge>
                                    </div>
                                    {gateways.map(g => (
                                        <div key={g.id} className="flex items-center gap-3 px-8 py-2 hover:bg-muted/20 text-sm">
                                            <span className={g.online ? 'text-green-600' : 'text-red-600'}>
                                                {g.online ? <Wifi className="h-3 w-3" /> : <WifiOff className="h-3 w-3" />}
                                            </span>
                                            <span className="font-medium">{g.name}</span>
                                            <Badge variant="outline" className="text-xs">{g.driver_type}</Badge>
                                            <span className="font-mono text-xs text-muted-foreground">{g.host || '-'}:{g.port || '-'}</span>
                                            {g.tls_enabled
                                                ? <ShieldCheck className="h-3 w-3 text-green-600 ml-auto" />
                                                : <ShieldAlert className="h-3 w-3 text-red-500 ml-auto" />
                                            }
                                            <span className="text-xs text-muted-foreground">{g.tag_count} tag</span>
                                        </div>
                                    ))}
                                </div>
                            ))}
                            {groupedByOrg.length === 0 && (
                                <div className="p-8 text-center text-muted-foreground">Nessun gateway trovato</div>
                            )}
                        </div>
                    )}
                </CardContent>
            </Card>
        </div>
    );
};

export default InfrastructurePage;
