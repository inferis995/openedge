import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import {
    Activity, AlertTriangle, Bell, BellOff, ChefHat, CheckCircle2, Clock, Cpu,
    Database, FileText, Gauge, LogIn, Mail, MessageCircle, Pencil, Radio, ShieldAlert,
    TrendingDown, TrendingUp, UserCircle, Users, Wifi, Wrench, XCircle, ArrowUpRight,
} from 'lucide-react';

import {
    dashboardApi, oeeApi,
    ActivityEvent, AlarmSummary, KPIWidget,
    OEESnapshot, OEEHistoryPoint, OEEOverview, OEEProfileSnapshot,
} from '@/api/dashboard';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';

// Refresh ogni 30s — bilancia "dati freschi" col carico DB. Sparkplug WS
// (già in piedi globalmente via useSparkplugListener) aggiorna i tag tra
// un refresh e l'altro per chi guarda valori specifici (Trend page).
const REFRESH_MS = 30_000;

// Mappa KPI key → pagina di drill-down. Click sul valore porta dritto
// all'elenco filtrato. KPI senza pagina di destinazione (es. logins_24h)
// restano non-cliccabili — meglio non promettere navigazione che non c'è.
const KPI_DESTINATION: Record<string, string | undefined> = {
    alarms_per_day: '/alarms',
    open_critical: '/alarms',
    writes_24h: undefined,        // niente pagina audit dedicata
    recipe_loads_24h: '/recipes',
    logins_24h: undefined,
    bad_quality_1h: '/tags',
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

const formatRelative = (iso: string): string => {
    const diffSec = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
    if (diffSec < 60) return `${diffSec}s fa`;
    if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m fa`;
    if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h fa`;
    return `${Math.floor(diffSec / 86400)}g fa`;
};

const severityColor = (sev: string): string => {
    switch (sev.toLowerCase()) {
        case 'critical': return 'text-red-500 bg-red-500/10 border-red-500/30';
        case 'high':     return 'text-orange-500 bg-orange-500/10 border-orange-500/30';
        case 'medium':   return 'text-amber-500 bg-amber-500/10 border-amber-500/30';
        case 'low':      return 'text-slate-400 bg-slate-500/10 border-slate-500/30';
        default:         return 'text-slate-400 bg-slate-500/10 border-slate-500/30';
    }
};

// ─────────────────────────────────────────────────────────────────────────────
// Clickable wrapper — applica hover state + cursor pointer quando c'è una
// destinazione di drill-down. Centralizzato qui per coerenza visiva.
// ─────────────────────────────────────────────────────────────────────────────
const Clickable = ({
    to, children, className = '',
}: { to?: string; children: React.ReactNode; className?: string }) => {
    const navigate = useNavigate();
    if (!to) return <div className={className}>{children}</div>;
    return (
        <button
            type="button"
            onClick={() => navigate(to)}
            className={`text-left w-full hover:bg-muted/40 hover:shadow-md transition-all cursor-pointer rounded-md ${className}`}
        >
            {children}
        </button>
    );
};

// ─────────────────────────────────────────────────────────────────────────────
// Status bar — riga compatta sempre in alto, alto contrasto per la lettura
// a distanza dal monitor industriale.
// ─────────────────────────────────────────────────────────────────────────────
const StatusBar = ({ data }: { data: NonNullable<ReturnType<typeof useDashboard>['data']> }) => {
    const navigate = useNavigate();
    const sysOk = data.system.ready && data.system.db_ok;
    const totalGw = data.gateways.online + data.gateways.offline + data.gateways.unknown;
    const offlineGw = data.gateways.offline;
    const crit = data.alarms.active_by_level.critical ?? 0;
    const high = data.alarms.active_by_level.high ?? 0;

    // Pill cliccabile — porta alla pagina di drill-down corrispondente.
    const pill = (ok: boolean, label: string, to?: string) => (
        <button
            type="button"
            onClick={() => to && navigate(to)}
            disabled={!to}
            className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border transition-all ${
                to ? 'hover:shadow cursor-pointer' : 'cursor-default'
            } ${
                ok ? 'border-emerald-500/30 text-emerald-500 bg-emerald-500/5'
                   : 'border-red-500/30 text-red-500 bg-red-500/5'
            }`}
        >
            <span className={`w-1.5 h-1.5 rounded-full ${ok ? 'bg-emerald-500' : 'bg-red-500 animate-pulse'}`} />
            {label}
        </button>
    );

    const alarmsToneClass = crit > 0
        ? 'border-red-500/30 text-red-500 bg-red-500/5'
        : high > 0
        ? 'border-orange-500/30 text-orange-500 bg-orange-500/5'
        : 'border-emerald-500/30 text-emerald-500 bg-emerald-500/5';

    return (
        <div className="flex flex-wrap items-center gap-3 px-4 py-3 rounded-md border bg-card">
            {pill(sysOk, sysOk ? 'Sistema OK' : 'Sistema in errore', '/diagnostics')}
            <button
                type="button"
                onClick={() => navigate('/diagnostics')}
                className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border border-border bg-background hover:shadow cursor-pointer transition-all">
                <Activity size={12} /> Uptime {formatDuration(data.system.api_uptime_sec)}
            </button>
            <button
                type="button"
                onClick={() => navigate('/alarms')}
                className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border hover:shadow cursor-pointer transition-all ${alarmsToneClass}`}>
                <Bell size={12} />
                {crit > 0 ? `${crit} critical` : high > 0 ? `${high} high` : 'Nessun allarme attivo'}
            </button>
            <button
                type="button"
                onClick={() => navigate('/gateways')}
                className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border hover:shadow cursor-pointer transition-all ${
                    offlineGw > 0 ? 'border-red-500/30 text-red-500 bg-red-500/5'
                                  : 'border-border text-foreground bg-background'
                }`}>
                <Wifi size={12} /> {totalGw > 0 ? `${data.gateways.online}/${totalGw} gateway online` : 'Nessun gateway'}
            </button>
            <span className="ml-auto text-xs text-muted-foreground">
                Aggiornato {formatRelative(data.generated_at)}
            </span>
        </div>
    );
};

// ─────────────────────────────────────────────────────────────────────────────
// KPI card — il widget "professionale intuitivo": numero grande, etichetta,
// freccia colorata + delta % vs periodo precedente. Stesso layout per tutti i
// KPI così l'occhio si abitua a leggere in 1s.
// ─────────────────────────────────────────────────────────────────────────────
const KPICard = ({ k }: { k: KPIWidget }) => {
    const isImprovement =
        (k.trend === 'up'   && k.good_when === 'up')   ||
        (k.trend === 'down' && k.good_when === 'down');
    const isWorsening =
        (k.trend === 'up'   && k.good_when === 'down') ||
        (k.trend === 'down' && k.good_when === 'up');

    const trendColor = isImprovement ? 'text-emerald-500' : isWorsening ? 'text-red-500' : 'text-muted-foreground';
    const Arrow = k.trend === 'up' ? TrendingUp : k.trend === 'down' ? TrendingDown : null;

    const valueStr = Number.isInteger(k.value) ? k.value.toString() : k.value.toFixed(2);
    const destination = KPI_DESTINATION[k.key];

    // Colore del valore principale: target_met (se definito) prende
    // precedenza sul trend. Se nessun target è configurato, il valore
    // resta neutro (foreground) e si lascia parlare solo la freccia trend.
    const valueColor = k.target_met === true
        ? 'text-emerald-500'
        : k.target_met === false
        ? 'text-red-500'
        : 'text-foreground';

    // Label del target: ≤ N (good_when=down) / ≥ N (good_when=up).
    const targetLabel = k.target !== undefined
        ? `${k.good_when === 'down' ? '≤' : '≥'} ${k.target}${k.unit}`
        : null;

    return (
        <Clickable to={destination}>
            <Card className="border-border h-full">
                <CardContent className="p-4 space-y-2">
                    <div className="flex items-center justify-between">
                        <p className="text-xs uppercase tracking-wider text-muted-foreground font-semibold">{k.label}</p>
                        {destination && <ArrowUpRight size={12} className="text-muted-foreground opacity-60" />}
                    </div>
                    <div className="flex items-baseline gap-2">
                        <span className={`text-3xl font-bold tracking-tight ${valueColor}`}>{valueStr}</span>
                        {k.unit && <span className="text-sm text-muted-foreground">{k.unit}</span>}
                    </div>
                    <div className="flex items-center justify-between text-xs">
                        {Arrow ? (
                            <div className={`flex items-center gap-1 ${trendColor}`}>
                                <Arrow size={12} />
                                <span>{Math.abs(k.delta_pct).toFixed(0)}%</span>
                            </div>
                        ) : <span className="text-muted-foreground">—</span>}
                        {targetLabel && (
                            <span className={`font-mono ${k.target_met ? 'text-emerald-500' : 'text-red-500'}`}>
                                Target {targetLabel}
                            </span>
                        )}
                    </div>
                </CardContent>
            </Card>
        </Clickable>
    );
};

// ─────────────────────────────────────────────────────────────────────────────
// Sparkline allarmi 7gg — SVG manuale (no library). Pochi punti, basta.
// ─────────────────────────────────────────────────────────────────────────────
const AlarmsSparkline = ({ data }: { data: { count: number }[] }) => {
    if (!data.length) return null;
    const max = Math.max(1, ...data.map((d) => d.count));
    const w = 140, h = 36, step = w / (data.length - 1 || 1);
    const points = data.map((d, i) => {
        const x = i * step;
        const y = h - (d.count / max) * (h - 4) - 2;
        return `${x},${y}`;
    }).join(' ');
    return (
        <svg viewBox={`0 0 ${w} ${h}`} className="w-full h-9">
            <polyline fill="none" stroke="currentColor" strokeWidth="1.5" points={points} className="text-primary" />
            <polyline fill="currentColor" opacity="0.1" points={`0,${h} ${points} ${w},${h}`} className="text-primary" />
        </svg>
    );
};

const AlarmsCard = ({ data }: { data: NonNullable<ReturnType<typeof useDashboard>['data']> }) => {
    const a = data.alarms;
    return (
        <Card>
            <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
                <CardTitle className="text-sm font-semibold text-muted-foreground flex items-center gap-2">
                    <ShieldAlert size={14} /> Allarmi attivi
                </CardTitle>
                <span className="text-xs text-muted-foreground">{a.last_24h_fired} fired 24h</span>
            </CardHeader>
            <CardContent className="space-y-3">
                <div className="grid grid-cols-4 gap-2">
                    {(['critical', 'high', 'medium', 'low'] as const).map((sev) => {
                        const n = a.active_by_level[sev] ?? 0;
                        return (
                            <div key={sev} className={`p-2 rounded-md border ${severityColor(sev)}`}>
                                <p className="text-2xl font-bold">{n}</p>
                                <p className="text-xs uppercase tracking-wider">{sev}</p>
                            </div>
                        );
                    })}
                </div>
                <div>
                    <p className="text-xs text-muted-foreground mb-1">Trend 7 giorni</p>
                    <AlarmsSparkline data={a.trend_7d} />
                </div>
            </CardContent>
        </Card>
    );
};

const OperationsCard = ({ data }: { data: NonNullable<ReturnType<typeof useDashboard>['data']> }) => {
    const o = data.operations;
    const row = (icon: React.ReactNode, label: string, value: React.ReactNode) => (
        <div className="flex items-center justify-between text-sm py-1.5">
            <span className="flex items-center gap-2 text-muted-foreground">{icon} {label}</span>
            <span>{value}</span>
        </div>
    );
    return (
        <Card>
            <CardHeader className="pb-2">
                <CardTitle className="text-sm font-semibold text-muted-foreground flex items-center gap-2">
                    <Activity size={14} /> Operatività (24h)
                </CardTitle>
            </CardHeader>
            <CardContent className="divide-y divide-border">
                {row(<ChefHat size={14} />, 'Ricette caricate', <span className="font-mono">{o.recipe_loads_24h}</span>)}
                {row(<Pencil size={14} />, 'Write PLC', <span className="font-mono">{o.writes_24h}</span>)}
                {row(<LogIn size={14} />, 'Login', <span className="font-mono">{o.logins_24h}</span>)}
                {row(<Mail size={14} />, 'Email alerts',
                    o.notif_email_enabled
                        ? <Badge className="bg-emerald-500/10 text-emerald-500 border-none text-xs">ON</Badge>
                        : <Badge className="bg-slate-500/10 text-slate-400 border-none text-xs">OFF</Badge>)}
                {row(<MessageCircle size={14} />, 'Telegram alerts',
                    o.notif_telegram_enabled
                        ? <Badge className="bg-emerald-500/10 text-emerald-500 border-none text-xs">ON</Badge>
                        : <Badge className="bg-slate-500/10 text-slate-400 border-none text-xs">OFF</Badge>)}
                {row(<BellOff size={14} />, 'Min severity', <code className="text-xs">{o.notif_min_severity}</code>)}
            </CardContent>
        </Card>
    );
};

// Mappa tipo evento → pagina di drill-down per i row della timeline.
const ACTIVITY_DESTINATION: Record<string, string | undefined> = {
    alarm: '/alarms',
    recipe: '/recipes',
    write: undefined,
    login: undefined,
};

const ActivityRow = ({ e }: { e: ActivityEvent }) => {
    const icon: Record<string, React.ReactNode> = {
        alarm:  <AlertTriangle size={14} className="text-orange-500" />,
        recipe: <ChefHat       size={14} className="text-purple-500" />,
        write:  <Pencil        size={14} className="text-blue-500" />,
        login:  <LogIn         size={14} className="text-slate-400" />,
    };
    return (
        <Clickable to={ACTIVITY_DESTINATION[e.type]} className="block">
            <div className="flex items-start gap-3 py-2 px-1 text-sm">
                <div className="mt-0.5">{icon[e.type] ?? <Radio size={14} />}</div>
                <div className="flex-1 min-w-0">
                    <p className="truncate">{e.title}</p>
                    {e.details && <p className="text-xs text-muted-foreground truncate">{e.details}</p>}
                </div>
                <span className="text-xs text-muted-foreground whitespace-nowrap">{formatRelative(e.timestamp)}</span>
            </div>
        </Clickable>
    );
};

const ActivityCard = ({ events }: { events: ActivityEvent[] }) => (
    <Card>
        <CardHeader className="pb-2">
            <CardTitle className="text-sm font-semibold text-muted-foreground flex items-center gap-2">
                <Activity size={14} /> Attività recenti
            </CardTitle>
        </CardHeader>
        <CardContent className="divide-y divide-border max-h-96 overflow-auto">
            {events.length === 0
                ? <p className="text-sm text-muted-foreground py-6 text-center">Nessuna attività recente.</p>
                : events.map((e, i) => <ActivityRow key={i} e={e} />)}
        </CardContent>
    </Card>
);

const RecentAlarmsCard = ({ alarms }: { alarms: AlarmSummary[] }) => {
    const navigate = useNavigate();
    return (
        <Card>
            <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
                <CardTitle className="text-sm font-semibold text-muted-foreground flex items-center gap-2">
                    <ShieldAlert size={14} /> Ultimi allarmi
                </CardTitle>
                <button onClick={() => navigate('/alarms')}
                    className="text-xs text-primary hover:underline flex items-center gap-1">
                    Tutti <ArrowUpRight size={12} />
                </button>
            </CardHeader>
            <CardContent className="divide-y divide-border">
                {alarms.length === 0
                    ? <p className="text-sm text-muted-foreground py-6 text-center">Nessun allarme recente.</p>
                    : alarms.map((a) => (
                        <Clickable to="/alarms" key={a.id}>
                            <div className="py-2 px-1 text-sm">
                                <div className="flex items-center gap-2">
                                    <Badge className={`text-xs ${severityColor(a.severity)} border-none`}>
                                        {a.severity}
                                    </Badge>
                                    <span className="truncate">{a.tag_alias || a.message || `Allarme #${a.id}`}</span>
                                    <span className="ml-auto text-xs text-muted-foreground">{formatRelative(a.trigger_time)}</span>
                                </div>
                                {a.gateway_name && <p className="text-xs text-muted-foreground mt-0.5 ml-1">{a.gateway_name}</p>}
                            </div>
                        </Clickable>
                    ))}
            </CardContent>
        </Card>
    );
};

