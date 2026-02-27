import { useEffect, useRef } from 'react';
import mqtt, { type MqttClient } from 'mqtt';
import { useSparkplugDeviceStore } from '@/stores/useSparkplugDeviceStore';
import { decodeSparkplugB } from '@/utils/sparkplugDecoder';

/**
 * Global hook that listens to Sparkplug B messages and updates device status
 * This should be mounted once at the app root level to track device online/offline status
 * across all pages.
 *
 * PRODUCTION READY:
 * - Ignores retained messages for first 5 seconds (grace period)
 * - Only processes fresh BIRTH/DEATH messages after grace period
 * - DATA messages always update device status (indicates device is alive)
 */
export function useSparkplugListener() {
    const clientRef = useRef<MqttClient | null>(null);
    const { handleBirth, handleDeath, handleData, setListenerConnected } = useSparkplugDeviceStore();

    useEffect(() => {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/mqtt`;

        console.log('[SparkplugListener] Connecting to MQTT broker...');

        const client = mqtt.connect(wsUrl, {
            clientId: `sparkplug-listener-${Date.now()}`,
            clean: true,
            connectTimeout: 10000,
            reconnectPeriod: 5000,
            keepalive: 60,
        });

        clientRef.current = client;

        client.on('connect', () => {
            console.log('[SparkplugListener] Connected to MQTT broker - starting grace period (5s)');

            // Notify store that listener is connected (starts grace period)
            setListenerConnected();

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

                // Try to decode payload
                let metricCount = 0;

                // Check if payload is Protobuf or JSON
                if (isProtobufData(payload)) {
                    try {
                        const decoded = decodeSparkplugB(payload);
                        if (decoded && decoded.metrics) {
                            metricCount = decoded.metrics.length;
                        }
                    } catch {
                        metricCount = 1;
                    }
                } else {
                    // Try JSON parse
                    try {
                        const data = JSON.parse(payload.toString());
                        const metrics = data.Metrics || data.metrics || [];
                        metricCount = Array.isArray(metrics) ? metrics.length : 1;
                    } catch {
                        metricCount = 1;
                    }
                }

                // Update device status based on message type
                if (msgType === 'DBIRTH' || msgType === 'NBIRTH') {
                    handleBirth(groupId, nodeId, deviceId, metricCount);
                } else if (msgType === 'DDEATH' || msgType === 'NDEATH') {
                    handleDeath(groupId, nodeId, deviceId);
                } else if (msgType === 'DDATA' || msgType === 'NDATA') {
                    handleData(groupId, nodeId, deviceId);
                }
            } catch (error) {
                // Silently ignore parse errors
            }
        });

        client.on('error', (error) => {
            console.error('[SparkplugListener] Error:', error);
        });

        client.on('close', () => {
            console.log('[SparkplugListener] Disconnected from MQTT broker');
        });

        return () => {
            if (clientRef.current) {
                console.log('[SparkplugListener] Cleaning up...');
                clientRef.current.end();
                clientRef.current = null;
            }
        };
    }, [handleBirth, handleDeath, handleData, setListenerConnected]);
}

// Helper to detect Protobuf data
function isProtobufData(data: Buffer): boolean {
    if (data.length === 0) return false;
    // Protobuf data typically starts with field tags (0x08, 0x12, 0x18, etc.)
    // JSON starts with '{' (0x7B) or '[' (0x5B)
    const firstByte = data[0];
    return firstByte !== 0x7B && firstByte !== 0x5B;
}
