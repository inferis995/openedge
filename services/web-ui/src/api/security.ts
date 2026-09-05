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
     * Controlli automatici di sicurezza. Non sono una certificazione: sei dei
     * dodici riguardano misure organizzative che il software non può
     * accertare, e arrivano con state 'not_assessed' anziché contati come
     * superati.
     */
    checks_passed: number;
    checks_evaluated: number;
    checks_not_assessed: number;
    checks: SecurityCheck[];
    // Il server serve ancora nis2_checks_passed e nis2_checks_total, deprecati
    // dalla 3.1.0 per non rompere i client scritti sulla 3.0.0. Non sono
    // dichiarati qui apposta: la web UI non deve poterli usare, e un campo che
    // non esiste nel tipo è più difficile da reintrodurre per distrazione di
    // uno marcato @deprecated. Vale lo stesso per `article` su ComplianceCheck.
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