// ─────────────────────────────────────────────────────────────────────────────
// Shift card — turno corrente con countdown e operatori in servizio.
// Card grande in alto perché è la prima cosa che l'operatore deve vedere
// quando apre la dashboard ("sono nel mio turno? quanto manca? chi è con me?").
// ─────────────────────────────────────────────────────────────────────────────
const ShiftCard = ({ data }: { data: NonNullable<ReturnType<typeof useDashboard>['data']> }) => {
    if (!data.shift) {
        return (
            <Card className="border-dashed">
                <CardHeader className="pb-2">
                    <CardTitle className="text-sm font-semibold text-muted-foreground flex items-center gap-2">
                        <Clock size={14} /> Turno corrente
                    </CardTitle>
                </CardHeader>
                <CardContent className="text-sm text-muted-foreground py-4 text-center">
                    Nessun turno in corso adesso.
                </CardContent>
            </Card>
        );
    }
    const s = data.shift;
    const totalMin = Math.max(1, Math.round((new Date(s.ends_at).getTime() - new Date(s.started_at).getTime()) / 60_000));
    const passedMin = Math.max(0, totalMin - s.time_left_min);
    const progress = Math.min(100, Math.round((passedMin / totalMin) * 100));
    const leftHours = Math.floor(s.time_left_min / 60);
    const leftMins = s.time_left_min % 60;
    const leftLabel = leftHours > 0 ? `${leftHours}h ${leftMins}m` : `${leftMins}m`;

    return (
        <Card>
            <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
                <CardTitle className="text-sm font-semibold text-muted-foreground flex items-center gap-2">
                    <Clock size={14} /> Turno corrente
                </CardTitle>
                <Badge className="bg-emerald-500/10 text-emerald-500 border-none">in corso</Badge>
            </CardHeader>
            <CardContent className="space-y-3">
                <div className="flex items-baseline justify-between">
                    <span className="text-xl font-bold tracking-tight">{s.name}</span>
                    <span className="text-xs text-muted-foreground font-mono">
                        {new Date(s.started_at).toISOString().slice(11, 16)}–{new Date(s.ends_at).toISOString().slice(11, 16)}
                    </span>
                </div>
                {/* progress bar */}
                <div className="space-y-1">
                    <div className="w-full h-2 bg-muted rounded">
                        <div className="h-2 rounded bg-primary" style={{ width: `${progress}%` }} />
                    </div>
                    <div className="flex justify-between text-xs text-muted-foreground">
                        <span>{progress}% completato</span>
                        <span>Restano {leftLabel}</span>
                    </div>
                </div>
                <div className="flex items-center gap-2 text-sm">
                    <Users size={14} className="text-muted-foreground" />
                    {s.operators.length === 0
                        ? <span className="text-muted-foreground">Nessun operatore designato</span>
                        : s.operators.map((u) => (
                            <span key={u} className="inline-flex items-center gap-1">
                                <UserCircle size={14} /> {u}
                            </span>
                        ))}
                </div>
                <div className="flex items-center justify-between text-xs pt-1 border-t">
                    <span className="text-muted-foreground">Allarmi durante questo turno</span>
                    <span className={`font-semibold ${s.alarms_this_shift > 0 ? 'text-orange-500' : 'text-emerald-500'}`}>
                        {s.alarms_this_shift}
                    </span>
                </div>
            </CardContent>
        </Card>
    );
};

