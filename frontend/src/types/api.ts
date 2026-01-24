// API Types matching backend OpenAPI specification

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

export interface ConnectionConfig {
  [key: string]: any;
}

export interface Gateway {
  id: number;
  area_id: number;
  name: string;
  driver_type: 'S7' | 'MODBUS_TCP';
  connection_config: ConnectionConfig;
  scan_rate_ms: number;
  enabled: boolean;
  created_at: string;
}

export interface GatewayWithHealth extends Gateway {
  connection_status: 'online' | 'offline';
  last_seen?: number;
}

export interface Tag {
  id: number;
  gateway_id: number;
  code: string;
  alias: string;
  data_type: 'INT' | 'REAL' | 'BOOL' | 'DINT';
  historize: boolean;
  historize_deadband: number;
  alarm_enabled: boolean;
  alarm_threshold?: number;
  alarm_operator?: '>' | '<' | '=';
  alarm_priority: number;
  created_at: string;
}

export interface TagValue {
  v: number | boolean | string;
  ts: number;
  q: number;
}

export interface HistoryPoint {
  timestamp: number;
  value: number | boolean | string;
  quality: number;
}

export interface Alarm {
  id: number;
  tag_id: number;
  tag_alias?: string;
  tag_code?: string;
  state: 'ACTIVE' | 'RTN' | 'ACKNOWLEDGED' | 'CLEAR';
  message: string;
  triggered_at: string;
  acknowledged_at?: string;
  cleared_at?: string;
}

// Request/Response Types

export interface CreateOrganizationRequest {
  name: string;
}

export interface CreateSiteRequest {
  org_id: number;
  name: string;
}

export interface CreateAreaRequest {
  site_id: number;
  name: string;
}

export interface CreateGatewayRequest {
  area_id: number;
  name: string;
  driver_type: 'S7' | 'MODBUS_TCP';
  connection_config: ConnectionConfig;
  scan_rate_ms?: number;
  enabled?: boolean;
}

export interface UpdateGatewayRequest {
  name?: string;
  connection_config?: ConnectionConfig;
  scan_rate_ms?: number;
  enabled?: boolean;
}

export interface CreateTagRequest {
  gateway_id: number;
  code: string;
  alias: string;
  data_type: 'INT' | 'REAL' | 'BOOL' | 'DINT';
  historize?: boolean;
  historize_deadband?: number;
  alarm_enabled?: boolean;
  alarm_threshold?: number;
  alarm_operator?: '>' | '<' | '=';
  alarm_priority?: number;
}

export interface UpdateTagRequest {
  code?: string;
  alias?: string;
  data_type?: 'INT' | 'REAL' | 'BOOL' | 'DINT';
  historize?: boolean;
  historize_deadband?: number;
  alarm_enabled?: boolean;
  alarm_threshold?: number;
  alarm_operator?: '>' | '<' | '=';
  alarm_priority?: number;
}

export interface HistoryQueryParams {
  tag_id: number;
  start: string; // ISO 8601 format
  end: string;   // ISO 8601 format
  agg?: string;  // mean, max, min, sum, first, last, count, median, stddev
  interval?: string; // 1m, 5m, 1h, 1d, etc.
}

export interface AcknowledgeResponse {
  id: number;
  tag_id: number;
  state: string;
  message: string;
  triggered_at: string;
  acknowledged_at?: string;
  cleared_at?: string;
}
