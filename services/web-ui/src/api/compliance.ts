import apiClient from './client';

export interface OTAsset {
  id: number;
  org_id: number;
  gateway_id: number | null;
  ip_address: string;
  mac_address: string;
  hostname: string;
  vendor: string;
  device_type: 'PLC' | 'HMI' | 'Switch' | 'Router' | 'Sensor' | 'Unknown';
  model: string;
  firmware_ver: string;
  protocol: string;
  is_authorized: boolean;
  risk_score: number;
  last_seen: string;
  discovered_at: string;
  notes: string;
  cves: CVEMatch[];
}

export interface CVEMatch {
  id: number;
  cve_id: string;
  severity: 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW';
  cvss_score: number | null;
  description: string;
  published: string | null;
}

export interface ComplianceFramework {
  id: number;
  code: string;
  name: string;
  version: string;
}

export interface ComplianceRequirement {
  id: number;
  req_code: string;
  category: string;
  title: string;
  description: string;
  weight: number;
  status: 'not_assessed' | 'compliant' | 'partial' | 'non_compliant';
  evidence: string;
  notes: string;
  assessed_at: string | null;
}

export interface RiskPosture {
  total_assets: number;
  avg_risk_score: number;
  critical_cves: number;
  high_cves: number;
  unauthorized_devices: number;
  by_type: { device_type: string; count: number; avg_score: number }[];
  top_risky: OTAsset[];
}

export interface ComplianceScore {
  nis2: FrameworkScore;
  iec62443: FrameworkScore;
  overall: number;
}

export interface FrameworkScore {
  score: number;
  compliant: number;
  partial: number;
  non_compliant: number;
  not_assessed: number;
  total: number;
}

export interface ScanJob {
  id: number;
  subnet: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  started_at: string | null;
  finished_at: string | null;
  found_count: number;
  new_count: number;
  error: string | null;
}

// ─── Threat Monitor types (Step 3) ───────────────────────────────────────────

export interface ThreatEvent {
  id: number;
  org_id: number;
  asset_id: number | null;
  event_type:
    | 'new_device'
    | 'protocol_anomaly'
    | 'config_change'
    | 'vulnerability_alert'
    | 'unauthorized_access'
    | 'cve_published'
    | 'scan_completed';
  severity: 'critical' | 'high' | 'medium' | 'low' | 'info';
  title: string;
  description: string;
  source: string;
  ip_address: string;
  resolved: boolean;
  resolved_at: string | null;
  created_at: string;
}

export interface ThreatSummary {
  total: number;
  unresolved: number;
  last_24h: number;
  by_severity: Record<string, number>;
}

export interface CreateThreatRequest {
  asset_id?: number | null;
  event_type: ThreatEvent['event_type'];
  severity?: ThreatEvent['severity'];
  title: string;
  description?: string;
  source?: string;
  ip_address?: string;
}

// ─── Compliance Report types (Step 4) ────────────────────────────────────────

export interface ComplianceReport {
  id: number;
  org_id: number;
  report_type:
    | 'nis2_assessment'
    | 'iec62443_assessment'
    | 'asset_inventory'
    | 'incident_timeline'
    | 'full_compliance';
  title: string;
  period_from: string | null;
  period_to: string | null;
  status: 'generating' | 'ready' | 'failed';
  format: string;
  content: Record<string, unknown> | null;
  generated_by: number | null;
  generated_at: string;
  error?: string;
}

export interface GenerateReportRequest {
  report_type: ComplianceReport['report_type'];
  title?: string;
  period_from?: string | null;
  period_to?: string | null;
}

// ─── CSIRT Incident types (Art.23) ───────────────────────────────────────────