const SystemCard = ({ data }: { data: NonNullable<ReturnType<typeof useDashboard>['data']> }) => {
    const s = data.system;
    const dot = (ok: boolean) => (
        <span className={`inline-block w-2 h-2 rounded-full ${ok ? 'bg-emerald-500' : 'bg-red-500'}`} />
    );
    return (
        <Card>
            <CardHeader className="pb-2">
                <CardTitle className="text-sm font-semibold text-muted-foreground flex items-center gap-2">
                    <Cpu size={14} /> Sistema
                </CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
                <div className="flex items-center justify-between">
                    <span className="flex items-center gap-2 text-muted-foreground">
                        {dot(s.db_ok)} <Database size={14} /> Postgres
                    </span>
                    <span className={s.db_ok ? 'text-emerald-500' : 'text-red-500'}>
                        {s.db_ok ? <CheckCircle2 size={16} /> : <XCircle size={16} />}
                    </span>
                </div>
                <div className="flex items-center justify-between">
                    <span className="flex items-center gap-2 text-muted-foreground">
                        {dot(s.ready)} <Activity size={14} /> Pronto
                    </span>
                    <span className={s.ready ? 'text-emerald-500' : 'text-red-500'}>{s.ready ? 'YES' : 'NO'}</span>
                </div>
                <div className="flex items-center justify-between">
                    <span className="flex items-center gap-2 text-muted-foreground">
                        <FileText size={14} /> Uptime API
                    </span>
                    <span className="font-mono text-xs">{formatDuration(s.api_uptime_sec)}</span>
                </div>
            </CardContent>
        </Card>
    );
};

