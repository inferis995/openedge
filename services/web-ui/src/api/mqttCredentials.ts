import api from './client';

/**
 * The identity the browser opens its MQTT connection with.
 *
 * The UI used to connect to the broker with no credentials at all. Behind the
 * cloud proxy that meant anyone on the internet could subscribe to every
 * tenant's live data and publish setpoint writes, so the broker now requires
 * authentication on the WebSocket listener too. These credentials are issued
 * per organization, are read-only on the broker, and only to a session that is
 * already signed in for that organization.
 */
export interface MqttUICredentials {
    username: string;
    password: string;
    /** Path the nginx proxy exposes the broker on. */
    path: string;
}

export const mqttCredentialsApi = {
    get: async (): Promise<MqttUICredentials> => {
        const { data } = await api.get('/mqtt/ui-credentials');
        return data;
    },
};
