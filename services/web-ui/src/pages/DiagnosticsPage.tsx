import { useQuery } from '@tanstack/react-query';
import { Activity, Cpu, HardDrive, MemoryStick, Network as NetIcon, ServerCog } from 'lucide-react';

import { diagnosticsApi, DiskInfo, NetworkIfInfo, ServiceHealth } from '@/api/diagnostics';
import { Badge } from '@/components/ui/badge';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';

// Auto-refresh interval (ms). Short enough that a disk filling fast or a
// stuck broker becomes visible quickly; long enough not to spam.
const REFRESH_INTERVAL = 5_000;

const formatBytes = (n: number): string => {
    if (n < 1024) return `${n} B`;
    const units = ['KB', 'MB', 'GB', 'TB'];
    let v = n / 1024, i = 0;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return `${v.toFixed(1)} ${units[i]}`;
};

const formatDuration = (sec: number): string => {
    if (!sec || sec < 0) return '—';
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (d > 0) return `${d}d ${h}h`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
};

// Usage colour: green up to 75, amber to 90, red above.
const usageBarColour = (pct: number): string => {
    if (pct >= 90) return 'bg-red-500';
    if (pct >= 75) return 'bg-amber-500';
    return 'bg-emerald-500';
};

const HealthDot = ({ h }: { h: ServiceHealth }) => (
    <span
        className={`inline-block w-2.5 h-2.5 rounded-full mr-2 ${h.ok ? 'bg-emerald-500' : 'bg-red-500'}`}
        title={h.message ?? (h.ok ? 'healthy' : 'down')}
    />
);

const UsageBar = ({ pct }: { pct: number }) => (
    <div className="w-full h-2 bg-muted rounded">
        <div className={`h-2 rounded ${usageBarColour(pct)}`} style={{ width: `${Math.min(100, pct).toFixed(0)}%` }} />
    </div>
);

const Card = ({ icon, title, children }: { icon: React.ReactNode; title: string; children: React.ReactNode }) => (
    <div className="rounded-md border bg-card p-4 space-y-3">
        <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
            {icon} <span>{title}</span>
        </div>
        {children}
    </div>
);

