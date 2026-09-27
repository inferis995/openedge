import api from './client';

export interface MqttExternalClient {
    id: number;
    username: string;
    description: string;
    created_at: string;
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
};
