import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Download, FileText, BellRing, ShieldCheck, Shield, Server, FileSignature } from 'lucide-react';

import api from '@/api/client';
import { tagsApi } from '@/api/tags';
import { useAuthStore } from '@/stores/useAuthStore';
import { showApiError } from '@/lib/api-error-handler';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { securityApi } from '@/api/security';
import { infrastructureApi } from '@/api/infrastructure';
import { useTranslation } from 'react-i18next';

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

// Il mese appena concluso, che è quello che si vuole quasi sempre: il report
// si genera il primo del mese per il mese prima.
const lastMonth = (): string => {
    const d = new Date();
    d.setUTCDate(1);
    d.setUTCMonth(d.getUTCMonth() - 1);
    return d.toISOString().slice(0, 7);
};

// Il mese corrente non è ancora finito: offrirlo come limite superiore evita di
// generare un rapporto su un periodo che sta ancora cambiando.
const thisMonth = (): string => new Date().toISOString().slice(0, 7);

interface ExportCardProps {
    icon: React.ReactNode;
    title: string;
    description: string;
    onExport: () => void;
    extraControls?: React.ReactNode;
    busy?: boolean;
    disabled?: boolean;
    buttonLabel?: string;
}

const ExportCard = ({ icon, title, description, onExport, extraControls, busy, disabled, buttonLabel }: ExportCardProps) => {
    const { t } = useTranslation();
    return (
    <div className="rounded-md border bg-card p-4 space-y-3">
        <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">{icon}{title}</div>
        <p className="text-sm">{description}</p>
        {extraControls}
        <Button onClick={onExport} disabled={busy || disabled} className="gap-2">
            <Download size={16} /> {busy ? t('reportsPage.preparing') : (buttonLabel ?? t('reportsPage.download_csv'))}
        </Button>
    </div>
    );
};