// ─────────────────────────────────────────────────────────────────────────────
// OEE card — la "lampante" del cruscotto. ISO 22400 dice: 85%+ world-class,
// 60-85% accettabile, <60% problemi. Coloriamo l'OEE in base a queste fasce
// e mostriamo le tre componenti A/P/Q come barre con il proprio numerino.
// La sparkline 7g sotto la card è caricata via /api/oee/history (separato dal
// dashboard overview per non pesare ogni 30s su 7 calcoli OEE giornalieri).
// ─────────────────────────────────────────────────────────────────────────────
const oeeBand = (v: number): { tone: string; ring: string; label: string } => {
    if (v >= 85) return { tone: 'text-emerald-500', ring: 'border-emerald-500/40 bg-emerald-500/5', label: 'World-class' };
    if (v >= 65) return { tone: 'text-amber-500',   ring: 'border-amber-500/40 bg-amber-500/5',     label: 'Tipico' };
    if (v >= 40) return { tone: 'text-orange-500',  ring: 'border-orange-500/40 bg-orange-500/5',   label: 'Da migliorare' };
    return         { tone: 'text-red-500',     ring: 'border-red-500/40 bg-red-500/5',         label: 'Critico' };
};

const OEEHistorySpark = ({ data }: { data: OEEHistoryPoint[] }) => {
    if (!data.length) return null;
    const max = 100;
    const w = 280, h = 48, step = w / (data.length - 1 || 1);
    const points = data.map((d, i) => {
        const x = i * step;
        const y = h - (d.oee / max) * (h - 4) - 2;
        return `${x},${y}`;
    }).join(' ');
    return (
        <svg viewBox={`0 0 ${w} ${h}`} className="w-full h-12">
            {/* Riferimento 85% (world-class) */}
            <line x1="0" y1={h - 0.85 * (h - 4) - 2} x2={w} y2={h - 0.85 * (h - 4) - 2}
                  stroke="currentColor" strokeWidth="0.5" strokeDasharray="2,2" className="text-emerald-500/40" />
            <polyline fill="none" stroke="currentColor" strokeWidth="1.5" points={points} className="text-primary" />
            <polyline fill="currentColor" opacity="0.1" points={`0,${h} ${points} ${w},${h}`} className="text-primary" />
        </svg>
    );
};

