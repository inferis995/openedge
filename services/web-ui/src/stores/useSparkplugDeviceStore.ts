// Sparkplug B Device Status Store
// Tracks device online/offline status based on DBIRTH/DDEATH messages
// This is the CORRECT way to determine if data is stale - not by value timestamp
//
// OPTIMIZED: Reduces unnecessary re-renders by only updating when status actually changes

import { create } from 'zustand';
import { persist } from 'zustand/middleware';

// Device status from Sparkplug B
export type DeviceStatus = 'online' | 'offline' | 'unknown';

export interface SparkplugDevice {
    deviceId: string;
    groupId: string;
    nodeId: string;
    status: DeviceStatus;
    lastBirth: number;      // timestamp of last DBIRTH
    lastDeath: number;      // timestamp of last DDEATH
    lastData: number;       // timestamp of last DDATA
    metricCount: number;    // number of metrics in last DBIRTH
}

interface SparkplugDeviceState {
    devices: Map<string, SparkplugDevice>;
    isListenerReady: boolean;  // True after grace period (ignores retained messages)
    listenerConnectedAt: number | null;  // When listener connected

    // Actions
    setListenerConnected: () => void;
    setListenerReady: () => void;
    handleBirth: (groupId: string, nodeId: string, deviceId: string, metricCount: number) => void;
    handleDeath: (groupId: string, nodeId: string, deviceId: string) => void;
    handleData: (groupId: string, nodeId: string, deviceId: string) => void;
    getDeviceStatus: (deviceId: string) => DeviceStatus;
    isDeviceOnline: (deviceId: string) => boolean;
    getDevice: (deviceId: string) => SparkplugDevice | undefined;
    clearDevices: () => void;
}

// Generate a unique key for device lookup
const getDeviceKey = (groupId: string, nodeId: string, deviceId: string): string => {
    return `${groupId}/${nodeId}/${deviceId}`;
};

// Grace period in ms before trusting Sparkplug messages (ignores retained messages)
const GRACE_PERIOD_MS = 5000;

// Throttle interval for data updates (ms)
const DATA_UPDATE_THROTTLE = 1000;

