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

// OEEProfileSnapshot = snapshot etichettato per un profilo (linea/reparto).
export interface OEEProfileSnapshot {
    profile_id: number;
    name: string;
    description?: string;
    area_id?: number;
    area_name?: string;
    enabled: boolean;
    snapshot: OEESnapshot;
}

// OEEOverview è la response top-level di /api/oee.
// - mode="legacy": un singolo snapshot (campo "legacy")
// - mode="profiles": lista di profili + rollup (campi "profiles" + "rollup")
export interface OEEOverview {
    mode: 'legacy' | 'profiles';
    profiles?: OEEProfileSnapshot[];
    rollup?: OEESnapshot;
    legacy?: OEESnapshot;
}

// Profilo OEE (modello DB) — usato dalla pagina admin.
export interface OEEProfile {
    id: number;
    org_id: number;
    name: string;
    description?: string;
    area_id?: number;
    area_name?: string;
    gateway_id?: number;
    run_time_tag_id?: number;
    produced_tag_id?: number;
    good_tag_id?: number;
    target_pieces_per_hour: number;
    window_minutes: number;
    target_oee: number;
    display_order: number;
    enabled: boolean;
    // Quando true (default), Availability divide per Planned Production
    // Time (window - pause turni - manutenzioni) invece di wall clock.
    respect_shifts: boolean;
    respect_maintenance: boolean;
}

export interface OEEProfileRequest {
    name: string;
    description?: string;
    area_id?: number | null;
    gateway_id?: number | null;
    run_time_tag_id?: number | null;
    produced_tag_id?: number | null;
    good_tag_id?: number | null;
    target_pieces_per_hour: number;
    window_minutes: number;
    target_oee: number;
    display_order: number;
    enabled: boolean;
    respect_shifts?: boolean;
    respect_maintenance?: boolean;
}