const OEEComponentBar = ({
    label, value, source,
}: { label: string; value: number; source: 'tag' | 'fallback' }) => {
    const band = oeeBand(value);
    return (
        <div className="space-y-1">
            <div className="flex items-baseline justify-between text-xs">
                <span className="text-muted-foreground uppercase tracking-wider font-semibold">{label}</span>
                <div className="flex items-center gap-1.5">
                    <span className={`font-mono font-bold text-base ${band.tone}`}>{value.toFixed(1)}%</span>
                    <Badge variant="outline" className="text-[9px] font-normal h-4 px-1 leading-none">
                        {source === 'tag' ? 'tag' : 'auto'}
                    </Badge>
                </div>
            </div>
            <div className="w-full h-2 bg-muted rounded overflow-hidden">
                <div className={`h-2 rounded transition-all ${
                    value >= 85 ? 'bg-emerald-500'
                    : value >= 65 ? 'bg-amber-500'
                    : value >= 40 ? 'bg-orange-500'
                    : 'bg-red-500'
                }`} style={{ width: `${Math.min(100, Math.max(0, value))}%` }} />
            </div>
        </div>
    );
};

// OEEWrapper — decide tra legacy (1 card) e profiles (rollup + griglia).
const OEEWrapper = ({ overview }: { overview: OEEOverview }) => {
    if (overview.mode === 'legacy' && overview.legacy) {
        return <OEECard o={overview.legacy} title="OEE" subtitle="" showNudge />;
    }
    if (overview.mode === 'profiles' && overview.profiles && overview.profiles.length > 0) {
        return <OEEProfilesView profiles={overview.profiles} rollup={overview.rollup} />;
    }
    return null;
};

