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
    driver_type: 'S7' | 'MODBUS_TCP' | 'MQTT' | 'OPC_UA' | 'LORAWAN';
    connection_config: any; // Nested config
    scan_rate_ms: number;
    enabled: boolean;
    status?: string; // deprecated, use connection_status
    connection_status?: string; // "online" | "offline" from API
    last_seen?: number;
    created_at?: string;
    zero_based?: boolean;
}

export interface Tag {
    id: number;
    gateway_id: number;
    code: string; // PLC Address
    alias?: string;
    data_type: 'BOOL' | 'INT' | 'REAL' | 'DINT' | 'STRING';
    scan_rate_ms?: number;
    historize: boolean;
    // For MQTT tags whose payload is JSON: dotted path to the field to extract
    // (e.g. "temp" or "data.temperature"). Empty/undefined = whole payload.
    json_path?: string | null;
    historize_interval_ms?: number;
    deadband_mode?: 'absolute' | 'percent';
    deadband_value?: number;
    // EU Scaling — raw-to-engineering-unit conversion applied at ingestion.
    scaling_enabled?: boolean;
    scaling_raw_min?: number;
    scaling_raw_max?: number;
    scaling_eu_min?: number;
    scaling_eu_max?: number;
    scaling_clamp?: boolean;
    eu_unit?: string;
    eu_decimals?: number;
    invert?: boolean;
    created_at?: string;
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
    driver_type: 'S7' | 'MODBUS_TCP' | 'MQTT' | 'OPC_UA' | 'LORAWAN';
    ip_address: string;
    rack?: number;
    slot?: number;
    port?: number;
    slave_id?: number;
    endpoint?: string; // OPC UA endpoint URL (e.g. opc.tcp://192.168.1.10:4840)
    auth_mode?: string;
    username?: string;
    password?: string;
    cert_file?: string;
    key_file?: string;
    scan_rate_ms: number;
    enabled: boolean;
    zero_based?: boolean; // For MODBUS_TCP: true = addresses start from 0, false = standard 1-based addressing
    org_id?: number; // Helper for passing context to API layer
}

export interface CreateTagDto {
    gateway_id: number;
    code: string;
    alias?: string;
    data_type: 'BOOL' | 'INT' | 'REAL' | 'DINT' | 'STRING';
    historize: boolean;
    deadband_value?: number;
    json_path?: string; // MQTT only — empty = whole payload
    // EU Scaling
    scaling_enabled?: boolean;
    scaling_raw_min?: number;
    scaling_raw_max?: number;
    scaling_eu_min?: number;
    scaling_eu_max?: number;
    scaling_clamp?: boolean;
    eu_unit?: string;
    eu_decimals?: number;
    invert?: boolean;
}

export interface OpcUaNode {
    node_id: string;
    name: string;
    display_name: string;
    node_class: string; // "Object" | "Variable" | "Method"
    data_type: string;  // "Int32", "Float", "Boolean", "Double", "String"
    children_count: number;
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

export interface WriteTagCommand {
    tag_id: number;
    value: any;
}

export interface WriteTagResult {
    success: boolean;
    error?: string;
    message?: string;
}
