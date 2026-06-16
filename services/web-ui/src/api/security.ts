import apiClient from './client';

export interface SecurityOverview {
    score: number;
    score_breakdown: {
        audit_logging: boolean;
        rbac_enabled: boolean;
        backup_fresh: boolean;
        rate_limiting: boolean;
        mfa_any_admin: boolean;
        account_lockout_active: boolean;
        strong_password_policy: boolean;
        mqtt_tls: boolean;
    };
    failed_logins_24h: number;
    locked_accounts: number;
    security_events_24h: number;
    nis2_checks_passed: number;
    nis2_checks_total: number;
}

export interface SecurityEvent {
    id: number;
    org_id: number | null;
    event_type: string;
    severity: 'low' | 'medium' | 'high' | 'critical';
    actor: string | null;
    resource: string | null;
    detail: unknown;
    created_at: string;
}

export interface ComplianceCheck {
    id: string;
    name: string;
    article: string;
    passed: boolean;
    detail: string;
}

export const securityApi = {
    overview: (): Promise<SecurityOverview> =>
        apiClient.get('/security/overview').then(r => r.data),
    events: (limit = 50): Promise<SecurityEvent[]> =>
        apiClient.get('/security/events', { params: { limit } }).then(r => r.data),
    compliance: (): Promise<ComplianceCheck[]> =>
        apiClient.get('/security/compliance').then(r => r.data),
};