// Vista multi-profilo: rollup grande in cima + griglia di card compatte per
// ogni profilo. Click su una card → /oee/profili (drill-down futuro).
const OEEProfilesView = ({
    profiles, rollup,
}: { profiles: OEEProfileSnapshot[]; rollup?: OEESnapshot }) => {
    return (
        <div className="space-y-3">
            {rollup && (
                <OEECard
                    o={rollup}
                    title="OEE Fabbrica"
                    subtitle={`Media di ${profiles.length} ${profiles.length === 1 ? 'profilo' : 'profili'}`}
                />
            )}
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
                {profiles.map((p) => (
                    <OEEProfileCard key={p.profile_id} p={p} />
                ))}
            </div>
        </div>
    );
};

// Card compatta per singolo profilo. Mostra nome, area, OEE numerico,
// 3 mini-barre A/P/Q. Click → pagina di gestione profili.
const OEEProfileCard = ({ p }: { p: OEEProfileSnapshot }) => {
    const navigate = useNavigate();
    const s = p.snapshot;
    const band = oeeBand(s.oee);
    const targetMet = s.target !== undefined ? s.oee >= s.target : null;

    return (
        <button
            type="button"
            onClick={() => navigate('/oee-profiles')}
            className={`text-left rounded-md border-2 p-3 hover:shadow-md transition-all ${band.ring}`}
        >
            <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                    <p className="font-semibold truncate">{p.name}</p>
                    {p.area_name && (
                        <p className="text-[11px] text-muted-foreground truncate">{p.area_name}</p>
                    )}
                </div>
                <Gauge size={18} className={band.tone} />
            </div>
            <div className="mt-2 flex items-baseline gap-1">
                <span className={`text-3xl font-bold tracking-tight ${band.tone}`}>{s.oee.toFixed(1)}</span>
                <span className="text-base font-semibold text-muted-foreground">%</span>
                {s.target !== undefined && (
                    <span className={`ml-auto text-[10px] ${targetMet ? 'text-emerald-500' : 'text-red-500'}`}>
                        {targetMet ? '✓' : '✗'} ≥ {s.target}%
                    </span>
                )}
            </div>
            <div className="mt-2 space-y-1">
                <MiniBar label="A" v={s.availability} />
                <MiniBar label="P" v={s.performance} />
                <MiniBar label="Q" v={s.quality} />
            </div>
        </button>
    );
};

