import { useEffect, useState } from 'react';
import { Shield, AlertTriangle, CheckCircle2, XCircle, Lock, Key, Wifi, Server, Eye, FileText, Download, MinusCircle } from 'lucide-react';
import { securityApi, SecurityOverview, SecurityEvent, ComplianceCheck } from '@/api/security';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

function timeAgo(dateStr: string): string {
    const now = new Date();
    const then = new Date(dateStr);
    const diffMs = now.getTime() - then.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    if (diffMins < 1) return 'adesso';
    if (diffMins < 60) return `${diffMins} min fa`;
    const diffHours = Math.floor(diffMins / 60);
    if (diffHours < 24) return `${diffHours}h fa`;
    const diffDays = Math.floor(diffHours / 24);
    if (diffDays === 1) return 'ieri';
    return `${diffDays} giorni fa`;
}

function eventTypeLabel(type: string): string {
    const labels: Record<string, string> = {
        login_failed: 'Login fallito',
        account_locked: 'Account bloccato',
        account_locked_attempt: 'Tentativo su account bloccato',
        password_changed: 'Password modificata',
        permission_denied: 'Accesso negato',
        config_changed: 'Config modificata',
        user_created: 'Utente creato',
        user_deleted: 'Utente eliminato',
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
    audit_logging: { label: 'Audit Log', icon: <Eye className="h-4 w-4" /> },
    rbac_enabled: { label: 'RBAC', icon: <Lock className="h-4 w-4" /> },
    backup_fresh: { label: 'Backup recente', icon: <Server className="h-4 w-4" /> },
    rate_limiting: { label: 'Rate Limiting', icon: <Shield className="h-4 w-4" /> },
    mfa_any_admin: { label: 'MFA Admin', icon: <Key className="h-4 w-4" /> },
    account_lockout_active: { label: 'Lockout Account', icon: <Lock className="h-4 w-4" /> },
    strong_password_policy: { label: 'Password Policy', icon: <Key className="h-4 w-4" /> },
    mqtt_tls: { label: 'MQTT TLS', icon: <Wifi className="h-4 w-4" /> },
};

const SecurityPage = () => {
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
            disclaimer:
                'Autovalutazione automatica della postura di sicurezza, modellata sulle ' +
                'misure dell\'art. 21 della direttiva NIS2. NON costituisce una ' +
                'dichiarazione né una certificazione di conformità NIS2: i controlli con ' +
                'stato "not_assessed" riguardano misure organizzative che il software non ' +
                'può accertare e restano in capo al titolare dell\'impianto.',
            generated_at: new Date().toISOString(),
            checks_passed: overview?.checks_passed,
            checks_evaluated: overview?.checks_evaluated,
            checks_not_assessed: overview?.checks_not_assessed,
            checks: overview?.checks,
            security_score: overview?.score,
            failed_logins_24h: overview?.failed_logins_24h,
            security_events_24h: overview?.security_events_24h,
            compliance_checks: compliance,
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
        const headers = ['ID', 'Tipo', 'Gravità', 'Attore', 'Risorsa', 'Data'];
        const rows = events.map(e => [
            String(e.id),
            eventTypeLabel(e.event_type),
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
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="text-muted-foreground">Caricamento...</div>
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
                        <h1 className="text-2xl font-bold">Security Center</h1>
                        <p className="text-muted-foreground text-sm">Monitoraggio della postura di sicurezza</p>
                    </div>
                </div>
                <div className="flex gap-2">
                    <Button variant="outline" size="sm" onClick={downloadEventsCSV}>
                        <Download className="h-4 w-4 mr-2" />
                        Esporta eventi CSV
                    </Button>
                    <Button size="sm" onClick={downloadPostureReport}>
                        <FileText className="h-4 w-4 mr-2" />
                        Report postura
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
                        <div className="text-sm text-muted-foreground mt-1">Security Score</div>
                    </CardContent>
                </Card>
                <Card>
                    <CardContent className="pt-6 text-center">
                        <div className="text-3xl font-bold text-red-600">{overview?.failed_logins_24h ?? 0}</div>
                        <div className="text-sm text-muted-foreground mt-1">Login falliti (24h)</div>
                    </CardContent>
                </Card>
                <Card>
                    <CardContent className="pt-6 text-center">
                        <div className="text-3xl font-bold text-orange-600">{overview?.locked_accounts ?? 0}</div>
                        <div className="text-sm text-muted-foreground mt-1">Account bloccati</div>
                    </CardContent>
                </Card>
                <Card>
                    <CardContent className="pt-6 text-center">
                        <div className="text-3xl font-bold text-blue-600">
                            {overview?.checks_passed ?? 0}/{overview?.checks_evaluated ?? 0}
                        </div>
                        <div className="text-sm text-muted-foreground mt-1">Controlli automatici superati</div>
                        {/* Il denominatore conta solo ciò che è stato davvero
                            guardato. Senza questa riga, sei controlli sparirebbero
                            dal totale senza che nessuno sappia che esistono. */}
                        <div className="text-xs text-muted-foreground mt-1">
                            {overview?.checks_not_assessed ?? 0} non valutabili automaticamente
                        </div>
                    </CardContent>
                </Card>
            </div>

            {/* Score breakdown */}
            <Card>
                <CardHeader>
                    <CardTitle className="text-base">Dettaglio Security Score</CardTitle>
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
                                        <div className="text-xs font-medium truncate">{meta?.label ?? key}</div>
                                        <div className={cn('text-xs', value ? 'text-green-600' : 'text-red-600')}>
                                            {value ? 'Attivo' : 'Mancante'}
                                        </div>
                                    </div>
                                    {value
                                        ? <CheckCircle2 className="h-4 w-4 text-green-600 flex-shrink-0" />
                                        : <XCircle className="h-4 w-4 text-red-600 flex-shrink-0" />
                                    }
                                </div>
                            );
                        })}
                    </div>
                </CardContent>
            </Card>

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {/* Postura di sicurezza — autovalutazione, non conformità */}
                <Card>
                    <CardHeader>
                        <CardTitle className="text-base flex items-center justify-between">
                            <span>Postura di sicurezza — misure art. 21 NIS2</span>
                            <Badge variant={evaluatedCount > 0 && passedCount === evaluatedCount ? 'default' : 'destructive'}>
                                {passedCount}/{evaluatedCount} superati
                            </Badge>
                        </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2 max-h-96 overflow-y-auto">
                        {compliance.map(check => (
                            <div key={check.id} className="flex items-start gap-2 py-1">
                                {check.state === 'pass'
                                    ? <CheckCircle2 className="h-4 w-4 text-green-600 mt-0.5 flex-shrink-0" />
                                    : check.state === 'fail'
                                        ? <XCircle className="h-4 w-4 text-red-600 mt-0.5 flex-shrink-0" />
                                        : <MinusCircle className="h-4 w-4 text-muted-foreground mt-0.5 flex-shrink-0" />
                                }
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2">
                                        <span className="text-sm font-medium">{check.name}</span>
                                        <span className="text-xs text-muted-foreground">{check.article}</span>
                                    </div>
                                    <div className="text-xs text-muted-foreground">{check.detail}</div>
                                </div>
                            </div>
                        ))}
                        {notAssessedCount > 0 && (
                            <p className="text-xs text-muted-foreground pt-3 border-t">
                                {notAssessedCount} misure sono organizzative e non accertabili dal
                                software: restano in capo al titolare dell'impianto. Questa schermata
                                è un'autovalutazione, non una dichiarazione di conformità NIS2.
                            </p>
                        )}
                    </CardContent>
                </Card>

                {/* Security Events */}
                <Card>
                    <CardHeader>
                        <CardTitle className="text-base flex items-center gap-2">
                            <AlertTriangle className="h-4 w-4 text-yellow-600" />
                            Eventi di sicurezza recenti
                        </CardTitle>
                    </CardHeader>
                    <CardContent>
                        <div className="space-y-2 max-h-96 overflow-y-auto">
                            {events.length === 0 ? (
                                <div className="text-center text-muted-foreground text-sm py-8">
                                    Nessun evento recente
                                </div>
                            ) : events.map((event, idx) => (
                                <div key={`${event.id}-${idx}`} className="flex items-start gap-2 py-1.5 border-b last:border-0">
                                    <Badge className={cn('text-xs flex-shrink-0', severityColor(event.severity))}>
                                        {event.severity}
                                    </Badge>
                                    <div className="flex-1 min-w-0">
                                        <div className="text-sm font-medium">{eventTypeLabel(event.event_type)}</div>
                                        {event.actor && (
                                            <div className="text-xs text-muted-foreground">
                                                {event.actor}{event.resource ? ` → ${event.resource}` : ''}
                                            </div>
                                        )}
                                    </div>
                                    <div className="text-xs text-muted-foreground flex-shrink-0">
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