export interface CSIRTIncident {
  id: number;
  org_id: number;
  threat_event_id?: number;
  title: string;
  description?: string;
  severity: 'critical' | 'high' | 'medium' | 'low';
  status: 'open' | 'early_warning_sent' | 'notified' | 'closed';
  affected_systems?: string;
  impact_description?: string;
  early_warning_due: string;
  notification_due: string;
  final_report_due: string;
  early_warning_sent_at?: string;
  notification_sent_at?: string;
  final_report_sent_at?: string;
  root_cause?: string;
  remediation?: string;
  closed_at?: string;
  created_at: string;
  updated_at: string;
  early_warning_overdue: boolean;
  notification_overdue: boolean;
  final_report_overdue: boolean;
  early_warning_remaining_h: number;
  notification_remaining_h: number;
  final_report_remaining_h: number;
}

export interface CSIRTSummary {
  total: number;
  open: number;
  overdue_early_warning: number;
  overdue_notification: number;
  overdue_final: number;
}

// ─── Vendor Risk types (Art.18) ───────────────────────────────────────────────

export interface OTVendor {
  id: number;
  org_id: number;
  name: string;
  country?: string;
  website?: string;
  contact_email?: string;
  products_used?: string;
  has_iso27001: boolean;
  has_soc2: boolean;
  has_iec62443: boolean;
  last_audit_date?: string;
  data_access_level: 'none' | 'read' | 'write' | 'admin';
  network_access: boolean;
  remote_access: boolean;
  contract_start?: string;
  contract_end?: string;
  security_clauses: boolean;
  risk_score: number;
  criticality: 'low' | 'medium' | 'high' | 'critical';
  notes?: string;
  auto_imported: boolean;
  created_at: string;
  updated_at: string;
}

export interface VendorSummary {
  total: number;
  critical: number;
  high: number;
  avg_score: number;
  contracts_expiring_soon: number;
}

export interface SyncAssetsResult {
  created: number;
  updated: number;
}