const MiniBar = ({ label, v }: { label: string; v: number }) => {
    const color =
        v >= 85 ? 'bg-emerald-500'
        : v >= 65 ? 'bg-amber-500'
        : v >= 40 ? 'bg-orange-500'
        : 'bg-red-500';
    return (
        <div className="flex items-center gap-2 text-[11px]">
            <span className="text-muted-foreground font-mono w-3">{label}</span>
            <div className="flex-1 h-1.5 bg-muted rounded overflow-hidden">
                <div className={`h-1.5 ${color}`} style={{ width: `${Math.min(100, Math.max(0, v))}%` }} />
            </div>
            <span className="font-mono w-10 text-right">{v.toFixed(0)}%</span>
        </div>
    );
};

const OEECard = ({
    o, title = 'OEE', subtitle, showNudge = false,
}: { o: OEESnapshot; title?: string; subtitle?: string; showNudge?: boolean }) => {
    const navigate = useNavigate();
    const band = oeeBand(o.oee);
    const windowH = (o.window_minutes / 60).toFixed(o.window_minutes % 60 === 0 ? 0 : 1);

    // History sparkline — query separata (più lenta del refresh dashboard).
    const { data: history } = useQuery({
        queryKey: ['oee-history'],
        queryFn: oeeApi.history,
        refetchInterval: 5 * 60_000,
        placeholderData: (prev) => prev,
    });

    const targetMet = o.target !== undefined ? o.oee >= o.target : null;
    const allFallback =
        o.availability_source === 'fallback' &&
        o.performance_source  === 'fallback' &&
        o.quality_source      === 'fallback';

    return (
        <Card className={`border-2 ${band.ring}`}>
            <CardContent className="p-5">
                <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-center">
                    {/* Sinistra: OEE grande + label fascia */}
                    <div className="lg:col-span-4 flex items-center gap-4">
                        <div className={`p-3 rounded-xl ${band.ring} border`}>
                            <Gauge size={36} className={band.tone} />
                        </div>
                        <div>
                            <div className="flex items-center gap-2 flex-wrap">
                                <span className="text-xs uppercase tracking-wider text-muted-foreground font-semibold">{title} {windowH}h</span>
                                <Badge variant="outline" className={`text-[10px] ${band.tone} border-current`}>
                                    {band.label}
                                </Badge>
                                {subtitle && (
                                    <span className="text-[10px] text-muted-foreground">{subtitle}</span>
                                )}
                            </div>
                            <div className="flex items-baseline gap-1 mt-1">
                                <span className={`text-5xl font-bold tracking-tight ${band.tone}`}>{o.oee.toFixed(1)}</span>
                                <span className="text-2xl font-semibold text-muted-foreground">%</span>
                            </div>
                            {o.target !== undefined && (
                                <p className={`text-xs mt-1 ${targetMet ? 'text-emerald-500' : 'text-red-500'}`}>
                                    Target ≥ {o.target}%  {targetMet ? '✓' : '✗'}
                                </p>
                            )}
                        </div>
                    </div>

                    {/* Centro: 3 barre A/P/Q */}
                    <div className="lg:col-span-5 space-y-3">
                        <OEEComponentBar label="Availability" value={o.availability} source={o.availability_source} />
                        <OEEComponentBar label="Performance"  value={o.performance}  source={o.performance_source} />
                        <OEEComponentBar label="Quality"      value={o.quality}      source={o.quality_source} />
                    </div>

                    {/* Destra: sparkline 7g + numeri di diagnostica */}
                    <div className="lg:col-span-3 space-y-2">
                        <div>
                            <p className="text-[10px] uppercase tracking-wider text-muted-foreground font-semibold mb-1">Trend 7g</p>
                            <OEEHistorySpark data={history ?? []} />
                        </div>
                        <div className="text-xs space-y-0.5 text-muted-foreground">
                            {o.critical_downtime_min > 0 && (
                                <div className="flex justify-between">
                                    <span>Downtime critical</span>
                                    <span className="font-mono">{o.critical_downtime_min.toFixed(0)} min</span>
                                </div>
                            )}
                            {o.pieces_produced !== undefined && o.pieces_produced > 0 && (
                                <div className="flex justify-between">
                                    <span>Pezzi prodotti</span>
                                    <span className="font-mono">{o.pieces_produced.toFixed(0)}</span>
                                </div>
                            )}
                            {o.pieces_good !== undefined && o.pieces_good > 0 && (
                                <div className="flex justify-between">
                                    <span>Pezzi buoni</span>
                                    <span className="font-mono">{o.pieces_good.toFixed(0)}</span>
                                </div>
                            )}
                        </div>
                    </div>
                </div>

                {/* Nudge configurazione — solo per OEE legacy (no profili) e
                    quando tutto è in fallback. */}
                {showNudge && allFallback && (
                    <button
                        type="button"
                        onClick={() => navigate('/oee-profiles')}
                        className="mt-4 w-full text-xs text-muted-foreground hover:text-foreground transition-colors text-left flex items-center gap-2 px-3 py-2 rounded border border-dashed border-border hover:border-primary/40">
                        <ArrowUpRight size={12} />
                        Calcolo OEE in modalità euristica. Crea i profili OEE in
                        <span className="text-primary underline ml-1">OEE Profili</span>
                        per monitorare ogni linea separatamente.
                    </button>
                )}
            </CardContent>
        </Card>
    );
};

