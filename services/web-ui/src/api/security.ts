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
    /**
     * Controlli automatici sulla postura di sicurezza, modellati sulle misure
     * dell'art. 21 NIS2. Non sono una dichiarazione di conformità: sei dei
     * dodici riguardano misure organizzative che il software non può
     * accertare, e arrivano con state 'not_assessed' anziché contati come
     * superati.
     */
    checks_passed: number;
    checks_evaluated: number;
    checks_not_assessed: number;
    checks: SecurityCheck[];
}

export interface SecurityCheck {
    id: string;
    state: 'pass' | 'fail' | 'not_assessed';
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
    state: 'pass' | 'fail' | 'not_assessed';
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
