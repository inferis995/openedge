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
export interface KPIWidget {
    key: string;
    label: string;
    value: number;
    unit: string;
    trend: 'up' | 'down' | 'flat';
    delta_pct: number;
    good_when: 'up' | 'down';
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

export interface DashboardOverview {
    generated_at: string;
    system: SystemStatus;
    alarms: AlarmsBlock;
    gateways: GatewaysBlock;
    operations: OperationsBlock;
    activity: ActivityEvent[];
    kpi: KPIWidget[];
    shift?: ShiftBlock | null;
}

export const dashboardApi = {
    overview: async (): Promise<DashboardOverview> => {
        const r = await api.get('/dashboard/overview');
        return r.data;
    },
};