const DiagnosticsPage = () => {
    const { data, isLoading, isError, error } = useQuery({
        queryKey: ['diagnostics'],
        queryFn: diagnosticsApi.get,
        refetchInterval: REFRESH_INTERVAL,
    });

    if (isLoading) return <div className="p-8 text-muted-foreground">Loading...</div>;
    if (isError) return <div className="p-8 text-red-500">Failed to load diagnostics: {(error as Error)?.message}</div>;
    if (!data) return null;

    return (
        <div className="space-y-6">
            <div>
                <h2 className="text-2xl font-bold tracking-tight flex items-center gap-2">
                    <Activity size={22} /> System diagnostics
                </h2>
                <p className="text-muted-foreground">
                    Live snapshot of host hardware + back-end services. Refreshes every {REFRESH_INTERVAL / 1000}s.
                </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">

                <Card icon={<ServerCog size={16} />} title="Host">
                    <div className="space-y-1 text-sm">
                        <div className="flex justify-between"><span className="text-muted-foreground">Hostname</span><span className="font-mono">{data.host.hostname}</span></div>
                        <div className="flex justify-between"><span className="text-muted-foreground">OS</span><span>{data.host.os} {data.host.arch}</span></div>
                        {data.host.kernel && (
                            <div className="flex justify-between"><span className="text-muted-foreground">Kernel</span><span className="font-mono text-xs">{data.host.kernel}</span></div>
                        )}
                        <div className="flex justify-between"><span className="text-muted-foreground">Host uptime</span><span>{formatDuration(data.host.uptime_sec ?? 0)}</span></div>
                        <div className="flex justify-between"><span className="text-muted-foreground">API uptime</span><span>{formatDuration(data.host.api_uptime_sec)}</span></div>
                        {data.host.load_average && (
                            <div className="flex justify-between"><span className="text-muted-foreground">Load avg (1/5/15)</span>
                                <span className="font-mono text-xs">{data.host.load_average.map((x) => x.toFixed(2)).join(' / ')}</span>
                            </div>
                        )}
                    </div>
                </Card>

                {data.cpu && (
                    <Card icon={<Cpu size={16} />} title="CPU">
                        <div className="space-y-2 text-sm">
                            <div className="flex justify-between"><span className="text-muted-foreground">Cores</span><span>{data.cpu.cores}</span></div>
                            {data.cpu.model_name && (
                                <div className="flex justify-between gap-2">
                                    <span className="text-muted-foreground shrink-0">Model</span>
                                    <span className="text-xs text-right truncate" title={data.cpu.model_name}>{data.cpu.model_name}</span>
                                </div>
                            )}
                            {data.cpu.usage_pct !== undefined && (
                                <div className="space-y-1">
                                    <div className="flex justify-between text-xs"><span>Usage</span><span>{data.cpu.usage_pct.toFixed(0)}%</span></div>
                                    <UsageBar pct={data.cpu.usage_pct} />
                                </div>
                            )}
                        </div>
                    </Card>
                )}

                {data.memory && (
                    <Card icon={<MemoryStick size={16} />} title="Memory">
                        <div className="space-y-2 text-sm">
                            <div className="flex justify-between"><span className="text-muted-foreground">Total</span><span>{formatBytes(data.memory.total_bytes)}</span></div>
                            <div className="flex justify-between"><span className="text-muted-foreground">Available</span><span>{formatBytes(data.memory.available_bytes)}</span></div>
                            <div className="space-y-1">
                                <div className="flex justify-between text-xs"><span>Used</span><span>{data.memory.used_pct.toFixed(0)}%</span></div>
                                <UsageBar pct={data.memory.used_pct} />
                            </div>
                        </div>
                    </Card>
                )}

                <Card icon={<HardDrive size={16} />} title="Disk">
                    {(data.disk?.length ?? 0) === 0
                        ? <p className="text-sm text-muted-foreground">No mount points reported.</p>
                        : (
                            <div className="space-y-3 text-sm">
                                {data.disk!.map((d: DiskInfo) => (
                                    <div key={d.path} className="space-y-1">
                                        <div className="flex justify-between text-xs">
                                            <span className="font-mono">{d.path}</span>
                                            <span>{formatBytes(d.used_bytes)} / {formatBytes(d.total_bytes)} ({d.used_pct.toFixed(0)}%)</span>
                                        </div>
                                        <UsageBar pct={d.used_pct} />
                                    </div>
                                ))}
                            </div>
                        )}
                </Card>

                <Card icon={<NetIcon size={16} />} title="Network">
                    {(data.network?.length ?? 0) === 0
                        ? <p className="text-sm text-muted-foreground">No physical interfaces detected.</p>
                        : (
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>Iface</TableHead>
                                        <TableHead>Link</TableHead>
                                        <TableHead>RX</TableHead>
                                        <TableHead>TX</TableHead>
                                        <TableHead>Errors</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {data.network!.map((n: NetworkIfInfo) => (
                                        <TableRow key={n.name}>
                                            <TableCell className="font-mono text-xs">{n.name}</TableCell>
                                            <TableCell>
                                                <Badge className={n.link_up
                                                    ? 'bg-emerald-500/10 text-emerald-500 border-none'
                                                    : 'bg-red-500/10 text-red-500 border-none'}>
                                                    {n.link_up ? 'up' : 'down'}
                                                </Badge>
                                            </TableCell>
                                            <TableCell className="text-xs">{formatBytes(n.rx_bytes)}</TableCell>
                                            <TableCell className="text-xs">{formatBytes(n.tx_bytes)}</TableCell>
                                            <TableCell className="text-xs">{n.rx_errors + n.tx_errors}</TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        )}
                </Card>

                <Card icon={<Activity size={16} />} title="Services">
                    <div className="space-y-2 text-sm">
                        {Object.entries(data.services).map(([name, h]) => (
                            <div key={name} className="flex items-center justify-between">
                                <span className="flex items-center"><HealthDot h={h} /><span className="font-mono">{name}</span></span>
                                <span className="text-xs text-muted-foreground">
                                    {h.ok ? (h.latency_ms ? `${h.latency_ms} ms` : 'ok') : (h.message ?? 'down')}
                                </span>
                            </div>
                        ))}
                    </div>
                </Card>

            </div>
        </div>
    );
};

export default DiagnosticsPage;
