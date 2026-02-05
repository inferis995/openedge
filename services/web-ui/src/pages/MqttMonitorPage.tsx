import { useState, useEffect, useRef, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow
} from '@/components/ui/table';
import {
    Play,
    Pause,
    Trash2,
    Radio,
    Filter,
    ArrowDown,
    Wifi
} from 'lucide-react';
import mqtt, { type MqttClient } from 'mqtt';

interface MqttMessage {
    id: string;
    topic: string;
    tagId: number;
    value: number | boolean | string;
    timestamp: number;
    quality: number;
    receivedAt: Date;
}

const MAX_MESSAGES = 500;

const MqttMonitorPage = () => {
    const [messages, setMessages] = useState<MqttMessage[]>([]);
    const [isConnected, setIsConnected] = useState(false);
    const [isPaused, setIsPaused] = useState(false);
    const [filter, setFilter] = useState('');
    const [autoScroll, setAutoScroll] = useState(true);
    const [messageCount, setMessageCount] = useState(0);
    const clientRef = useRef<MqttClient | null>(null);
    const tableEndRef = useRef<HTMLDivElement>(null);
    const pausedMessagesRef = useRef<MqttMessage[]>([]);
    const isPausedRef = useRef(isPaused);

    // Keep ref in sync with state
    useEffect(() => {
        isPausedRef.current = isPaused;
    }, [isPaused]);

    // Connect to MQTT broker directly
    useEffect(() => {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const host = window.location.hostname;
        const port = 9001;
        const path = '/mqtt';
        const wsUrl = `${protocol}//${host}:${port}${path}`;

        console.log('[MQTT Monitor] Connecting to:', wsUrl);

        const client = mqtt.connect(wsUrl, {
            clientId: `mqtt-monitor-${Date.now()}`,
            clean: true,
            connectTimeout: 10000,
            reconnectPeriod: 3000,
            keepalive: 60,
        });

        clientRef.current = client;

        client.on('connect', () => {
            setIsConnected(true);
            console.log('[MQTT Monitor] Connected to MQTT broker');

            // Subscribe to all data topics
            client.subscribe('data/#', { qos: 0 }, (err) => {
                if (err) {
                    console.error('[MQTT Monitor] Subscribe error:', err);
                } else {
                    console.log('[MQTT Monitor] Subscribed to data/#');
                }
            });
        });

        client.on('message', (topic: string, payload: Buffer) => {
            try {
                const message = payload.toString();
                let data: any;

                try {
                    data = JSON.parse(message);
                } catch {
                    // If not valid JSON, use raw message as value
                    data = { v: message, ts: Date.now(), q: 0 };
                }

                const msg: MqttMessage = {
                    id: `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
                    topic: topic,
                    tagId: data.tag_id || 0,
                    value: data.v ?? data.value ?? '-',
                    timestamp: data.ts || data.timestamp || Date.now(),
                    quality: data.q ?? data.quality ?? 0,
                    receivedAt: new Date()
                };

                setMessageCount(prev => prev + 1);

                if (isPausedRef.current) {
                    pausedMessagesRef.current.push(msg);
                } else {
                    setMessages(prev => {
                        const newMessages = [...prev, msg];
                        if (newMessages.length > MAX_MESSAGES) {
                            return newMessages.slice(-MAX_MESSAGES);
                        }
                        return newMessages;
                    });
                }
            } catch (e) {
                console.error('[MQTT Monitor] Parse error:', e);
            }
        });

        client.on('error', (err) => {
            console.error('[MQTT Monitor] Error:', err);
        });

        client.on('close', () => {
            setIsConnected(false);
            console.log('[MQTT Monitor] Disconnected');
        });

        client.on('reconnect', () => {
            console.log('[MQTT Monitor] Reconnecting...');
        });

        return () => {
            if (client) {
                client.end();
            }
        };
    }, []);

    // Auto-scroll to bottom
    useEffect(() => {
        if (autoScroll && tableEndRef.current) {
            tableEndRef.current.scrollIntoView({ behavior: 'smooth' });
        }
    }, [messages, autoScroll]);

    // Resume: add paused messages
    const handleResume = useCallback(() => {
        if (pausedMessagesRef.current.length > 0) {
            setMessages(prev => {
                const newMessages = [...prev, ...pausedMessagesRef.current];
                pausedMessagesRef.current = [];
                if (newMessages.length > MAX_MESSAGES) {
                    return newMessages.slice(-MAX_MESSAGES);
                }
                return newMessages;
            });
        }
        setIsPaused(false);
    }, []);

    const handleClear = () => {
        setMessages([]);
        pausedMessagesRef.current = [];
    };

    const getQualityBadge = (quality: number) => {
        if (quality === 0) {
            return <Badge className="bg-green-500 text-white">GOOD</Badge>;
        } else if (quality === 1) {
            return <Badge className="bg-yellow-500 text-white">UNCERTAIN</Badge>;
        } else {
            return <Badge className="bg-red-500 text-white">BAD</Badge>;
        }
    };

    const formatValue = (value: number | boolean | string) => {
        if (typeof value === 'boolean') {
            return value ? 'TRUE' : 'FALSE';
        }
        if (typeof value === 'number') {
            return Number.isInteger(value) ? value.toString() : value.toFixed(4);
        }
        return String(value);
    };

    const formatTime = (date: Date) => {
        const time = date.toLocaleTimeString('it-IT', {
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit'
        });
        const ms = date.getMilliseconds().toString().padStart(3, '0');
        return `${time}.${ms}`;
    };

    // Filter messages
    const filteredMessages = filter
        ? messages.filter(m =>
            m.topic.toLowerCase().includes(filter.toLowerCase()) ||
            m.tagId.toString().includes(filter)
        )
        : messages;

    return (
        <div className="space-y-4 h-full flex flex-col">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                    <Radio className={`h-6 w-6 ${isConnected ? 'text-green-500 animate-pulse' : 'text-red-500'}`} />
                    <div>
                        <h2 className="text-2xl font-bold tracking-tight">MQTT Live Monitor</h2>
                        <p className="text-muted-foreground text-sm">
                            Real-time message stream • {messageCount.toLocaleString()} messages received
                        </p>
                    </div>
                </div>

                <div className="flex items-center gap-2">
                    <Badge variant={isConnected ? 'default' : 'destructive'} className="gap-1">
                        <Wifi className="h-3 w-3" />
                        {isConnected ? 'Connected' : 'Disconnected'}
                    </Badge>
                </div>
            </div>

            {/* Controls */}
            <Card>
                <CardContent className="py-3">
                    <div className="flex items-center gap-4">
                        {/* Pause/Resume */}
                        {isPaused ? (
                            <Button onClick={handleResume} variant="default" className="gap-2">
                                <Play className="h-4 w-4" />
                                Resume
                                {pausedMessagesRef.current.length > 0 && (
                                    <Badge variant="secondary" className="ml-1">
                                        +{pausedMessagesRef.current.length}
                                    </Badge>
                                )}
                            </Button>
                        ) : (
                            <Button onClick={() => setIsPaused(true)} variant="outline" className="gap-2">
                                <Pause className="h-4 w-4" />
                                Pause
                            </Button>
                        )}

                        {/* Clear */}
                        <Button onClick={handleClear} variant="outline" className="gap-2">
                            <Trash2 className="h-4 w-4" />
                            Clear
                        </Button>

                        {/* Auto-scroll */}
                        <Button
                            onClick={() => setAutoScroll(!autoScroll)}
                            variant={autoScroll ? 'default' : 'outline'}
                            className="gap-2"
                        >
                            <ArrowDown className="h-4 w-4" />
                            Auto-scroll
                        </Button>

                        {/* Filter */}
                        <div className="flex-1 flex items-center gap-2 ml-4">
                            <Filter className="h-4 w-4 text-muted-foreground" />
                            <Input
                                placeholder="Filter by topic or tag ID..."
                                value={filter}
                                onChange={(e) => setFilter(e.target.value)}
                                className="max-w-sm"
                            />
                        </div>

                        {/* Stats */}
                        <div className="text-sm text-muted-foreground">
                            Showing {filteredMessages.length} / {messages.length}
                        </div>
                    </div>
                </CardContent>
            </Card>

            {/* Messages Table */}
            <Card className="flex-1 overflow-hidden">
                <CardHeader className="py-3">
                    <CardTitle className="text-base">Message Stream</CardTitle>
                    <CardDescription>
                        Latest MQTT messages (max {MAX_MESSAGES})
                    </CardDescription>
                </CardHeader>
                <CardContent className="p-0">
                    <div className="h-[calc(100vh-380px)] overflow-auto">
                        <Table>
                            <TableHeader className="sticky top-0 bg-white z-10">
                                <TableRow>
                                    <TableHead className="w-[120px]">Time</TableHead>
                                    <TableHead className="w-[80px]">Tag ID</TableHead>
                                    <TableHead>Topic</TableHead>
                                    <TableHead className="w-[150px] text-right">Value</TableHead>
                                    <TableHead className="w-[100px] text-center">Quality</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {filteredMessages.length === 0 ? (
                                    <TableRow>
                                        <TableCell colSpan={5} className="h-32 text-center text-muted-foreground">
                                            {isPaused ? 'Stream paused...' : 'Waiting for messages...'}
                                        </TableCell>
                                    </TableRow>
                                ) : (
                                    filteredMessages.map((msg) => (
                                        <TableRow key={msg.id} className="font-mono text-sm">
                                            <TableCell className="text-muted-foreground">
                                                {formatTime(msg.receivedAt)}
                                            </TableCell>
                                            <TableCell>
                                                <Badge variant="outline">{msg.tagId}</Badge>
                                            </TableCell>
                                            <TableCell className="text-xs truncate max-w-[400px]" title={msg.topic}>
                                                {msg.topic}
                                            </TableCell>
                                            <TableCell className="text-right font-semibold text-blue-600">
                                                {formatValue(msg.value)}
                                            </TableCell>
                                            <TableCell className="text-center">
                                                {getQualityBadge(msg.quality)}
                                            </TableCell>
                                        </TableRow>
                                    ))
                                )}
                            </TableBody>
                        </Table>
                        <div ref={tableEndRef} />
                    </div>
                </CardContent>
            </Card>
        </div>
    );
};

export default MqttMonitorPage;
