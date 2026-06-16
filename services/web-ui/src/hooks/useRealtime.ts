import { useEffect, useState, useRef } from 'react';

interface RealtimeUpdate {
    tag_id: number;
    v: unknown;
    ts: number;
    q: number;
}

export interface RealtimeResult {
    values: Map<number, { value: unknown; timestamp: number; quality: number }>;
    connected: boolean;
}

export const useRealtime = (orgId: number | undefined, tagIds?: ReadonlySet<number>): RealtimeResult => {
    const [values, setValues] = useState<Map<number, { value: unknown; timestamp: number; quality: number }>>(new Map());
    const [connected, setConnected] = useState(false);
    const ws = useRef<WebSocket | null>(null);
    const tagIdsRef = useRef<ReadonlySet<number> | undefined>(tagIds);

    // Keep ref in sync without causing WS reconnect
    useEffect(() => {
        tagIdsRef.current = tagIds;
    }, [tagIds]);

    useEffect(() => {
        if (!orgId) return;

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/api/ws/realtime?org_id=${orgId}`;

        console.warn(`[WS] Connecting to ${wsUrl}`);
        const socket = new WebSocket(wsUrl);
        ws.current = socket;

        socket.onmessage = (event) => {
            try {
                const update: RealtimeUpdate = JSON.parse(event.data as string);
                const filter = tagIdsRef.current;
                // Skip if filter is active and tag not in set
                if (filter && filter.size > 0 && !filter.has(update.tag_id)) return;
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
            console.warn('[WS] Connected');
            setConnected(true);
        };

        socket.onclose = () => {
            console.warn('[WS] Disconnected');
            setConnected(false);
        };

        socket.onerror = (error) => {
            console.error('[WS] Error', error);
            setConnected(false);
        };

        return () => {
            if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) {
                socket.close();
            }
            setConnected(false);
        };
    }, [orgId]);

    return { values, connected };
};
