import apiClient from './client';

export interface GatewayInfraEntry {
    id: number;
    name: string;
    driver_type: string;
    org_id: number;
    org_name: string;
    host: string;
    port: number;
    enabled: boolean;
    online: boolean;
    last_seen_at: string | null;
    last_seen_ip: string | null;
    agent_version: string | null;
    tag_count: number;
    tls_enabled: boolean;
    mqtt_auth: boolean;
}

export interface InfrastructureResponse {
    gateways: GatewayInfraEntry[];
    summary: {
        total: number;
        online: number;
        offline: number;
        tls_missing: number;
        mqtt_auth_ok: number;
    };
}

export const infrastructureApi = {
    list: (): Promise<InfrastructureResponse> =>
        apiClient.get('/infrastructure').then(r => r.data),
};
