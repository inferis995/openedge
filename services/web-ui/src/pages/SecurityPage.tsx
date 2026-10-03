import { useEffect, useState } from 'react';
import { Shield, AlertTriangle, CheckCircle2, XCircle, Lock, Key, Wifi, Server, Eye, FileText, Download, MinusCircle } from 'lucide-react';
import { securityApi, SecurityOverview, SecurityEvent, ComplianceCheck } from '@/api/security';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';

function timeAgo(dateStr: string): string {
    const now = new Date();
    const then = new Date(dateStr);
    const diffMs = now.getTime() - then.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    if (diffMins < 1) return i18n.t('securityPage.now');
    if (diffMins < 60) return i18n.t('securityPage.mins_ago', { n: diffMins });
    const diffHours = Math.floor(diffMins / 60);
    if (diffHours < 24) return i18n.t('securityPage.hours_ago', { n: diffHours });
    const diffDays = Math.floor(diffHours / 24);
    if (diffDays === 1) return i18n.t('securityPage.yesterday');
    return i18n.t('securityPage.days_ago', { n: diffDays });
}

function eventTypeLabel(type: string): string {
    const labels: Record<string, string> = {
        login_failed: i18n.t('securityPage.ev_login_failed'),
        account_locked: i18n.t('securityPage.ev_account_locked'),
        account_locked_attempt: i18n.t('securityPage.ev_account_locked_attempt'),
        password_changed: i18n.t('securityPage.ev_password_changed'),
        permission_denied: i18n.t('securityPage.ev_permission_denied'),
        config_changed: i18n.t('securityPage.ev_config_changed'),
        user_created: i18n.t('securityPage.ev_user_created'),
        user_deleted: i18n.t('securityPage.ev_user_deleted'),
    };
    return labels[type] ?? type;
}

function severityColor(severity: string): string {
    switch (severity) {
        case 'critical': return 'bg-red-600 text-white';
        case 'high': return 'bg-red-500 text-white';
        case 'medium': return 'bg-yellow-500 text-white';
        case 'low': return 'bg-blue-500 text-white';
        default: return 'bg-gray-500 text-white';
    }
}

function scoreColor(score: number): string {
    if (score >= 80) return 'text-green-600';
    if (score >= 60) return 'text-yellow-600';
    return 'text-red-600';
}

const breakdownLabels: Record<string, { label: string; icon: React.ReactNode }> = {
    audit_logging: { label: 'securityPage.bd_audit_logging', icon: <Eye className="h-4 w-4" /> },
    rbac_enabled: { label: 'securityPage.bd_rbac_enabled', icon: <Lock className="h-4 w-4" /> },
    backup_fresh: { label: 'securityPage.bd_backup_fresh', icon: <Server className="h-4 w-4" /> },
    rate_limiting: { label: 'securityPage.bd_rate_limiting', icon: <Shield className="h-4 w-4" /> },
    mfa_any_admin: { label: 'securityPage.bd_mfa_any_admin', icon: <Key className="h-4 w-4" /> },
    account_lockout_active: { label: 'securityPage.bd_account_lockout_active', icon: <Lock className="h-4 w-4" /> },
    strong_password_policy: { label: 'securityPage.bd_strong_password_policy', icon: <Key className="h-4 w-4" /> },
    mqtt_tls: { label: 'securityPage.bd_mqtt_tls', icon: <Wifi className="h-4 w-4" /> },
};

