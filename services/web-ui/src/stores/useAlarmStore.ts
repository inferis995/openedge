import { create } from 'zustand';
import { getMQTTClient, type AlarmEvent } from '@/lib/mqtt-client';

interface AlarmStore {
    // State
    activeAlarmCount: number;
    lastAlarmEvent: AlarmEvent | null;
    isMqttConnected: boolean;
    notificationSoundEnabled: boolean;

    // Actions
    setActiveAlarmCount: (count: number) => void;
    setLastAlarmEvent: (event: AlarmEvent | null) => void;
    setMqttConnected: (connected: boolean) => void;
    setNotificationSoundEnabled: (enabled: boolean) => void;
    incrementActiveCount: () => void;
    decrementActiveCount: () => void;

    // Initialize MQTT connection
    connectMqtt: () => void;
    disconnectMqtt: () => void;
}

export const useAlarmStore = create<AlarmStore>((set) => ({
    // Initial state
    activeAlarmCount: 0,
    lastAlarmEvent: null,
    isMqttConnected: false,
    notificationSoundEnabled: getMQTTClient().isNotificationSoundEnabled(),

    // Actions
    setActiveAlarmCount: (count) => set({ activeAlarmCount: count }),
    setLastAlarmEvent: (event) => set({ lastAlarmEvent: event }),
    setMqttConnected: (connected) => set({ isMqttConnected: connected }),
    setNotificationSoundEnabled: (enabled) => {
        getMQTTClient().setNotificationSoundEnabled(enabled);
        set({ notificationSoundEnabled: enabled });
    },
    incrementActiveCount: () => set((state) => ({
        activeAlarmCount: state.activeAlarmCount + 1,
    })),
    decrementActiveCount: () => set((state) => ({
        activeAlarmCount: Math.max(0, state.activeAlarmCount - 1),
    })),

    // MQTT connection management
    connectMqtt: () => {
        const mqttClient = getMQTTClient();

        // Connect to MQTT broker
        mqttClient.connect()
            .then(() => {
                set({ isMqttConnected: true });
            })
            .catch((error) => {
                console.error('Failed to connect to MQTT:', error);
                set({ isMqttConnected: false });
            });

        // Subscribe to alarm events
        mqttClient.onAlarmEvent((event: AlarmEvent) => {
            set({ lastAlarmEvent: event });

            // Update active alarm count based on state
            if (event.state === 'ACTIVE') {
                set((state) => ({
                    activeAlarmCount: state.activeAlarmCount + 1,
                }));
            } else if (event.state === 'RTN' || event.state === 'ACKNOWLEDGED' || event.state === 'CLEAR') {
                set((state) => ({
                    activeAlarmCount: Math.max(0, state.activeAlarmCount - 1),
                }));
            }
        });

        // Listen for connection status changes
        const checkConnection = setInterval(() => {
            set({ isMqttConnected: mqttClient.isConnected() });
        }, 5000);

        // Cleanup interval on unmount (handled by store lifecycle)
        return () => {
            clearInterval(checkConnection);
        };
    },

    disconnectMqtt: () => {
        getMQTTClient().disconnect();
        set({ isMqttConnected: false });
    },
}));
