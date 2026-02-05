import { useEffect, useState, useRef } from 'react';

interface RealtimeUpdate {
    tag_id: number;
    v: any;
    ts: number;
    q: number;
}

export const useRealtime = (orgId: number | undefined) => {
    const [values, setValues] = useState<Map<number, { value: any; timestamp: number; quality: number }>>(new Map());
    const ws = useRef<WebSocket | null>(null);

    useEffect(() => {
        if (!orgId) return;

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        // Use the same host as the API, but check if we are in dev mode (localhost:3004 vs localhost:8080)
        // Usually the API is on port 8080 as per docker-compose
        const apiHost = window.location.hostname === 'localhost' ? 'localhost:8081' : window.location.host;
        const wsUrl = `${protocol}//${apiHost}/api/ws/realtime?org_id=${orgId}`;

        console.log(`[WS] Connecting to ${wsUrl}`);
        const socket = new WebSocket(wsUrl);
        ws.current = socket;

        socket.onmessage = (event) => {
            try {
                const update: RealtimeUpdate = JSON.parse(event.data);
                setValues(prev => {
                    const next = new Map(prev);
                    next.set(update.tag_id, {
                        value: update.v,
                        timestamp: update.ts,
                        quality: update.q
                    });
                    return next;
                });
            } catch (error) {
                console.error('[WS] Failed to parse message', error);
            }
        };

        socket.onopen = () => {
            console.log('[WS] Connected');
        };

        socket.onclose = () => {
            console.log('[WS] Disconnected');
            // Reconnect logic could be added here
        };

        socket.onerror = (error) => {
            console.error('[WS] Error', error);
        };

        return () => {
            if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) {
                socket.close();
            }
        };
    }, [orgId]);

    return values;
};