const SecurityPage = () => {
    const { t } = useTranslation();
    const [overview, setOverview] = useState<SecurityOverview | null>(null);
    const [events, setEvents] = useState<SecurityEvent[]>([]);
    const [compliance, setCompliance] = useState<ComplianceCheck[]>([]);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        Promise.all([
            securityApi.overview(),
            securityApi.events(20),
            securityApi.compliance(),
        ]).then(([o, e, c]) => {
            setOverview(o);
            setEvents(e);
            setCompliance(c);
        }).catch((err: unknown) => {
            console.error('Failed to load security data', err);
        }).finally(() => setLoading(false));
    }, []);

    const downloadPostureReport = () => {
        const report = {
            type: 'SECURITY_POSTURE_SELF_ASSESSMENT',
            // Chi riceve questo file lo leggerà fuori contesto, magari mesi
            // dopo. La riga che segue è l'unica cosa che gli impedisce di
            // scambiarlo per un certificato di conformità.
            disclaimer: t('securityPage.disclaimer'),
            generated_at: new Date().toISOString(),
            checks_passed: overview?.checks_passed,
            checks_evaluated: overview?.checks_evaluated,
            checks_not_assessed: overview?.checks_not_assessed,
            checks: overview?.checks,
            security_score: overview?.score,
            failed_logins_24h: overview?.failed_logins_24h,
            security_events_24h: overview?.security_events_24h,
            // Solo ciò che serve a chi legge: cosa è stato controllato, com'è
            // andato e perché. Il riferimento normativo resta nell'API per
            // compatibilità ma non finisce in un file che qualcuno potrebbe
            // presentare come prova di conformità.
            controlli: compliance.map(c => ({ id: c.id, name: c.name, state: c.state, detail: c.detail })),
        };
        const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `security-posture-${new Date().toISOString().slice(0, 10)}.json`;
        a.click();
        URL.revokeObjectURL(url);
    };

    const downloadEventsCSV = () => {
        const headers = [t('securityPage.csv_id'), t('securityPage.csv_type'), t('securityPage.csv_severity'), t('securityPage.csv_actor'), t('securityPage.csv_resource'), t('securityPage.csv_date')];
        const rows = events.map(e => [
            String(e.id),
            eventTypeLabel(e.event_type),
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
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="text-muted-foreground">{t('common.loading')}</div>
            </div>
        );
    }

    const passedCount = compliance.filter(c => c.state === 'pass').length;
    // Il denominatore è ciò che è stato valutato, non il numero di righe. Sei
    // di queste voci sono misure organizzative: contarle nel totale faceva
    // sembrare incompleta una piattaforma che su quei punti non ha nulla da
    // dire, né mai potrà averne.
    const evaluatedCount = compliance.filter(c => c.state !== 'not_assessed').length;
    const notAssessedCount = compliance.length - evaluatedCount;

    return (
        <div className="p-6 space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex items-center gap-3">
                    <Shield className="h-10 sm:h-8 w-10 sm:w-8 text-primary" />
                    <div>
                        <h1 className="text-2xl font-bold">{t('securityPage.title')}</h1>
                        <p className="text-muted-foreground text-sm">{t('securityPage.subtitle')}</p>
                    </div>
                </div>
                <div className="flex gap-2">
                    <Button variant="outline" size="sm" onClick={downloadEventsCSV}>
                        <Download className="h-4 w-4 mr-2" />
                        {t('securityPage.export_csv')}
                    </Button>
                    <Button size="sm" onClick={downloadPostureReport}>
                        <FileText className="h-4 w-4 mr-2" />
                        {t('securityPage.posture_report')}
                    </Button>
                </div>
            </div>

            {/* Top stats */}
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <Card>
                    <CardContent className="pt-6 text-center">
                        <div className={cn('text-5xl font-bold', overview ? scoreColor(overview.score) : 'text-gray-400')}>
                            {overview?.score ?? '-'}
                        </div>
                        <div className="text-sm text-muted-foreground mt-1">{t('securityPage.score')}</div>
                    </CardContent>
                </Card>
                <Card>
                    <CardContent className="pt-6 text-center">
                        <div className="text-3xl font-bold text-red-600">{overview?.failed_logins_24h ?? 0}</div>
                        <div className="text-sm text-muted-foreground mt-1">{t('securityPage.failed_logins')}</div>
                    </CardContent>
                </Card>
                <Card>
                    <CardContent className="pt-6 text-center">
                        <div className="text-3xl font-bold text-orange-600">{overview?.locked_accounts ?? 0}</div>
                        <div className="text-sm text-muted-foreground mt-1">{t('securityPage.locked')}</div>
                    </CardContent>
                </Card>
                <Card>
                    <CardContent className="pt-6 text-center">
                        <div className="text-3xl font-bold text-blue-600">
                            {overview?.checks_passed ?? 0}/{overview?.checks_evaluated ?? 0}
                        </div>
                        <div className="text-sm text-muted-foreground mt-1">{t('securityPage.checks_passed')}</div>
                        {/* Il denominatore conta solo ciò che è stato davvero
                            guardato. Senza questa riga, sei controlli sparirebbero
                            dal totale senza che nessuno sappia che esistono. */}
                        <div className="text-xs text-muted-foreground mt-1">
                            {t('securityPage.not_assessed', { count: overview?.checks_not_assessed ?? 0 })}
                        </div>
                    </CardContent>
                </Card>
            </div>

            {/* Score breakdown */}
            <Card>
                <CardHeader>
                    <CardTitle className="text-base">{t('securityPage.score_detail')}</CardTitle>
                </CardHeader>
                <CardContent>
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                        {overview && Object.entries(overview.score_breakdown).map(([key, value]) => {
                            const meta = breakdownLabels[key];
                            return (
                                <div key={key} className={cn(
                                    'flex items-center gap-2 p-3 rounded-lg border',
                                    value ? 'border-green-200 bg-green-50' : 'border-red-200 bg-red-50'
                                )}>
                                    <span className={value ? 'text-green-600' : 'text-red-600'}>
                                        {meta?.icon}
                                    </span>
                                    <div className="flex-1 min-w-0">
                                        <div className="text-xs font-medium truncate">{meta ? t(meta.label) : key}</div>
                                        <div className={cn('text-xs', value ? 'text-green-600' : 'text-red-600')}>
                                            {value ? t('securityPage.active') : t('securityPage.missing')}
                                        </div>
                                    </div>
                                    {value
                                        ? <CheckCircle2 className="h-4 w-4 text-green-600 shrink-0" />
                                        : <XCircle className="h-4 w-4 text-red-600 shrink-0" />
                                    }
                                </div>
                            );
                        })}
                    </div>
                </CardContent>
            </Card>

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {/* Controlli di sicurezza — autovalutazione, non certificazione */}
                <Card>
                    <CardHeader>
                        <CardTitle className="text-base flex items-center justify-between">
                            <span>{t('securityPage.checks')}</span>
                            <Badge variant={evaluatedCount > 0 && passedCount === evaluatedCount ? 'default' : 'destructive'}>
                                {t('securityPage.passed_badge', { passed: passedCount, total: evaluatedCount })}
                            </Badge>
                        </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2 max-h-96 overflow-y-auto">
                        {compliance.map(check => (
                            <div key={check.id} className="flex items-start gap-2 py-1">
                                {check.state === 'pass'
                                    ? <CheckCircle2 className="h-4 w-4 text-green-600 mt-0.5 shrink-0" />
                                    : check.state === 'fail'
                                        ? <XCircle className="h-4 w-4 text-red-600 mt-0.5 shrink-0" />
                                        : <MinusCircle className="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
                                }
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2">
                                        <span className="text-sm font-medium">{check.name}</span>
                                    </div>
                                    <div className="text-xs text-muted-foreground">{check.detail}</div>
                                </div>
                            </div>
                        ))}
                        {notAssessedCount > 0 && (
                            <p className="text-xs text-muted-foreground pt-3 border-t">
                                {t('securityPage.not_assessed_note', { count: notAssessedCount })}
                            </p>
                        )}
                    </CardContent>
                </Card>

                {/* Security Events */}
                <Card>
                    <CardHeader>
                        <CardTitle className="text-base flex items-center gap-2">
                            <AlertTriangle className="h-4 w-4 text-yellow-600" />
                            {t('securityPage.events')}
                        </CardTitle>
                    </CardHeader>
                    <CardContent>
                        <div className="space-y-2 max-h-96 overflow-y-auto">
                            {events.length === 0 ? (
                                <div className="text-center text-muted-foreground text-sm py-8">
                                    {t('securityPage.no_events')}
                                </div>
                            ) : events.map((event, idx) => (
                                <div key={`${event.id}-${idx}`} className="flex items-start gap-2 py-1.5 border-b last:border-0">
                                    <Badge className={cn('text-xs shrink-0', severityColor(event.severity))}>
                                        {t(`securityPage.sev_${event.severity}`, { defaultValue: event.severity })}
                                    </Badge>
                                    <div className="flex-1 min-w-0">
                                        <div className="text-sm font-medium">{eventTypeLabel(event.event_type)}</div>
                                        {event.actor && (
                                            <div className="text-xs text-muted-foreground">
                                                {event.actor}{event.resource ? ` → ${event.resource}` : ''}
                                            </div>
                                        )}
                                    </div>
                                    <div className="text-xs text-muted-foreground shrink-0">
                                        {timeAgo(event.created_at)}
                                    </div>
                                </div>
                            ))}
                        </div>
                    </CardContent>
                </Card>
            </div>
        </div>
    );
};

export default SecurityPage;