export const useSparkplugDeviceStore = create<SparkplugDeviceState>()(
    persist(
        (set, get) => ({
            devices: new Map<string, SparkplugDevice>(),
            isListenerReady: false,
            listenerConnectedAt: null,

            setListenerConnected: () => {
                set({ listenerConnectedAt: Date.now(), isListenerReady: false });

                // Set ready after grace period
                setTimeout(() => {
                    set({ isListenerReady: true });
                    console.log('[Sparkplug] Listener ready - now tracking device status');
                }, GRACE_PERIOD_MS);
            },

            setListenerReady: () => {
                set({ isListenerReady: true });
            },

            handleBirth: (groupId, nodeId, deviceId, metricCount) => {
                // Ignore messages during grace period (retained messages)
                const state = get();
                if (!state.isListenerReady) {
                    return;
                }

                const key = getDeviceKey(groupId, nodeId, deviceId);
                const now = Date.now();

                // Only update if status actually changes
                const existing = state.devices.get(key);
                if (existing && existing.status === 'online' && existing.metricCount === metricCount) {
                    return; // No change needed
                }

                set((state) => {
                    const newDevices = new Map(state.devices);
                    newDevices.set(key, {
                        deviceId,
                        groupId,
                        nodeId,
                        status: 'online',
                        lastBirth: now,
                        lastDeath: 0,
                        lastData: now,
                        metricCount,
                    });
                    return { devices: newDevices };
                });
            },

            handleDeath: (groupId, nodeId, deviceId) => {
                // Ignore messages during grace period (retained messages)
                const state = get();
                if (!state.isListenerReady) {
                    return;
                }

                const key = getDeviceKey(groupId, nodeId, deviceId);
                const now = Date.now();

                // Only update if status actually changes
                const existing = state.devices.get(key);
                if (existing && existing.status === 'offline') {
                    return; // Already offline
                }

                set((state) => {
                    const newDevices = new Map(state.devices);
                    const existing = newDevices.get(key);
                    if (existing) {
                        newDevices.set(key, {
                            ...existing,
                            status: 'offline',
                            lastDeath: now,
                        });
                    } else {
                        newDevices.set(key, {
                            deviceId,
                            groupId,
                            nodeId,
                            status: 'offline',
                            lastBirth: 0,
                            lastDeath: now,
                            lastData: 0,
                            metricCount: 0,
                        });
                    }
                    return { devices: newDevices };
                });
            },

            handleData: (groupId, nodeId, deviceId) => {
                // Always process DATA messages - they indicate device is alive
                // BUT throttle updates to prevent excessive re-renders
                const key = getDeviceKey(groupId, nodeId, deviceId);
                const now = Date.now();

                const state = get();
                const existing = state.devices.get(key);

                // Throttle: only update if lastData is older than DATA_UPDATE_THROTTLE
                if (existing && existing.lastData && (now - existing.lastData) < DATA_UPDATE_THROTTLE) {
                    return; // Throttled
                }

                // Only update if status changes or throttle expired
                if (existing && existing.status === 'online' && (now - existing.lastData) < DATA_UPDATE_THROTTLE) {
                    return;
                }

                set((state) => {
                    const newDevices = new Map(state.devices);
                    const existing = newDevices.get(key);
                    if (existing) {
                        newDevices.set(key, {
                            ...existing,
                            lastData: now,
                            // If we receive data, device is definitely online
                            status: 'online',
                        });
                    }
                    return { devices: newDevices };
                });
            },

            getDeviceStatus: (deviceId) => {
                const state = get();

                // If listener not ready, return unknown (use quality-based status)
                if (!state.isListenerReady) {
                    return 'unknown';
                }

                // Find device by deviceId (without needing full path)
                for (const device of state.devices.values()) {
                    if (device.deviceId === deviceId) {
                        return device.status;
                    }
                }
                return 'unknown';
            },

            isDeviceOnline: (deviceId) => {
                return get().getDeviceStatus(deviceId) === 'online';
            },

            getDevice: (deviceId) => {
                const state = get();
                for (const device of state.devices.values()) {
                    if (device.deviceId === deviceId) {
                        return device;
                    }
                }
                return undefined;
            },

            clearDevices: () => {
                set({ devices: new Map(), isListenerReady: false, listenerConnectedAt: null });
            },
        }),
        {
            name: 'sparkplug-device-store',
            // Don't persist - always fresh on page load
            partialize: () => ({}),
        }
    )
);

// Helper to parse Sparkplug B topic and extract components
export function parseSparkplugTopic(topic: string): {
    groupId: string;
    msgType: string;
    nodeId: string;
    deviceId: string;
} | null {
    // Topic format: spBv1.0/{groupId}/{msgType}/{nodeId}[/{deviceId}]
    const parts = topic.split('/');
    if (parts.length < 4 || parts[0] !== 'spBv1.0') {
        return null;
    }

    return {
        groupId: parts[1],
        msgType: parts[2],
        nodeId: parts[3],
        deviceId: parts[4] || parts[3], // If no deviceId, use nodeId
    };
}

// Hook to get correct data status based on Sparkplug B device state
export function useSparkplugDataStatus(deviceId: string, quality: number): {
    status: 'good' | 'bad' | 'unknown';
    isOnline: boolean;
    device: SparkplugDevice | undefined;
} {
    const { getDeviceStatus, getDevice } = useSparkplugDeviceStore();
    const deviceStatus = getDeviceStatus(deviceId);
    const device = getDevice(deviceId);

    let status: 'good' | 'bad' | 'unknown';

    if (deviceStatus === 'unknown') {
        // No Sparkplug info - fall back to quality
        status = quality === 0 ? 'good' : 'bad';
    } else if (deviceStatus === 'offline') {
        status = 'bad';
    } else {
        // Device is online
        status = quality === 0 ? 'good' : 'bad';
    }

    return {
        status,
        isOnline: deviceStatus === 'online',
        device,
    };
}
