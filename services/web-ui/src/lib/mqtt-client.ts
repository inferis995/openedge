import mqtt, { type MqttClient } from 'mqtt';
import { toast } from 'sonner';

export interface AlarmEvent {
    tag: number;
    state: 'ACTIVE' | 'RTN' | 'ACKNOWLEDGED' | 'CLEAR';
    msg: string;
    ts: number;
}

export type AlarmEventListener = (event: AlarmEvent) => void;

class MQTTClientService {
    private client: MqttClient | null = null;
    private listeners: Set<AlarmEventListener> = new Set();
    private reconnectAttempts = 0;
    private maxReconnectAttempts = 5;
    private reconnectDelay = 5000; // 5 seconds
    private isManuallyDisconnected = false;
    private notificationSoundEnabled = false;
    private audio: HTMLAudioElement | null = null;

    constructor() {
        // Load notification sound preference from localStorage
        const savedPref = localStorage.getItem('ralph_notification_sound');
        this.notificationSoundEnabled = savedPref === 'true';

        // Preload notification sound
        try {
            this.audio = new Audio('/notification.mp3');
            this.audio.volume = 0.5;
        } catch (e) {
            console.warn('Could not preload notification sound:', e);
        }
    }

    connect(): Promise<void> {
        return new Promise((resolve, reject) => {
            // Determine WebSocket URL based on current location
            // Determine WebSocket URL based on current location
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const env = (import.meta as any).env;
            const host = env.VITE_MQTT_HOST || window.location.hostname;
            const port = env.VITE_MQTT_PORT || (window.location.protocol === 'https:' ? 443 : 80);
            const path = env.VITE_MQTT_PATH || '/mqtt';

            const wsUrl = `${protocol}//${host}:${port}${path}`;

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

                // Subscribe to alarm events
                this.client?.subscribe('events/alarms/#', { qos: 1 }, (err) => {
                    if (err) {
                        console.error('Failed to subscribe to alarm events:', err);
                    } else {
                        console.log('Subscribed to events/alarms/#');
                    }
                });

                resolve();
            });

            this.client.on('message', (topic: string, payload: Buffer) => {
                try {
                    // Parse topic: events/alarms/{tag_id}
                    const topicParts = topic.split('/');
                    const tagId = parseInt(topicParts[2]);

                    // Parse payload
                    const message = payload.toString();
                    const event: AlarmEvent = JSON.parse(message);

                    // Ensure tag ID matches topic
                    event.tag = tagId;

                    // Notify all listeners
                    this.notifyListeners(event);

                    // Show toast for new ACTIVE alarms
                    if (event.state === 'ACTIVE') {
                        this.handleActiveAlarm(event);
                    }

                } catch (error) {
                    console.error('Error parsing MQTT message:', error);
                }
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

    // Subscribe to alarm events
    onAlarmEvent(listener: AlarmEventListener): () => void {
        this.listeners.add(listener);
        // Return unsubscribe function
        return () => {
            this.listeners.delete(listener);
        };
    }

    private notifyListeners(event: AlarmEvent): void {
        this.listeners.forEach(listener => {
            try {
                listener(event);
            } catch (error) {
                console.error('Error in alarm event listener:', error);
            }
        });
    }

    private handleActiveAlarm(event: AlarmEvent): void {
        // Play notification sound if enabled
        if (this.notificationSoundEnabled && this.audio) {
            this.playNotificationSound();
        }

        // Show toast notification
        toast.error(`Alarm Active`, {
            description: event.msg,
            duration: 10000,
            action: {
                label: 'View',
                onClick: () => {
                    window.location.href = '/alarms';
                },
            },
        });
    }

    private playNotificationSound(): void {
        if (this.audio) {
            // Reset audio to start to allow replay
            this.audio.currentTime = 0;
            this.audio.play().catch((err) => {
                console.warn('Could not play notification sound:', err);
                // Browser may block autoplay without user interaction
            });
        }
    }

    setNotificationSoundEnabled(enabled: boolean): void {
        this.notificationSoundEnabled = enabled;
        localStorage.setItem('ralph_notification_sound', String(enabled));
    }

    isNotificationSoundEnabled(): boolean {
        return this.notificationSoundEnabled;
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
