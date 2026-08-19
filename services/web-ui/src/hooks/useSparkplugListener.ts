import { useEffect, useRef } from 'react';
import { type MqttClient } from 'mqtt';
import { connectAuthenticatedMqtt } from '@/lib/mqtt-client';
import { useSparkplugDeviceStore } from '@/stores/useSparkplugDeviceStore';
import { decodeSparkplugB } from '@/utils/sparkplugDecoder';

/**
 * Global hook that listens to Sparkplug B messages and updates device status
 * This should be mounted once at the app root level to track device online/offline status
 * across all pages.
 *
 * OPTIMIZED: Uses throttled updates to prevent UI blocking with high-frequency messages
 */
export function useSparkplugListener() {
    const clientRef = useRef<MqttClient | null>(null);
    const isMountedRef = useRef(true);
    const pendingUpdatesRef = useRef<Map<string, { type: 'birth' | 'death' | 'data', groupId: string, nodeId: string, deviceId: string, metricCount?: number }>>(new Map());
    const flushTimeoutRef = useRef<NodeJS.Timeout | null>(null);

    useEffect(() => {
        isMountedRef.current = true;

        console.log('[SparkplugListener] Connecting to MQTT broker...');

        // The broker requires credentials now, and fetching them is a round trip
        // to the API — so the client arrives asynchronously and its handlers are
        // registered in wire() below once it does. `disposed` covers the case
        // where the component unmounts while that request is still in flight:
        // without it the connection would be opened after cleanup had run and
        // never closed.
        let disposed = false;

        // Flush pending updates in batch (throttled)
        const flushPendingUpdates = () => {
            if (!isMountedRef.current) return;

            const pending = pendingUpdatesRef.current;
            if (pending.size === 0) return;

            pendingUpdatesRef.current = new Map();

            const { handleBirth, handleDeath, handleData } = useSparkplugDeviceStore.getState();

            // Process only the last update for each device
            pending.forEach((update) => {
                try {
                    if (update.type === 'birth') {
                        handleBirth(update.groupId, update.nodeId, update.deviceId, update.metricCount || 1);
                    } else if (update.type === 'death') {
                        handleDeath(update.groupId, update.nodeId, update.deviceId);
                    } else if (update.type === 'data') {
                        handleData(update.groupId, update.nodeId, update.deviceId);
                    }
                } catch (e) {
                    // Ignore errors
                }
            });
        };

        // Schedule batch flush every 200ms
        const scheduleFlush = () => {
            if (flushTimeoutRef.current) return;
            flushTimeoutRef.current = setTimeout(() => {
                flushTimeoutRef.current = null;
                flushPendingUpdates();
            }, 200);
        };

        // Every handler this hook needs, registered once the signed-in client
        // exists.
        const wire = (client: MqttClient) => {
            client.on('connect', () => {
                if (!isMountedRef.current) return;
                console.log('[SparkplugListener] Connected to MQTT broker - starting grace period (5s)');

                // Notify store that listener is connected (starts grace period)
                useSparkplugDeviceStore.getState().setListenerConnected();

                // Subscribe to Sparkplug B topics
                client.subscribe('spBv1.0/#', { qos: 0 }, (err) => {
                    if (err) {
                        console.error('[SparkplugListener] Subscribe error:', err);
                    } else {
                        console.log('[SparkplugListener] Subscribed to spBv1.0/#');
                    }
                });
            });

            client.on('message', (topic: string, payload: Buffer) => {
                if (!isMountedRef.current) return;

                try {
                    // Only process Sparkplug B topics
                    if (!topic.startsWith('spBv1.0/')) return;

                    // Parse topic: spBv1.0/{groupId}/{msgType}/{nodeId}[/{deviceId}]
                    const topicParts = topic.split('/');
                    if (topicParts.length < 4) return;

                    const groupId = topicParts[1];
                    const msgType = topicParts[2];
                    const nodeId = topicParts[3];
                    const deviceId = topicParts[4] || nodeId;

                    // Determine update type
                    let updateType: 'birth' | 'death' | 'data';
                    if (msgType === 'DBIRTH' || msgType === 'NBIRTH') {
                        updateType = 'birth';
                    } else if (msgType === 'DDEATH' || msgType === 'NDEATH') {
                        updateType = 'death';
                    } else if (msgType === 'DDATA' || msgType === 'NDATA') {
                        updateType = 'data';
                    } else {
                        return; // Unknown message type
                    }

                    // Get metric count for birth messages
                    let metricCount = 1;
                    if (updateType === 'birth') {
                        if (isProtobufData(payload)) {
                            try {
                                const decoded = decodeSparkplugB(payload);
                                if (decoded && decoded.metrics) {
                                    metricCount = decoded.metrics.length;
                                }
                            } catch { }
                        } else {
                            try {
                                const data = JSON.parse(payload.toString());
                                const metrics = data.Metrics || data.metrics || [];
                                metricCount = Array.isArray(metrics) ? metrics.length : 1;
                            } catch { }
                        }
                    }

                    // Queue update (will overwrite previous for same device)
                    const key = `${groupId}/${nodeId}/${deviceId}`;
                    pendingUpdatesRef.current.set(key, {
                        type: updateType,
                        groupId,
                        nodeId,
                        deviceId,
                        metricCount
                    });

                    // Schedule batch processing
                    scheduleFlush();
                } catch (error) {
                    // Silently ignore parse errors
                }
            });

            client.on('error', (error) => {
                console.error('[SparkplugListener] Error:', error);
            });

            client.on('close', () => {
                if (!isMountedRef.current) return;
                console.log('[SparkplugListener] Disconnected from MQTT broker');
            });

        };

        connectAuthenticatedMqtt('sparkplug-listener')
            .then((client) => {
                if (disposed) {
                    client.end(true);
                    return;
                }
                clientRef.current = client;
                wire(client);
            })
            .catch((err) => {
                console.error('[SparkplugListener] no MQTT credentials, not connecting:', err);
            });

        return () => {
            console.log('[SparkplugListener] Cleanup');
            isMountedRef.current = false;
            disposed = true;

            // Clear pending timeout
            if (flushTimeoutRef.current) {
                clearTimeout(flushTimeoutRef.current);
                flushTimeoutRef.current = null;
            }

            // Clear pending updates
            pendingUpdatesRef.current.clear();

            // Force close MQTT connection
            if (clientRef.current) {
                clientRef.current.removeAllListeners();
                clientRef.current.end(true);
                clientRef.current = null;
            }
        };
    }, []);
}

// Helper to detect Protobuf data
function isProtobufData(data: Buffer | Uint8Array | null | undefined): boolean {
    if (!data || data.length === 0) return false;
    const firstByte = data[0];
    return firstByte !== 0x7B && firstByte !== 0x5B;
}
