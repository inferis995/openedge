import { useEffect, useState, useRef } from 'react';
import { useAuthStore } from '../stores/useAuthStore';

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

// Reconnect backoff: quick first retry so a blip is invisible, capped so a long
// backend outage does not hammer the server.
const RECONNECT_MIN_MS = 1_000;
const RECONNECT_MAX_MS = 30_000;

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

        const token = useAuthStore.getState().token;
        if (!token) return;

        // A dropped socket used to be terminal: values stayed frozen on screen
        // while the operator believed they were watching a live plant. The
        // socket now reconnects with backoff, and `connected` reports the truth
        // so pages can mark the data stale.
        let disposed = false;
        let retryDelay = RECONNECT_MIN_MS;
        let retryTimer: ReturnType<typeof setTimeout> | undefined;

        const connect = () => {
            if (disposed) return;

            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            // The org is derived server-side from the JWT; organization_id only
            // tells a global admin's socket which tenant to follow.
            const wsUrl = `${protocol}//${window.location.host}/api/ws/realtime?organization_id=${orgId}`;

            // A browser cannot set an Authorization header on a WebSocket, so the
            // JWT travels as a subprotocol value — keeping it out of access logs
            // and browser history, unlike a query parameter.
            const socket = new WebSocket(wsUrl, ['bearer', token]);
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
                retryDelay = RECONNECT_MIN_MS; // healthy again — reset backoff
                setConnected(true);
            };

            const scheduleReconnect = () => {
                setConnected(false);
                if (disposed) return;
                retryTimer = setTimeout(connect, retryDelay);
                retryDelay = Math.min(retryDelay * 2, RECONNECT_MAX_MS);
            };

            socket.onclose = () => {
                console.warn('[WS] Disconnected — will retry');
                scheduleReconnect();
            };

            socket.onerror = (error) => {
                console.error('[WS] Error', error);
                // onclose always follows onerror; closing here would double-schedule.
                socket.close();
            };
        };

        connect();

        return () => {
            disposed = true;
            if (retryTimer) clearTimeout(retryTimer);
            const socket = ws.current;
            if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
                socket.close();
            }
            setConnected(false);
        };
    }, [orgId]);

    return { values, connected };
};
