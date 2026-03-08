import { create } from 'zustand';
import { getMQTTClient } from '@/lib/mqtt-client';

interface MqttState {
    isMqttConnected: boolean;
    connectMqtt: () => Promise<void>;
    disconnectMqtt: () => void;
}

export const useMqttStore = create<MqttState>((set) => ({
    isMqttConnected: false,
    connectMqtt: async () => {
        const client = getMQTTClient();
        if (client.isConnected()) {
            set({ isMqttConnected: true });
            return;
        }

        try {
            await client.connect();
            set({ isMqttConnected: true });
        } catch (error) {
            console.error('Failed to connect to MQTT:', error);
            set({ isMqttConnected: false });
        }
    },
    disconnectMqtt: () => {
        getMQTTClient().disconnect();
        set({ isMqttConnected: false });
    },
}));
