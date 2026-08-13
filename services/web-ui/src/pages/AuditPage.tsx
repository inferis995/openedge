import { useState, useCallback } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
    Shield,
    CheckCircle2,
    XCircle,
    ChevronDown,
    ChevronRight,
    RefreshCw,
    Filter,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { auditApi, AuditLog, AuditFilters } from '@/api/audit';

const PRESET_RANGES = [
    { label: 'Last 1h', hours: 1 },
    { label: 'Last 6h', hours: 6 },
    { label: 'Last 24h', hours: 24 },
    { label: 'Last 7d', hours: 168 },
    { label: 'Last 30d', hours: 720 },
] as const;

const ACTION_COLORS: Record<string, string> = {
    'tag.write': 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300',
    'auth.login': 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300',
    'auth.logout': 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300',
    'recipe.load': 'bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300',
    'user.create': 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300',
    'user.delete': 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300',
    'org.create': 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900/30 dark:text-indigo-300',
    'org.delete': 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300',
};

const actionColor = (action: string) =>
    ACTION_COLORS[action] ?? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300';

function formatTs(iso: string): string {
    const d = new Date(iso);
    return d.toLocaleString(undefined, {
        month: 'short', day: '2-digit',
        hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
}

function DetailsCell({ details }: { details: Record<string, unknown> | null }) {
    const [open, setOpen] = useState(false);
    if (!details || Object.keys(details).length === 0) return <span className="text-muted-foreground text-xs">—</span>;
    return (
        <div>
            <button
                onClick={() => setOpen(v => !v)}
                className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
                {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                {open ? 'hide' : 'show'}
            </button>
            {open && (
                <pre className="mt-2 rounded bg-muted px-3 py-2 text-xs leading-relaxed font-mono overflow-auto max-h-40 max-w-sm">
                    {JSON.stringify(details, null, 2)}
                </pre>
            )}
        </div>
    );
}

export default function AuditPage() {
    const [rangeHours, setRangeHours] = useState(24);
    const [actionFilter, setActionFilter] = useState('');
    const [successFilter, setSuccessFilter] = useState<'' | 'true' | 'false'>('');

    const buildFilters = useCallback((): AuditFilters => {
        const start = new Date(Date.now() - rangeHours * 3600 * 1000).toISOString();
        const f: AuditFilters = { start, limit: 500 };
        if (actionFilter) f.action = actionFilter;
        if (successFilter !== '') f.success = successFilter;
        return f;
    }, [rangeHours, actionFilter, successFilter]);

    const { data: logs = [], isFetching, refetch } = useQuery<AuditLog[]>({
        queryKey: ['audit-logs', rangeHours, actionFilter, successFilter],
        queryFn: () => auditApi.getLogs(buildFilters()),
        refetchInterval: 30_000,
    });

    const { data: actions = [] } = useQuery<string[]>({
        queryKey: ['audit-actions'],
        queryFn: () => auditApi.getActions(),
        staleTime: 5 * 60_000,
    });

    const successCount = logs.filter(l => l.success).length;
    const failCount = logs.length - successCount;

    return (
        <div className="p-6 space-y-6">
            {/* Header */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex items-center gap-3">
                    <div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
                        <Shield className="h-5 w-5 text-primary" />
                    </div>
                    <div>
                        <h1 className="text-2xl font-bold">Audit Log</h1>
                        <p className="text-sm text-muted-foreground">Track every operator action for compliance</p>
                    </div>
                </div>
                <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
                    <RefreshCw className={`h-4 w-4 mr-2 ${isFetching ? 'animate-spin' : ''}`} />
                    Refresh
                </Button>
            </div>

            {/* Summary cards */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                <div className="rounded-xl border bg-card p-4">
                    <p className="text-sm text-muted-foreground">Total events</p>
                    <p className="text-3xl font-bold mt-1">{logs.length}</p>
                </div>
                <div className="rounded-xl border bg-card p-4">
                    <p className="text-sm text-muted-foreground flex items-center gap-1.5">
                        <CheckCircle2 className="h-4 w-4 text-green-500" /> Successful
                    </p>
                    <p className="text-3xl font-bold mt-1 text-green-600 dark:text-green-400">{successCount}</p>
                </div>
                <div className="rounded-xl border bg-card p-4">
                    <p className="text-sm text-muted-foreground flex items-center gap-1.5">
                        <XCircle className="h-4 w-4 text-red-500" /> Failed
                    </p>
                    <p className="text-3xl font-bold mt-1 text-red-600 dark:text-red-400">{failCount}</p>
                </div>
            </div>

            {/* Filters */}
            <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-card p-4">
                <Filter className="h-4 w-4 text-muted-foreground" />
                <Select value={String(rangeHours)} onValueChange={v => setRangeHours(Number(v))}>
                    <SelectTrigger className="w-36">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {PRESET_RANGES.map(r => (
                            <SelectItem key={r.hours} value={String(r.hours)}>{r.label}</SelectItem>
                        ))}
                    </SelectContent>
                </Select>

                <Select value={actionFilter || 'all'} onValueChange={v => setActionFilter(v === 'all' ? '' : v)}>
                    <SelectTrigger className="w-44">
                        <SelectValue placeholder="All actions" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">All actions</SelectItem>
                        {actions.map(a => (
                            <SelectItem key={a} value={a}>{a}</SelectItem>
                        ))}
                    </SelectContent>
                </Select>

                <Select value={successFilter || 'all'} onValueChange={v => setSuccessFilter(v === 'all' ? '' : v as 'true' | 'false')}>
                    <SelectTrigger className="w-36">
                        <SelectValue placeholder="All results" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">All results</SelectItem>
                        <SelectItem value="true">Success only</SelectItem>
                        <SelectItem value="false">Failures only</SelectItem>
                    </SelectContent>
                </Select>

                <span className="ml-auto text-xs text-muted-foreground">
                    {logs.length} event{logs.length !== 1 ? 's' : ''} · refreshes every 30s
                </span>
            </div>

            {/* Table */}
            <div className="rounded-xl border bg-card overflow-hidden">
                <div className="overflow-x-auto">
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="border-b bg-muted/40">
                                <th className="text-left px-4 py-3 font-medium text-muted-foreground w-44">Timestamp</th>
                                <th className="text-left px-4 py-3 font-medium text-muted-foreground w-32">User</th>
                                <th className="text-left px-4 py-3 font-medium text-muted-foreground w-44">Action</th>
                                <th className="text-left px-4 py-3 font-medium text-muted-foreground w-20">Result</th>
                                <th className="text-left px-4 py-3 font-medium text-muted-foreground w-32">IP</th>
                                <th className="text-left px-4 py-3 font-medium text-muted-foreground">Details</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y">
                            {logs.length === 0 && (
                                <tr>
                                    <td colSpan={6} className="px-4 py-12 text-center text-muted-foreground">
                                        {isFetching ? 'Loading…' : 'No audit events in the selected time range.'}
                                    </td>
                                </tr>
                            )}
                            {logs.map(log => (
                                <tr key={log.id} className="hover:bg-muted/30 transition-colors">
                                    <td className="px-4 py-3 font-mono text-xs text-muted-foreground whitespace-nowrap">
                                        {formatTs(log.created_at)}
                                    </td>
                                    <td className="px-4 py-3 font-medium truncate max-w-[120px]" title={log.username}>
                                        {log.username || <span className="text-muted-foreground italic">system</span>}
                                    </td>
                                    <td className="px-4 py-3">
                                        <span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${actionColor(log.action)}`}>
                                            {log.action}
                                        </span>
                                    </td>
                                    <td className="px-4 py-3">
                                        {log.success
                                            ? <Badge variant="outline" className="border-green-500 text-green-600 dark:text-green-400">OK</Badge>
                                            : <Badge variant="outline" className="border-red-500 text-red-600 dark:text-red-400">FAIL</Badge>
                                        }
                                    </td>
                                    <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                                        {log.ip_address || '—'}
                                    </td>
                                    <td className="px-4 py-3">
                                        <DetailsCell details={log.details} />
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    );
}
