import mqtt, { type MqttClient } from 'mqtt';
import { toast } from 'sonner';

class MQTTClientService {
    private client: MqttClient | null = null;
    private reconnectAttempts = 0;
    private maxReconnectAttempts = 5;
    private reconnectDelay = 5000; // 5 seconds
    private isManuallyDisconnected = false;

    constructor() { }

    connect(): Promise<void> {
        return new Promise((resolve, reject) => {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            // Connect through Nginx proxy (same host/port as the web UI)
            const wsUrl = `${protocol}//${window.location.host}/mqtt`;

            console.log('Connecting to MQTT broker:', wsUrl.replace(/\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/, 'xxx.xxx.xxx.xxx'));

            this.client = mqtt.connect(wsUrl, {
                clientId: `web-ui-${Date.now()}`,
                clean: true,
                connectTimeout: 10000,
                reconnectPeriod: this.reconnectDelay,
                keepalive: 60,
            });

            this.client.on('connect', () => {
                console.log('MQTT connected successfully');
                this.reconnectAttempts = 0;
                this.isManuallyDisconnected = false;
                resolve();
            });

            this.client.on('message', (_topic: string, _payload: Buffer) => {
                // Potential future generic message handling
            });

            this.client.on('error', (err) => {
                console.error('MQTT error:', err);
                this.reconnectAttempts++;
                if (this.reconnectAttempts >= this.maxReconnectAttempts) {
                    toast.error('MQTT connection failed. Please refresh the page.');
                    reject(err);
                }
            });

            this.client.on('offline', () => {
                console.warn('MQTT client offline');
                if (!this.isManuallyDisconnected) {
                    toast.warning('MQTT connection lost. Reconnecting...');
                }
            });

            this.client.on('reconnect', () => {
                console.log('MQTT reconnecting...');
            });
        });
    }

    disconnect(): void {
        this.isManuallyDisconnected = true;
        if (this.client) {
            this.client.end();
            this.client = null;
        }
    }

    isConnected(): boolean {
        return this.client?.connected || false;
    }
}

// Singleton instance
let mqttClientInstance: MQTTClientService | null = null;

export const getMQTTClient = (): MQTTClientService => {
    if (!mqttClientInstance) {
        mqttClientInstance = new MQTTClientService();
    }
    return mqttClientInstance;
};

export default MQTTClientService;