export const complianceApi = {
  listAssets: (orgId?: number) => {
    const params = orgId ? `?org_id=${orgId}` : '';
    return apiClient.get<OTAsset[]>(`/compliance/assets${params}`).then(r => r.data);
  },
  createAsset: (data: Partial<OTAsset>) =>
    apiClient.post<OTAsset>('/compliance/assets', data).then(r => r.data),
  updateAsset: (id: number, data: Partial<OTAsset>) =>
    apiClient.put<OTAsset>(`/compliance/assets/${id}`, data).then(r => r.data),
  deleteAsset: (id: number) =>
    apiClient.delete(`/compliance/assets/${id}`).then(r => r.data),
  addCVE: (assetId: number, cve: Partial<CVEMatch>) =>
    apiClient.post(`/compliance/assets/${assetId}/cves`, cve).then(r => r.data),
  removeCVE: (assetId: number, cveId: string) =>
    apiClient.delete(`/compliance/assets/${assetId}/cves/${cveId}`).then(r => r.data),

  listFrameworks: () =>
    apiClient.get<ComplianceFramework[]>('/compliance/frameworks').then(r => r.data),
  getAssessment: (code: string) =>
    apiClient.get<ComplianceRequirement[]>(`/compliance/frameworks/${code}/assessment`).then(r => r.data),
  updateAssessment: (code: string, reqId: number, data: { status: string; evidence?: string; notes?: string }) =>
    apiClient.put(`/compliance/frameworks/${code}/assessment/${reqId}`, data).then(r => r.data),

  getRiskPosture: () =>
    apiClient.get<RiskPosture>('/compliance/risk-posture').then(r => r.data),
  getScore: () =>
    apiClient.get<ComplianceScore>('/compliance/score').then(r => r.data),

  triggerScan: (subnet: string) =>
    apiClient.post<ScanJob>('/compliance/scan', { subnet }).then(r => r.data),
  getScanStatus: (id: number) =>
    apiClient.get<ScanJob>(`/compliance/scan/${id}`).then(r => r.data),

  // ── Threat Monitor (Step 3) ──────────────────────────────────────────────

  /** List threat events. Supports resolved, severity, limit, offset filters. */
  listThreats: (params?: {
    resolved?: boolean;
    severity?: ThreatEvent['severity'];
    limit?: number;
    offset?: number;
  }): Promise<ThreatEvent[]> =>
    apiClient.get('/compliance/threats', { params }).then(r => r.data),

  /** Manually create a threat event (admin only). */
  createThreat: (data: CreateThreatRequest): Promise<{ id: number; message: string }> =>
    apiClient.post('/compliance/threats', data).then(r => r.data),

  /** Mark a threat event as resolved (admin only). */
  resolveThreat: (id: number): Promise<{ message: string }> =>
    apiClient.put(`/compliance/threats/${id}/resolve`, {}).then(r => r.data),

  /** Get aggregate threat counts for the dashboard. */
  getThreatSummary: (): Promise<ThreatSummary> =>
    apiClient.get('/compliance/threats/summary').then(r => r.data),

  // ── Compliance Reports (Step 4) ──────────────────────────────────────────

  /** Generate a new compliance report (async — returns id + status immediately). */
  generateReport: (data: GenerateReportRequest): Promise<{ id: number; status: 'generating' }> =>
    apiClient.post('/compliance/reports', data).then(r => r.data),

  /** List reports for the current org (latest 100). */
  listReports: (): Promise<ComplianceReport[]> =>
    apiClient.get('/compliance/reports').then(r => r.data),

  /** Get a single report including its content JSONB. */
  getReport: (id: number): Promise<ComplianceReport> =>
    apiClient.get(`/compliance/reports/${id}`).then(r => r.data),

  /** Delete a report (admin only). */
  deleteReport: (id: number): Promise<{ message: string }> =>
    apiClient.delete(`/compliance/reports/${id}`).then(r => r.data),

  // ── CSIRT Incidents (Art.23) ─────────────────────────────────────────────

  listIncidents: (params?: { status?: string; severity?: string }): Promise<CSIRTIncident[]> =>
    apiClient.get('/compliance/csirt', { params }).then(r => r.data),
  getIncidentSummary: (): Promise<CSIRTSummary> =>
    apiClient.get('/compliance/csirt/summary').then(r => r.data),
  createIncident: (data: Partial<CSIRTIncident>): Promise<CSIRTIncident> =>
    apiClient.post('/compliance/csirt', data).then(r => r.data),
  updateIncident: (id: number, data: Partial<CSIRTIncident>): Promise<CSIRTIncident> =>
    apiClient.put(`/compliance/csirt/${id}`, data).then(r => r.data),
  markEarlyWarning: (id: number): Promise<unknown> =>
    apiClient.put(`/compliance/csirt/${id}/early-warning`).then(r => r.data),
  markNotification: (id: number): Promise<unknown> =>
    apiClient.put(`/compliance/csirt/${id}/notify`).then(r => r.data),
  closeIncident: (id: number, data: { root_cause?: string; remediation?: string }): Promise<unknown> =>
    apiClient.put(`/compliance/csirt/${id}/close`, data).then(r => r.data),

  // ── Vendor Risk (Art.18) ─────────────────────────────────────────────────

  listVendors: (): Promise<OTVendor[]> =>
    apiClient.get('/compliance/vendors').then(r => r.data),
  getVendorSummary: (): Promise<VendorSummary> =>
    apiClient.get('/compliance/vendors/summary').then(r => r.data),
  createVendor: (data: Partial<OTVendor>): Promise<OTVendor> =>
    apiClient.post('/compliance/vendors', data).then(r => r.data),
  updateVendor: (id: number, data: Partial<OTVendor>): Promise<OTVendor> =>
    apiClient.put(`/compliance/vendors/${id}`, data).then(r => r.data),
  deleteVendor: (id: number): Promise<unknown> =>
    apiClient.delete(`/compliance/vendors/${id}`).then(r => r.data),
  syncVendorsFromGateways: (): Promise<{ imported: number; updated: number }> =>
    apiClient.post('/compliance/vendors/sync').then(r => r.data),

  // ── Asset & Auto-assess helpers ──────────────────────────────────────────

  syncAssetsFromGateways: (): Promise<SyncAssetsResult> =>
    apiClient.post('/compliance/sync-assets').then(r => r.data),
  autoAssess: (): Promise<Array<{ req_code: string; status: string; evidence: string }>> =>
    apiClient.get('/compliance/auto-assess').then(r => r.data),
};