const useDashboard = () => {
    return useQuery({
        queryKey: ['dashboard-overview'],
        queryFn: dashboardApi.overview,
        refetchInterval: REFRESH_MS,
        placeholderData: (prev) => prev,
    });
};

const DashboardPage = () => {
    const { data, isLoading, isError } = useDashboard();

    if (isLoading && !data) {
        return <div className="p-8 text-center text-muted-foreground">Caricamento dashboard...</div>;
    }
    if (isError || !data) {
        return <div className="p-8 text-center text-red-500">Errore caricamento dashboard.</div>;
    }

    return (
        <div className="space-y-6">
            <div>
                <h2 className="text-3xl font-bold tracking-tight">Dashboard</h2>
                <p className="text-muted-foreground">Stato del sistema in tempo reale.</p>
            </div>

            <StatusBar data={data} />

            {/* Banner manutenzione in corso — sopra ai KPI così è
                impossibile non vederla: spiega anche perché le notifiche
                potrebbero non arrivare. */}
            {data.maintenance && (
                <Clickable to="/maintenance">
                    <div className="rounded-md border border-amber-500/40 bg-amber-500/5 px-4 py-3 flex items-start gap-3">
                        <Wrench size={20} className="text-amber-500 mt-0.5" />
                        <div className="flex-1">
                            <p className="font-semibold text-amber-500">
                                Manutenzione in corso: {data.maintenance.title}
                            </p>
                            <p className="text-xs text-muted-foreground mt-0.5">
                                Le notifiche email/Telegram sono silenziate fino a{' '}
                                {new Date(data.maintenance.ends_at).toLocaleString('it-IT')}
                                {data.maintenance.reason && ` — ${data.maintenance.reason}`}
                            </p>
                        </div>
                    </div>
                </Clickable>
            )}

            {/* OEE — "lampante": prima cosa che l'operatore vede dopo lo
                status bar. Due modalità: legacy (1 card) o profili
                (rollup + griglia di card linea/reparto). */}
            {data.oee && <OEEWrapper overview={data.oee} />}

            <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
                {data.kpi.map((k) => <KPICard key={k.key} k={k} />)}
            </div>

            {/* Turno corrente — riga a sé sopra le 3 card metriche, perché è
                l'informazione più "operativa" che l'operatore vuole vedere subito. */}
            <ShiftCard data={data} />

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
                <AlarmsCard      data={data} />
                <OperationsCard  data={data} />
                <SystemCard      data={data} />
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                <RecentAlarmsCard alarms={data.alarms.recent_top5} />
                <ActivityCard     events={data.activity} />
            </div>
        </div>
    );
};

export default DashboardPage;