const ReportsPage = () => {
    const { isAdmin, isGlobalAdmin } = useAuthStore();
    const { t: tr, i18n } = useTranslation();

    const [start, setStart] = useState(defaultStart());
    const [end, setEnd] = useState(defaultEnd());
    const [selectedTags, setSelectedTags] = useState<number[]>([]);
    const [busy, setBusy] = useState<string | null>(null);
    const [reportMonth, setReportMonth] = useState(lastMonth());

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
            showApiError(new Error('Invalid date range'), tr('reportsPage.invalid_range'));
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
            showApiError(e, tr('reportsPage.export_failed'));
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
                    <FileText size={22} /> {tr('reportsPage.title')}
                </h2>
                <p className="text-muted-foreground">
                    {tr('reportsPage.subtitle')}
                </p>
            </div>

            <div className="rounded-md border bg-card p-4 grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="grid gap-1">
                    <Label htmlFor="rep-start">{tr('reportsPage.start')}</Label>
                    <Input id="rep-start" type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} />
                </div>
                <div className="grid gap-1">
                    <Label htmlFor="rep-end">{tr('reportsPage.end')}</Label>
                    <Input id="rep-end" type="datetime-local" value={end} onChange={(e) => setEnd(e.target.value)} />
                </div>
                <p className="text-xs text-muted-foreground md:col-span-2">
                    {tr('reportsPage.range_hint')}
                </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">

                <ExportCard
                    icon={<FileText size={16} />}
                    title={tr('reportsPage.history_title')}
                    description={tr('reportsPage.history_desc')}
                    busy={busy === '/reports/history.csv'}
                    onExport={() => download('/reports/history.csv',
                        `history-${Date.now()}.csv`,
                        selectedTags.length ? { tag_ids: selectedTags.join(',') } : {})}
                    extraControls={
                        <div className="text-xs text-muted-foreground">
                            {selectedTags.length === 0
                                ? tr('reportsPage.all_tags_included')
                                : tr('reportsPage.tags_selected', { count: selectedTags.length })}
                        </div>
                    }
                />

                <ExportCard
                    icon={<BellRing size={16} />}
                    title={tr('reportsPage.alarms_title')}
                    description={tr('reportsPage.alarms_desc')}
                    busy={busy === '/reports/alarms.csv'}
                    onExport={() => download('/reports/alarms.csv', `alarms-${Date.now()}.csv`, {})}
                />

                <ExportCard
                    icon={<FileSignature size={16} />}
                    title={tr('reportsPage.service_title')}
                    description={tr('reportsPage.service_desc')}
                    busy={busy === '/reports/service-report.html'}
                    buttonLabel={tr('reportsPage.download_report')}
                    onExport={() => download('/reports/service-report.html',
                        `rapporto-servizio-${reportMonth}.html`, { month: reportMonth })}
                    extraControls={
                        <div className="space-y-1">
                            <Label htmlFor="rep-month" className="text-xs">{tr('reportsPage.month')}</Label>
                            <Input
                                id="rep-month"
                                type="month"
                                value={reportMonth}
                                max={thisMonth()}
                                onChange={(e) => setReportMonth(e.target.value)}
                            />
                            <p className="text-[11px] text-muted-foreground">
                                {tr('reportsPage.month_hint')}
                            </p>
                        </div>
                    }
                />

                {/* The server serves it to the global admin only; an org
                    admin saw the card and got a 403. */}
                {isGlobalAdmin() && (
                    <ExportCard
                        icon={<ShieldCheck size={16} />}
                        title={tr('reportsPage.audit_title')}
                        description={tr('reportsPage.audit_desc')}
                        busy={busy === '/reports/audit.csv'}
                        onExport={() => download('/reports/audit.csv', `audit-${Date.now()}.csv`, {})}
                    />
                )}

            </div>

            {/* Conformità & Sicurezza section */}
            {isAdmin() && (
                <div className="space-y-3">
                    <h3 className="font-semibold flex items-center gap-2">
                        <Shield size={16} /> {tr('reportsPage.compliance_title')}
                    </h3>
                    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                        <div className="rounded-md border bg-card p-4 space-y-3">
                            <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                                <ShieldCheck size={16} /> {tr('reportsPage.posture_title')}
                            </div>
                            <p className="text-sm">{tr('reportsPage.posture_desc')}</p>
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
                                                'Autovalutazione automatica dei controlli di sicurezza della piattaforma. ' +
                                                'NON costituisce una certificazione né una dichiarazione di conformità ' +
                                                'normativa: i controlli con stato "not_assessed" riguardano misure ' +
                                                'organizzative che il software non può accertare e restano in capo al ' +
                                                'titolare dell\'impianto.',
                                            generated_at: new Date().toISOString(),
                                            security_score: overview.score,
                                            checks_passed: overview.checks_passed,
                                            checks_evaluated: overview.checks_evaluated,
                                            checks_not_assessed: overview.checks_not_assessed,
                                            checks: overview.checks,
                                            failed_logins_24h: overview.failed_logins_24h,
                                            locked_accounts: overview.locked_accounts,
                                            controlli: compliance.map(c => ({ id: c.id, name: c.name, state: c.state, detail: c.detail })),
                                        };
                                        const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' });
                                        const url = URL.createObjectURL(blob);
                                        const a = document.createElement('a');
                                        a.href = url;
                                        a.download = `security-posture-${new Date().toISOString().slice(0, 10)}.json`;
                                        a.click();
                                        URL.revokeObjectURL(url);
                                    } catch (e) {
                                        showApiError(e, tr('reportsPage.export_failed'));
                                    }
                                }}
                            >
                                <Download size={16} /> {tr('reportsPage.download_json')}
                            </Button>
                        </div>

                        <div className="rounded-md border bg-card p-4 space-y-3">
                            <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                                <Shield size={16} /> {tr('reportsPage.sec_events_title')}
                            </div>
                            <p className="text-sm">{tr('reportsPage.sec_events_desc')}</p>
                            <Button
                                className="gap-2"
                                onClick={async () => {
                                    try {
                                        const events = await securityApi.events(50);
                                        const headers = [tr('reportsPage.csv_id'), tr('reportsPage.csv_type'), tr('reportsPage.csv_severity'), tr('reportsPage.csv_actor'), tr('reportsPage.csv_resource'), tr('reportsPage.csv_date')];
                                        const rows = events.map(e => [
                                            String(e.id),
                                            e.event_type,
                                            e.severity,
                                            e.actor ?? '',
                                            e.resource ?? '',
                                            new Date(e.created_at).toLocaleString(i18n.language),
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
                                        showApiError(e, tr('reportsPage.export_failed'));
                                    }
                                }}
                            >
                                <Download size={16} /> {tr('reportsPage.download_csv')}
                            </Button>
                        </div>

                        <div className="rounded-md border bg-card p-4 space-y-3">
                            <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                                <Server size={16} /> {tr('reportsPage.inventory_title')}
                            </div>
                            <p className="text-sm">{tr('reportsPage.inventory_desc')}</p>
                            <Button
                                className="gap-2"
                                onClick={async () => {
                                    try {
                                        const { gateways } = await infrastructureApi.list();
                                        const headers = [tr('reportsPage.csv_id'), tr('reportsPage.csv_name'), tr('reportsPage.csv_org'), tr('reportsPage.csv_driver'), tr('reportsPage.csv_host'), tr('reportsPage.csv_port'), tr('reportsPage.csv_online'), 'TLS', tr('reportsPage.csv_auth'), tr('reportsPage.csv_version'), tr('reportsPage.csv_tags')];
                                        const rows = gateways.map(g => [
                                            String(g.id),
                                            g.name,
                                            g.org_name,
                                            g.driver_type,
                                            g.host,
                                            String(g.port),
                                            g.online ? tr('reportsPage.csv_yes') : tr('reportsPage.csv_no'),
                                            g.tls_enabled ? tr('reportsPage.csv_yes') : tr('reportsPage.csv_no'),
                                            g.mqtt_auth ? tr('reportsPage.csv_yes') : tr('reportsPage.csv_no'),
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
                                        showApiError(e, tr('reportsPage.export_failed'));
                                    }
                                }}
                            >
                                <Download size={16} /> {tr('reportsPage.download_csv')}
                            </Button>
                        </div>

                        <div className="rounded-md border bg-card p-4 space-y-3">
                            <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                                <ShieldCheck size={16} /> {tr('reportsPage.score_title')}
                            </div>
                            <p className="text-sm">{tr('reportsPage.score_desc')}</p>
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
                                        showApiError(e, tr('reportsPage.export_failed'));
                                    }
                                }}
                            >
                                <Download size={16} /> {tr('reportsPage.download_json')}
                            </Button>
                        </div>
                    </div>
                </div>
            )}

            <div className="rounded-md border bg-card p-4">
                <div className="flex items-center justify-between mb-3">
                    <h3 className="font-semibold">{tr('reportsPage.tag_filter_title')}</h3>
                    {selectedTags.length > 0 && (
                        <Button variant="outline" size="sm" onClick={() => setSelectedTags([])}>{tr('reportsPage.clear')}</Button>
                    )}
                </div>
                <div className="max-h-72 overflow-auto border rounded-md divide-y">
                    {tags.length === 0 ? (
                        <p className="p-4 text-sm text-muted-foreground">{tr('reportsPage.no_tags')}</p>
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
