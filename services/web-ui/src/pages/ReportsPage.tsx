import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Download, FileText, BellRing, ShieldCheck, Shield, Server } from 'lucide-react';

import api from '@/api/client';
import { tagsApi } from '@/api/tags';
import { useAuthStore } from '@/stores/useAuthStore';
import { showApiError } from '@/lib/api-error-handler';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { securityApi } from '@/api/security';
import { infrastructureApi } from '@/api/infrastructure';

// Default ISO-8601 helpers used by the date inputs. We work in the
// operator's LOCAL timezone in the UI; the API converts back to UTC.
const toLocalInput = (d: Date) => {
    const pad = (n: number) => `${n}`.padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
};
const fromLocalInput = (s: string): string => new Date(s).toISOString();

const defaultEnd = () => toLocalInput(new Date());
const defaultStart = () => {
    const d = new Date();
    d.setHours(d.getHours() - 24);
    return toLocalInput(d);
};

interface ExportCardProps {
    icon: React.ReactNode;
    title: string;
    description: string;
    onExport: () => void;
    extraControls?: React.ReactNode;
    busy?: boolean;
    disabled?: boolean;
}

const ExportCard = ({ icon, title, description, onExport, extraControls, busy, disabled }: ExportCardProps) => (
    <div className="rounded-md border bg-card p-4 space-y-3">
        <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">{icon}{title}</div>
        <p className="text-sm">{description}</p>
        {extraControls}
        <Button onClick={onExport} disabled={busy || disabled} className="gap-2">
            <Download size={16} /> {busy ? 'Preparing...' : 'Download CSV'}
        </Button>
    </div>
);