// Riga della matrice "OEE per turno × giorno".
export interface OEEShiftRow {
    profile_id?: number;
    shift_id: number;
    shift_name: string;
    date: string; // YYYY-MM-DD
    oee: number;
    availability: number;
    performance: number;
    quality: number;
    hours: number;
    bucket_start: string;
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
    oee?: OEEOverview | null;
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
    snapshot: async (): Promise<OEEOverview> => {
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
    // Profiles CRUD
    listProfiles: async (): Promise<OEEProfile[]> => {
        const r = await api.get('/oee/profiles');
        return r.data;
    },
    createProfile: async (data: OEEProfileRequest): Promise<OEEProfile> => {
        const r = await api.post('/oee/profiles', data);
        return r.data;
    },
    updateProfile: async (id: number, data: OEEProfileRequest): Promise<OEEProfile> => {
        const r = await api.put(`/oee/profiles/${id}`, data);
        return r.data;
    },
    deleteProfile: async (id: number): Promise<void> => {
        await api.delete(`/oee/profiles/${id}`);
    },
    exportCSV: async (type: 'history' | 'by-shift' | 'losses' | 'profiles', params: Record<string, string>): Promise<void> => {
        const qs = new URLSearchParams(params).toString();
        const url = `/oee/export/${type}.csv${qs ? '?' + qs : ''}`;
        const r = await api.get(url, { responseType: 'blob' });
        const blob = new Blob([r.data], { type: 'text/csv;charset=utf-8;' });
        const link = document.createElement('a');
        link.href = window.URL.createObjectURL(blob);
        link.download = `oee_${type}_${new Date().toISOString().split('T')[0]}.csv`;
        link.click();
        window.URL.revokeObjectURL(link.href);
    },
    profileHistory: async (id: number): Promise<OEEHistoryPoint[]> => {
        const r = await api.get(`/oee/profiles/${id}/history`);
        return r.data;
    },
    // Storico persistente (rollup orario/giornaliero salvato dal cron).
    // Per finestre lunghe (> 7g) usa questo, non profileHistory che
    // ricalcola al volo da tag_history.
    historyV2: async (params: {
        profile_id?: number | null;
        from: string;
        to: string;
        bucket: 'hour' | 'day' | 'shift';
    }): Promise<OEEHistoryRow[]> => {
        const r = await api.get('/oee/history-v2', { params });
        return r.data;
    },
    byShift: async (params: {
        profile_id?: number | null;
        from: string;
        to: string;
    }): Promise<OEEShiftRow[]> => {
        const r = await api.get('/oee/by-shift', { params });
        return r.data;
    },
    // Loss tree (Six Big Losses) + Pareto cause + MTBF/MTTR.
    lossTree: async (params: {
        profile_id: number;
        from: string;
        to: string;
    }): Promise<{ categories: OEELossCategoryAgg[]; total_minutes: number }> => {
        const r = await api.get('/oee/loss-tree', { params });
        return r.data;
    },
    reliability: async (id: number, from: string, to: string): Promise<OEEReliability> => {
        const r = await api.get(`/oee/profiles/${id}/reliability`, { params: { from, to } });
        return r.data;
    },
    // Alert rules CRUD
    listAlertRules: async (): Promise<OEEAlertRule[]> => {
        const r = await api.get('/oee/alert-rules');
        return r.data;
    },
    createAlertRule: async (data: OEEAlertRuleRequest): Promise<OEEAlertRule> => {
        const r = await api.post('/oee/alert-rules', data);
        return r.data;
    },
    updateAlertRule: async (id: number, data: OEEAlertRuleRequest): Promise<OEEAlertRule> => {
        const r = await api.put(`/oee/alert-rules/${id}`, data);
        return r.data;
    },
    deleteAlertRule: async (id: number): Promise<void> => {
        await api.delete(`/oee/alert-rules/${id}`);
    },
    // Gerarchia Site → Area → Profile con rollup multi-livello.
    hierarchy: async (): Promise<OEEHierarchyResponse> => {
        const r = await api.get('/oee/hierarchy');
        return r.data;
    },
};

export interface OEEHierarchyNode {
    node_type: 'site' | 'area' | 'profile';
    id: number;
    name: string;
    snapshot: OEESnapshot;
    children?: OEEHierarchyNode[];
}

export interface OEEHierarchyResponse {
    sites: OEEHierarchyNode[];
    unassigned: OEEHierarchyNode | null;
}

export interface OEEAlertRule {
    id: number;
    profile_id?: number | null;
    name: string;
    metric: 'oee' | 'availability' | 'performance' | 'quality';
    op: '<' | '>';
    threshold: number;
    sustained_minutes: number;
    severity: 'info' | 'warning' | 'critical';
    enabled: boolean;
    last_notified_at?: string;
    last_state: 'normal' | 'violating';
    created_at: string;
    updated_at: string;
}

export interface OEEAlertRuleRequest {
    profile_id?: number | null;
    name: string;
    metric: 'oee' | 'availability' | 'performance' | 'quality';
    op: '<' | '>';
    threshold: number;
    sustained_minutes: number;
    severity: 'info' | 'warning' | 'critical';
    enabled: boolean;
}

export interface OEELossCategoryAgg {
    category_id: number;
    code: string;
    pillar: 'availability' | 'performance' | 'quality';
    display_label: string;
    color: string;
    events_count: number;
    total_minutes: number;
    percent_of_total: number;
}

export interface OEEReliability {
    profile_id: number;
    from: string;
    to: string;
    breakdown_count: number;
    total_run_min: number;
    total_repair_min: number;
    mtbf_minutes: number;
    mtbf_hours: number;
    mttr_minutes: number;
}

// Riga del rollup persistito — più informazioni del semplice
// OEEHistoryPoint (include A/P/Q, pieces, shift).
export interface OEEHistoryRow {
    profile_id?: number;
    bucket_start: string;
    bucket_size: 'hour' | 'day' | 'shift';
    oee: number;
    availability: number;
    performance: number;
    quality: number;
    planned_min: number;
    downtime_min: number;
    pieces_produced: number;
    pieces_good: number;
    shift_id?: number;
}
