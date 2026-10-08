import api from './client';

export interface MqttExternalClient {
    id: number;
    username: string;
    description: string;
    created_at: string;
    enabled: boolean;
    on_broker: boolean;
}

export interface MaintainedMqttClient extends MqttExternalClient {
    org_id: number;
    org_name: string;
}

export interface BrokerMqttClient {
    username: string;
    disabled: boolean;
    roles: string[];
}

export const mqttClientsApi = {
    list: async (orgId: number): Promise<MqttExternalClient[]> => {
        const { data } = await api.get(`/organizations/${orgId}/mqtt-clients`);
        return data;
    },
    /** The password is in this response only; it is not stored anywhere. */
    create: async (orgId: number, description: string): Promise<MqttExternalClient & {
        password: string; broker_host: string; broker_port: number; broker_tls: boolean;
    }> => {
        const { data } = await api.post(`/organizations/${orgId}/mqtt-clients`, { description });
        return data;
    },
    remove: async (orgId: number, id: number): Promise<void> => {
        await api.delete(`/organizations/${orgId}/mqtt-clients/${id}`);
    },
    /** New password, shown once. The username does not change. */
    rotate: async (orgId: number, id: number): Promise<{ username: string; password: string }> => {
        const { data } = await api.post(`/organizations/${orgId}/mqtt-clients/${id}/rotate`);
        return data;
    },
    setEnabled: async (orgId: number, id: number, enabled: boolean): Promise<void> => {
        await api.post(`/organizations/${orgId}/mqtt-clients/${id}/enabled`, { enabled });
    },
};

export const mqttMaintenanceApi = {
    /** Whole registry across organizations (global admin). */
    listAll: async (orgId?: number): Promise<{ items: MaintainedMqttClient[]; total: number }> => {
        const { data } = await api.get('/system/mqtt/external-clients', orgId ? { params: { org_id: orgId } } : undefined);
        return data;
    },
    /** Every identity the broker knows, read-only (global admin). */
    brokerClients: async (): Promise<{ items: BrokerMqttClient[]; total: number }> => {
        const { data } = await api.get('/system/mqtt/broker-clients');
        return data;
    },
};