const ReportsPage = () => {
    const { isAdmin } = useAuthStore();

    const [start, setStart] = useState(defaultStart());
    const [end, setEnd] = useState(defaultEnd());
    const [selectedTags, setSelectedTags] = useState<number[]>([]);
    const [busy, setBusy] = useState<string | null>(null);

    const { data: tags = [] } = useQuery({
        queryKey: ['tags-with-hierarchy'],
        queryFn: tagsApi.getAllWithHierarchy,
        staleTime: 60_000,
    });

    const range = useMemo(() => {
        try {
            return { start: fromLocalInput(start), end: fromLocalInput(end) };
        } catch {
            return null;
        }
    }, [start, end]);

    // Triggers a browser download for a CSV endpoint, attaching the
    // Authorization header axios already injects. We use the axios
    // client directly (rather than building a URL with the token in a
    // query string) so the JWT never leaks into browser history.
    const download = async (path: string, filename: string, params: Record<string, string>) => {
        if (!range) {
            showApiError(new Error('Invalid date range'), 'Invalid date range');
            return;
        }
        setBusy(path);
        try {
            const res = await api.get(path, {
                params: { ...params, start: range.start, end: range.end },
                responseType: 'blob',
            });
            const url = window.URL.createObjectURL(new Blob([res.data], { type: 'text/csv' }));
            const a = document.createElement('a');
            a.href = url;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            a.remove();
            window.URL.revokeObjectURL(url);
        } catch (e) {
            showApiError(e, 'Export failed');
        } finally {
            setBusy(null);
        }
    };

    const toggleTag = (id: number) =>
        setSelectedTags((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));

    return (
        <div className="space-y-6">
            <div>
                <h2 className="text-2xl font-bold tracking-tight flex items-center gap-2">
                    <FileText size={22} /> Reports
                </h2>
                <p className="text-muted-foreground">
                    Export historian, alarms and audit data as CSV. Files open directly in Excel /
                    LibreOffice and import cleanly into BI tools.
                </p>
            </div>

            <div className="rounded-md border bg-card p-4 grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="grid gap-1">
                    <Label htmlFor="rep-start">Start (local time)</Label>
                    <Input id="rep-start" type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} />
                </div>
                <div className="grid gap-1">
                    <Label htmlFor="rep-end">End (local time)</Label>
                    <Input id="rep-end" type="datetime-local" value={end} onChange={(e) => setEnd(e.target.value)} />
                </div>
                <p className="text-xs text-muted-foreground md:col-span-2">
                    Max range: 90 days. The API converts the times to UTC before querying.
                </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">

                <ExportCard
                    icon={<FileText size={16} />}
                    title="Tag history"
                    description="Raw historian samples. Filter by selecting one or more tags below."
                    busy={busy === '/reports/history.csv'}
                    onExport={() => download('/reports/history.csv',
                        `history-${Date.now()}.csv`,
                        selectedTags.length ? { tag_ids: selectedTags.join(',') } : {})}
                    extraControls={
                        <div className="text-xs text-muted-foreground">
                            {selectedTags.length === 0
                                ? 'All tags will be included (large export possible).'
                                : `${selectedTags.length} tag${selectedTags.length === 1 ? '' : 's'} selected.`}
                        </div>
                    }
                />

                <ExportCard
                    icon={<BellRing size={16} />}
                    title="Alarm events"
                    description="Every alarm trigger / clear within the range with severity, value and message."
                    busy={busy === '/reports/alarms.csv'}
                    onExport={() => download('/reports/alarms.csv', `alarms-${Date.now()}.csv`, {})}
                />

                {isAdmin() && (
                    <ExportCard
                        icon={<ShieldCheck size={16} />}
                        title="Audit log"
                        description="System-wide actions (logins, tag writes, recipe loads, ...). Global admin only."
                        busy={busy === '/reports/audit.csv'}
                        onExport={() => download('/reports/audit.csv', `audit-${Date.now()}.csv`, {})}
                    />
                )}

            </div>

            {/* Conformità & Sicurezza section */}
            {isAdmin() && (
                <div className="space-y-3">
                    <h3 className="font-semibold flex items-center gap-2">
                        <Shield size={16} /> Conformità &amp; Sicurezza
                    </h3>
                    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                        <div className="rounded-md border bg-card p-4 space-y-3">
                            <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                                <ShieldCheck size={16} /> Report postura di sicurezza (JSON)
                            </div>
                            <p className="text-sm">Autovalutazione dei controlli automatici, modellati sulle misure dell'art. 21 NIS2. Non è una dichiarazione di conformità.</p>
                            <Button
                                className="gap-2"
                                onClick={async () => {
                                    try {
                                        const [overview, compliance] = await Promise.all([
                                            securityApi.overview(),
                                            securityApi.compliance(),
                                        ]);
                                        const report = {
                                            type: 'SECURITY_POSTURE_SELF_ASSESSMENT',
                                            disclaimer:
                                                'Autovalutazione automatica della postura di sicurezza, modellata sulle ' +
                                                'misure dell\'art. 21 della direttiva NIS2. NON costituisce una ' +
                                                'dichiarazione né una certificazione di conformità NIS2: i controlli con ' +
                                                'stato "not_assessed" riguardano misure organizzative che il software non ' +
                                                'può accertare e restano in capo al titolare dell\'impianto.',
                                            generated_at: new Date().toISOString(),
                                            security_score: overview.score,
                                            checks_passed: overview.checks_passed,
                                            checks_evaluated: overview.checks_evaluated,
                                            checks_not_assessed: overview.checks_not_assessed,
                                            checks: overview.checks,
                                            failed_logins_24h: overview.failed_logins_24h,
                                            locked_accounts: overview.locked_accounts,
                                            compliance_checks: compliance,
                                        };
                                        const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' });
                                        const url = URL.createObjectURL(blob);
                                        const a = document.createElement('a');
                                        a.href = url;
                                        a.download = `security-posture-${new Date().toISOString().slice(0, 10)}.json`;
                                        a.click();
                                        URL.revokeObjectURL(url);
                                    } catch (e) {
                                        showApiError(e, 'Export failed');
                                    }
                                }}
                            >
                                <Download size={16} /> Download JSON
                            </Button>
                        </div>

                        <div className="rounded-md border bg-card p-4 space-y-3">
                            <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                                <Shield size={16} /> Security Events (CSV)
                            </div>
                            <p className="text-sm">Ultimi 50 eventi di sicurezza — login falliti, account bloccati, accessi negati.</p>
                            <Button
                                className="gap-2"
                                onClick={async () => {
                                    try {
                                        const events = await securityApi.events(50);
                                        const headers = ['ID', 'Tipo', 'Gravità', 'Attore', 'Risorsa', 'Data'];
                                        const rows = events.map(e => [
                                            String(e.id),
                                            e.event_type,
                                            e.severity,
                                            e.actor ?? '',
                                            e.resource ?? '',
                                            new Date(e.created_at).toLocaleString('it-IT'),
                                        ]);
                                        const csv = [headers, ...rows].map(r => r.map(f => `"${f}"`).join(',')).join('\n');
                                        const blob = new Blob([csv], { type: 'text/csv' });
                                        const url = URL.createObjectURL(blob);
                                        const a = document.createElement('a');
                                        a.href = url;
                                        a.download = `security-events-${new Date().toISOString().slice(0, 10)}.csv`;
                                        a.click();
                                        URL.revokeObjectURL(url);
                                    } catch (e) {
                                        showApiError(e, 'Export failed');
                                    }
                                }}
                            >
                                <Download size={16} /> Download CSV
                            </Button>
                        </div>

                        <div className="rounded-md border bg-card p-4 space-y-3">
                            <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                                <Server size={16} /> Inventario Gateway (CSV)
                            </div>
                            <p className="text-sm">Elenco completo gateway con stato TLS, versione agente e stato online/offline.</p>
                            <Button
                                className="gap-2"
                                onClick={async () => {
                                    try {
                                        const { gateways } = await infrastructureApi.list();
                                        const headers = ['ID', 'Nome', 'Organizzazione', 'Driver', 'Host', 'Porta', 'Online', 'TLS', 'Auth', 'Versione', 'Tag'];
                                        const rows = gateways.map(g => [
                                            String(g.id),
                                            g.name,
                                            g.org_name,
                                            g.driver_type,
                                            g.host,
                                            String(g.port),
                                            g.online ? 'SI' : 'NO',
                                            g.tls_enabled ? 'SI' : 'NO',
                                            g.mqtt_auth ? 'SI' : 'NO',
                                            g.agent_version ?? '',
                                            String(g.tag_count),
                                        ]);
                                        const csv = [headers, ...rows].map(r => r.map(f => `"${f}"`).join(',')).join('\n');
                                        const blob = new Blob([csv], { type: 'text/csv' });
                                        const url = URL.createObjectURL(blob);
                                        const a = document.createElement('a');
                                        a.href = url;
                                        a.download = `gateway-inventory-${new Date().toISOString().slice(0, 10)}.csv`;
                                        a.click();
                                        URL.revokeObjectURL(url);
                                    } catch (e) {
                                        showApiError(e, 'Export failed');
                                    }
                                }}
                            >
                                <Download size={16} /> Download CSV
                            </Button>
                        </div>

                        <div className="rounded-md border bg-card p-4 space-y-3">
                            <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                                <ShieldCheck size={16} /> Security Score (JSON)
                            </div>
                            <p className="text-sm">Score di sicurezza con breakdown per categoria — adatto per audit esterni.</p>
                            <Button
                                className="gap-2"
                                onClick={async () => {
                                    try {
                                        const overview = await securityApi.overview();
                                        const blob = new Blob([JSON.stringify(overview, null, 2)], { type: 'application/json' });
                                        const url = URL.createObjectURL(blob);
                                        const a = document.createElement('a');
                                        a.href = url;
                                        a.download = `security-score-${new Date().toISOString().slice(0, 10)}.json`;
                                        a.click();
                                        URL.revokeObjectURL(url);
                                    } catch (e) {
                                        showApiError(e, 'Export failed');
                                    }
                                }}
                            >
                                <Download size={16} /> Download JSON
                            </Button>
                        </div>
                    </div>
                </div>
            )}

            <div className="rounded-md border bg-card p-4">
                <div className="flex items-center justify-between mb-3">
                    <h3 className="font-semibold">Tag filter for history export</h3>
                    {selectedTags.length > 0 && (
                        <Button variant="outline" size="sm" onClick={() => setSelectedTags([])}>Clear</Button>
                    )}
                </div>
                <div className="max-h-72 overflow-auto border rounded-md divide-y">
                    {tags.length === 0 ? (
                        <p className="p-4 text-sm text-muted-foreground">No tags loaded.</p>
                    ) : tags.map((t) => (
                        <label key={t.id} className="flex items-center gap-3 px-3 py-2 hover:bg-muted/30 cursor-pointer">
                            <input
                                type="checkbox"
                                className="accent-primary"
                                checked={selectedTags.includes(t.id)}
                                onChange={() => toggleTag(t.id)}
                            />
                            <span className="font-mono text-xs flex-1 truncate">{t.alias ?? t.code}</span>
                            <span className="text-xs text-muted-foreground">{t.gateway_name}</span>
                            <span className="text-xs text-muted-foreground">{t.data_type}</span>
                        </label>
                    ))}
                </div>
            </div>
        </div>
    );
};

export default ReportsPage;
