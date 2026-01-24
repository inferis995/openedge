export interface Organization {
    id: number;
    name: string;
    created_at: string;
}

export interface Site {
    id: number;
    org_id: number;
    name: string;
    created_at: string;
}

export interface Area {
    id: number;
    site_id: number;
    name: string;
    created_at: string;
}

export interface Gateway {
    id: number;
    area_id: number;
    name: string;
    driver_type: 'S7' | 'MODBUS_TCP';
    ip_address: string;
    rack?: number;
    slot?: number;
    port?: number;
    slave_id?: number;
    scan_rate_ms: number;
    enabled: boolean;
    status?: string; // "online" | "offline"
    last_seen?: number;
    created_at?: string;
}

export interface Tag {
    id: number;
    gateway_id: number;
    code: string; // PLC Address
    alias?: string;
    data_type: 'BOOL' | 'INT' | 'REAL' | 'DINT' | 'STRING';
    scan_rate_ms?: number;
    historize: boolean;
    historize_interval_ms?: number;
    deadband_mode?: 'absolute' | 'percent';
    deadband_value?: number;
    alarm_enabled: boolean;
    alarm_threshold?: number;
    alarm_operator?: '>' | '<' | '>=' | '<=' | '=' | '!=';
    alarm_priority?: number;
    created_at?: string;
}

export interface Alarm {
    id: number;
    tag_id: number;
    state: 'active' | 'rtn' | 'acknowledged' | 'cleared';
    message: string;
    value_at_trigger?: number;
    triggered_at: string;
    acknowledged_at?: string;
    cleared_at?: string;
    tag_alias?: string;
    gateway_name?: string;
}

// DTOs
export interface CreateOrganizationDto {
    name: string;
}

export interface CreateSiteDto {
    org_id: number;
    name: string;
}

export interface CreateAreaDto {
    site_id: number;
    name: string;
    org_id?: number; // Helper for passing context to API layer
}

export interface CreateGatewayDto {
    area_id: number;
    name: string;
    driver_type: 'S7' | 'MODBUS_TCP';
    ip_address: string;
    rack?: number;
    slot?: number;
    port?: number;
    slave_id?: number;
    scan_rate_ms: number;
    enabled: boolean;
    org_id?: number; // Helper for passing context to API layer
}

export interface CreateTagDto {
    gateway_id: number;
    code: string;
    alias?: string;
    data_type: 'BOOL' | 'INT' | 'REAL' | 'DINT' | 'STRING';
    historize: boolean;
    deadband_value?: number;
    alarm_enabled: boolean;
    alarm_threshold?: number;
    alarm_operator?: '>' | '<' | '>=' | '<=' | '=' | '!=' | string; // Relaxed type for now
    alarm_priority?: number;
}

export interface HistoryDataPoint {
    timestamp: number;
    value: number;
    quality: number;
}

export interface HistoryQueryParams {
    tag_id: number;
    start: string;
    end: string;
    agg?: 'mean' | 'max' | 'min' | 'sum' | 'first' | 'last' | 'count' | 'median' | 'stddev';
    interval?: string;
}
