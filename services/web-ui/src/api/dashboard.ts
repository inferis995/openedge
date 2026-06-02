import api from './client';

// Shape ritornata da GET /api/dashboard/overview — vedi
// internal/handlers/dashboard.go per il sorgente di verità.

export interface SystemStatus {
    ready: boolean;
    db_ok: boolean;
    api_uptime_sec: number;
}

export interface AlarmSummary {
    id: number;
    severity: string;
    status: string;
    message: string;
    trigger_time: string;
    tag_alias: string;
    gateway_name: string;
}

export interface TrendBucket {
    bucket: string;
    count: number;
}

export interface AlarmsBlock {
    active_by_level: Record<string, number>;
    active_total: number;
    last_24h_fired: number;
    trend_7d: TrendBucket[];
    recent_top5: AlarmSummary[];
}

export interface GatewayBrief {
    id: number;
    name: string;
    driver_type: string;
}

export interface GatewaysBlock {
    online: number;
    offline: number;
    unknown: number;
    offline_list: GatewayBrief[];
}

export interface OperationsBlock {
    notif_email_enabled: boolean;
    notif_telegram_enabled: boolean;
    notif_min_severity: string;
    recipe_loads_24h: number;
    writes_24h: number;
    logins_24h: number;
}

export interface ActivityEvent {
    type: 'alarm' | 'recipe' | 'write' | 'login';
    timestamp: string;
    title: string;
    details: string;
    severity?: string;
}

// Un KPI è "big number + trend" — value e unit per la cella, trend per la
// freccia, delta_pct per la badge laterale, good_when per il colore (la
// stessa "up" è verde su una metrica positiva e rossa su una negativa).
// target + target_met sono opzionali (settable da System → KPI Targets) e
// guidano il colore del valore principale.
export interface KPIWidget {
    key: string;
    label: string;
    value: number;
    unit: string;
    trend: 'up' | 'down' | 'flat';
    delta_pct: number;
    good_when: 'up' | 'down';
    target?: number;
    target_met?: boolean;
}

export interface MaintenanceBlock {
    id: number;
    title: string;
    start_at: string;
    ends_at: string;
    reason?: string;
}

export interface ShiftBlock {
    shift_id: number;
    name: string;
    started_at: string;
    ends_at: string;
    time_left_min: number;
    operators: string[];
    alarms_this_shift: number;
}

// OEESnapshot è la "card lampante" della dashboard — Availability ×
// Performance × Quality, ognuno con la propria source ("tag" = calcolato
// da tag configurati; "fallback" = euristica out-of-the-box).
export interface OEESnapshot {
    oee: number;
    availability: number;
    performance: number;
    quality: number;
    availability_source: 'tag' | 'fallback';
    performance_source: 'tag' | 'fallback';
    quality_source: 'tag' | 'fallback';
    window_minutes: number;
    target?: number;
    critical_downtime_min: number;
    pieces_produced?: number;
    pieces_good?: number;
    target_pieces_per_hour?: number;
}

export interface OEEHistoryPoint {
    bucket: string;
    oee: number;
}

// Risultato di "lo provi prima di salvare" per il wizard OEE.
export interface OEETagTestResult {
    tag_id: number;
    alias: string;
    code: string;
    data_type: string;
    samples_count: number;
    current_value: number;
    first_value: number;
    last_value: number;
    min_value: number;
    max_value: number;
    delta: number;
    is_monotonic: boolean;
    is_boolish: boolean;
    warnings: string[];
    ok: boolean;
}

export interface DashboardOverview {
    generated_at: string;
    system: SystemStatus;
    alarms: AlarmsBlock;
    gateways: GatewaysBlock;
    operations: OperationsBlock;
    activity: ActivityEvent[];
    kpi: KPIWidget[];
    shift?: ShiftBlock | null;
    maintenance?: MaintenanceBlock | null;
    oee?: OEESnapshot | null;
}

export const dashboardApi = {
    overview: async (): Promise<DashboardOverview> => {
        const r = await api.get('/dashboard/overview');
        return r.data;
    },
};

// Endpoint OEE dedicato — usato dalla sparkline "ultimi 7 giorni" sotto
// la card grande. Quello dentro overview è solo lo snapshot corrente.
export const oeeApi = {
    snapshot: async (): Promise<OEESnapshot> => {
        const r = await api.get('/oee');
        return r.data;
    },
    history: async (): Promise<OEEHistoryPoint[]> => {
        const r = await api.get('/oee/history');
        return r.data;
    },
    testTag: async (tagId: number, role: 'running' | 'counter'): Promise<OEETagTestResult> => {
        const r = await api.get(`/oee/test-tag/${tagId}`, { params: { role } });
        return r.data;
    },
};
