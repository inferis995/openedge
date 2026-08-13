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
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue
} from '@/components/ui/select';
import {
    Play,
    Pause,
    Trash2,
    Radio,
    Filter,
    ArrowDown,
    Wifi,
    Zap
} from 'lucide-react';
import mqtt, { type MqttClient } from 'mqtt';
import { decodeSparkplugB, isProtobufData, convertSparkplugQuality } from '@/utils/sparkplugDecoder';

type MessageFormat = 'all' | 'legacy' | 'sparkplug';

interface MqttMessage {
    id: string;
    topic: string;
    tagId: number;
    tagAlias: string;
    value: number | boolean | string;
    timestamp: number;
    quality: number;
    receivedAt: Date;
    format: 'legacy' | 'sparkplug';
}

const MAX_MESSAGES = 200; // Reduced from 500
const BATCH_INTERVAL = 100; // Batch updates every 100ms

const MqttMonitorPage = () => {
    const [messages, setMessages] = useState<MqttMessage[]>([]);
    const [isConnected, setIsConnected] = useState(false);
    const [isPaused, setIsPaused] = useState(false);
    const [filter, setFilter] = useState('');
    const [formatFilter, setFormatFilter] = useState<MessageFormat>('all');
    const [autoScroll, setAutoScroll] = useState(true);
    const [stats, setStats] = useState({ total: 0, legacy: 0, sparkplug: 0 });

    const clientRef = useRef<MqttClient | null>(null);
    const tableEndRef = useRef<HTMLDivElement>(null);
    const isPausedRef = useRef(false);
    const isMountedRef = useRef(true);
    const pendingMessagesRef = useRef<MqttMessage[]>([]);
    const batchTimeoutRef = useRef<NodeJS.Timeout | null>(null);

    // Keep ref in sync with state
    useEffect(() => {
        isPausedRef.current = isPaused;
    }, [isPaused]);

    // Batch update function - processes pending messages in batches
    const flushPendingMessages = useCallback(() => {
        if (!isMountedRef.current) return;

        const pending = pendingMessagesRef.current;
        if (pending.length === 0) return;

        pendingMessagesRef.current = [];

        setMessages(prev => {
            const newMessages = [...prev, ...pending];
            if (newMessages.length > MAX_MESSAGES) {
                return newMessages.slice(-MAX_MESSAGES);
            }
            return newMessages;
        });
    }, []);

    // Schedule batch update
    const scheduleBatch = useCallback(() => {
        if (batchTimeoutRef.current) return;

        batchTimeoutRef.current = setTimeout(() => {
            batchTimeoutRef.current = null;
            flushPendingMessages();
        }, BATCH_INTERVAL);
    }, [flushPendingMessages]);

    // Add message to pending batch
    const addMessage = useCallback((msg: MqttMessage, format: 'legacy' | 'sparkplug') => {
        if (!isMountedRef.current) return;

        setStats(prev => ({
            total: prev.total + 1,
            legacy: prev.legacy + (format === 'legacy' ? 1 : 0),
            sparkplug: prev.sparkplug + (format === 'sparkplug' ? 1 : 0)
        }));

        if (isPausedRef.current) return;

        pendingMessagesRef.current.push(msg);
        scheduleBatch();
    }, [scheduleBatch]);

    // Extract alias from topic path
    const extractAliasFromTopic = (topic: string): string => {
        const parts = topic.split('/');
        return parts[parts.length - 1] || topic;
    };

    // Connect to MQTT broker directly
    useEffect(() => {
        isMountedRef.current = true;

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/mqtt`;

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
            if (!isMountedRef.current) return;
            setIsConnected(true);
            console.log('[MQTT Monitor] Connected to MQTT broker');

            // Subscribe to legacy data topics
            client.subscribe('data/#', { qos: 0 }, (err) => {
                if (err) {
                    console.error('[MQTT Monitor] Subscribe error (data/#):', err);
                } else {
                    console.log('[MQTT Monitor] Subscribed to data/# (Legacy)');
                }
            });

            // Subscribe to Sparkplug B topics
            client.subscribe('spBv1.0/#', { qos: 0 }, (err) => {
                if (err) {
                    console.error('[MQTT Monitor] Subscribe error (spBv1.0/#):', err);
                } else {
                    console.log('[MQTT Monitor] Subscribed to spBv1.0/# (Sparkplug B)');
                }
            });
        });

        client.on('message', (topic: string, payload: Buffer) => {
            if (!isMountedRef.current) return;

            try {
                // Determine format based on topic
                const isSparkplug = topic.startsWith('spBv1.0/');

                if (isSparkplug) {
                    // Try to decode as Protobuf first
                    if (isProtobufData(payload)) {
                        const decoded = decodeSparkplugB(payload);
                        if (decoded && decoded.metrics.length > 0) {
                            const topicParts = topic.split('/');
                            const msgType = topicParts[2] || '';
                            const nodeId = topicParts[3] || '';
                            const deviceId = topicParts[4] || '';

                            // Only show ONE message per payload (not one per metric)
                            let tagAlias: string;
                            let value: number | boolean | string;
                            let quality: number;

                            if (msgType === 'DBIRTH' || msgType === 'NBIRTH') {
                                tagAlias = `[${msgType}] ${deviceId || nodeId}`;
                                value = `${decoded.metrics.length} metrics`;
                                quality = 0;
                            } else if (msgType === 'DDEATH' || msgType === 'NDEATH') {
                                tagAlias = `[${msgType}] ${deviceId || nodeId}`;
                                value = 'offline';
                                quality = 2;
                            } else {
                                // DDATA / NDATA — just show first metric as sample
                                const metric = decoded.metrics[0];
                                tagAlias = metric.name || deviceId || extractAliasFromTopic(topic);
                                value = metric.value ?? '-';
                                quality = convertSparkplugQuality(metric.quality);
                            }

                            const msg: MqttMessage = {
                                id: `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
                                topic: topic,
                                tagId: 0,
                                tagAlias,
                                value,
                                timestamp: decoded.timestamp || Date.now(),
                                quality,
                                receivedAt: new Date(),
                                format: 'sparkplug'
                            };

                            addMessage(msg, 'sparkplug');
                            return;
                        }
                    }

                    // Fallback: try JSON parse
                    try {
                        const data = JSON.parse(payload.toString());
                        const topicParts = topic.split('/');
                        const msgType = topicParts[2] || '';
                        const nodeId = topicParts[3] || '';
                        const deviceId = topicParts[4] || '';
                        const metrics = data.Metrics || data.metrics || [];
                        const metricCount = Array.isArray(metrics) ? metrics.length : 1;

                        let tagAlias: string;
                        let value: number | boolean | string;
                        let quality: number;

                        if (msgType === 'DBIRTH' || msgType === 'NBIRTH') {
                            tagAlias = `[${msgType}] ${deviceId || nodeId}`;
                            value = `${metricCount} metric${metricCount !== 1 ? 's' : ''}`;
                            quality = 0;
                        } else if (msgType === 'DDEATH' || msgType === 'NDEATH') {
                            tagAlias = `[${msgType}] ${deviceId || nodeId}`;
                            value = 'offline';
                            quality = 2;
                        } else {
                            const metric = Array.isArray(metrics) ? metrics[0] : metrics;
                            const sparkplugQuality = metric?.Quality ?? metric?.quality ?? 192;
                            tagAlias = metric?.Name || metric?.name || extractAliasFromTopic(topic);
                            value = metric?.Value ?? metric?.value ?? '-';
                            quality = convertSparkplugQuality(sparkplugQuality);
                        }

                        const msg: MqttMessage = {
                            id: `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
                            topic: topic,
                            tagId: 0,
                            tagAlias,
                            value,
                            timestamp: data.Timestamp || data.timestamp || Date.now(),
                            quality,
                            receivedAt: new Date(),
                            format: 'sparkplug'
                        };

                        addMessage(msg, 'sparkplug');
                        return;
                    } catch {
                        // Not JSON, show as binary
                        const msg: MqttMessage = {
                            id: `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
                            topic: topic,
                            tagId: 0,
                            tagAlias: `[BINARY] ${extractAliasFromTopic(topic)}`,
                            value: `<${payload.length} bytes>`,
                            timestamp: Date.now(),
                            quality: 0,
                            receivedAt: new Date(),
                            format: 'sparkplug'
                        };

                        addMessage(msg, 'sparkplug');
                        return;
                    }
                }

                // Legacy format (JSON)
                const message = payload.toString();
                let data: any;

                try {
                    data = JSON.parse(message);
                } catch {
                    data = { v: message, ts: Date.now(), q: 0 };
                }

                const msg: MqttMessage = {
                    id: `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
                    topic: topic,
                    tagId: data.tag_id || 0,
                    tagAlias: extractAliasFromTopic(topic),
                    value: data.v ?? data.value ?? '-',
                    timestamp: data.ts || data.timestamp || Date.now(),
                    quality: data.q ?? data.quality ?? 0,
                    receivedAt: new Date(),
                    format: 'legacy'
                };

                addMessage(msg, 'legacy');
            } catch (e) {
                console.error('[MQTT Monitor] Parse error:', e);
            }
        });

        client.on('error', (err) => {
            console.error('[MQTT Monitor] Error:', err);
        });

        client.on('close', () => {
            if (!isMountedRef.current) return;
            setIsConnected(false);
            console.log('[MQTT Monitor] Disconnected');
        });

        client.on('reconnect', () => {
            console.log('[MQTT Monitor] Reconnecting...');
        });

        return () => {
            console.log('[MQTT Monitor] Cleanup - closing connection');
            isMountedRef.current = false;

            // Clear any pending batch timeout
            if (batchTimeoutRef.current) {
                clearTimeout(batchTimeoutRef.current);
                batchTimeoutRef.current = null;
            }

            // Force flush any remaining messages
            pendingMessagesRef.current = [];

            // End MQTT connection immediately (not gracefully to avoid blocking)
            if (client) {
                client.removeAllListeners();
                client.end(true); // Force close immediately
            }
        };
    }, [addMessage]);

    // Auto-scroll to bottom
    useEffect(() => {
        if (autoScroll && tableEndRef.current) {
            tableEndRef.current.scrollIntoView({ behavior: 'smooth' });
        }
    }, [messages, autoScroll]);

    // Resume: add paused messages
    const handleResume = useCallback(() => {
        setIsPaused(false);
        // Clear any pending since we were paused
        pendingMessagesRef.current = [];
    }, []);

    const handleClear = () => {
        setMessages([]);
        pendingMessagesRef.current = [];
        setStats({ total: 0, legacy: 0, sparkplug: 0 });
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

    const getFormatBadge = (format: 'legacy' | 'sparkplug') => {
        if (format === 'sparkplug') {
            return <Badge className="bg-[#c8e600] text-black gap-1"><Zap className="h-3 w-3" />SPB</Badge>;
        }
        return <Badge className="bg-muted text-muted-foreground">LEG</Badge>;
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

    // Filter messages by format and text
    const filteredMessages = messages.filter(m => {
        // Format filter
        if (formatFilter === 'legacy' && m.format !== 'legacy') return false;
        if (formatFilter === 'sparkplug' && m.format !== 'sparkplug') return false;

        // Text filter
        if (filter) {
            return m.topic.toLowerCase().includes(filter.toLowerCase()) ||
                m.tagAlias.toLowerCase().includes(filter.toLowerCase()) ||
                m.tagId.toString().includes(filter);
        }
        return true;
    });

    return (
        <div className="space-y-4 h-full flex flex-col">
            {/* Header */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex items-center gap-3">
                    <Radio className={`h-6 w-6 ${isConnected ? 'text-green-500 animate-pulse' : 'text-red-500'}`} />
                    <div>
                        <h2 className="text-2xl font-bold tracking-tight">MQTT Live Monitor</h2>
                        <p className="text-muted-foreground text-sm">
                            Real-time message stream • {stats.total.toLocaleString()} total
                            <span className="ml-2 text-slate-500">(Legacy: {stats.legacy}, Sparkplug: {stats.sparkplug})</span>
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

                        {/* Format Filter */}
                        <div className="flex items-center gap-2">
                            <span className="text-sm text-muted-foreground">Format:</span>
                            <Select value={formatFilter} onValueChange={(v) => setFormatFilter(v as MessageFormat)}>
                                <SelectTrigger className="w-[140px]">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="all">All Messages</SelectItem>
                                    <SelectItem value="legacy">Legacy (data/#)</SelectItem>
                                    <SelectItem value="sparkplug">Sparkplug B</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>

                        {/* Filter */}
                        <div className="flex-1 flex items-center gap-2 ml-4">
                            <Filter className="h-4 w-4 text-muted-foreground" />
                            <Input
                                placeholder="Filter by topic or alias..."
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
                        Latest MQTT messages (max {MAX_MESSAGES}) • Topics: data/# (Legacy) + spBv1.0/# (Sparkplug B)
                    </CardDescription>
                </CardHeader>
                <CardContent className="p-0">
                    <div className="h-[calc(100vh-380px)] overflow-auto">
                        <Table>
                            <TableHeader className="sticky top-0 bg-background z-10">
                                <TableRow>
                                    <TableHead className="w-[100px]">Time</TableHead>
                                    <TableHead className="w-[70px]">Format</TableHead>
                                    <TableHead className="w-[80px]">Tag ID</TableHead>
                                    <TableHead>Topic / Alias</TableHead>
                                    <TableHead className="w-[120px] text-right">Value</TableHead>
                                    <TableHead className="w-[100px] text-center">Quality</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {filteredMessages.length === 0 ? (
                                    <TableRow>
                                        <TableCell colSpan={6} className="h-32 text-center text-muted-foreground">
                                            {isPaused ? 'Stream paused...' : 'Waiting for messages...'}
                                        </TableCell>
                                    </TableRow>
                                ) : (
                                    filteredMessages.map((msg) => {
                                        const isLifecycle = msg.format === 'sparkplug' &&
                                            (msg.tagAlias.startsWith('[DBIRTH]') ||
                                                msg.tagAlias.startsWith('[NBIRTH]') ||
                                                msg.tagAlias.startsWith('[DDEATH]') ||
                                                msg.tagAlias.startsWith('[NDEATH]'));
                                        const isDeath = isLifecycle && (msg.tagAlias.includes('DEATH'));

                                        return (
                                            <TableRow
                                                key={msg.id}
                                                className={`font-mono text-sm ${isLifecycle
                                                    ? isDeath
                                                        ? 'bg-red-500/10 italic'
                                                        : 'bg-green-500/10 italic'
                                                    : msg.format === 'sparkplug'
                                                        ? 'bg-[#c8e600]/5'
                                                        : ''
                                                    }`}
                                            >
                                                <TableCell className="text-muted-foreground">
                                                    {formatTime(msg.receivedAt)}
                                                </TableCell>
                                                <TableCell>
                                                    {getFormatBadge(msg.format)}
                                                </TableCell>
                                                <TableCell>
                                                    {msg.tagId > 0 ? (
                                                        <Badge variant="outline">{msg.tagId}</Badge>
                                                    ) : (
                                                        <span className="text-muted-foreground">-</span>
                                                    )}
                                                </TableCell>
                                                <TableCell className="text-xs">
                                                    <div className="truncate max-w-[350px]" title={msg.topic}>
                                                        {isLifecycle ? (
                                                            <span>
                                                                <span className={`font-semibold ${isDeath ? 'text-red-600' : 'text-green-600'}`}>
                                                                    {msg.tagAlias}
                                                                </span>
                                                                <span className="text-muted-foreground ml-1 text-[10px]">({msg.topic})</span>
                                                            </span>
                                                        ) : msg.tagAlias && msg.tagAlias !== msg.topic.split('/').pop() ? (
                                                            <span>
                                                                <span className="font-medium text-[#4a5500] dark:text-[#c8e600]">{msg.tagAlias}</span>
                                                                <span className="text-muted-foreground ml-1">({msg.topic})</span>
                                                            </span>
                                                        ) : (
                                                            <span>{msg.topic}</span>
                                                        )}
                                                    </div>
                                                </TableCell>
                                                <TableCell className={`text-right font-semibold ${isLifecycle ? (isDeath ? 'text-red-500' : 'text-green-500') : 'text-foreground'}`}>
                                                    {formatValue(msg.value)}
                                                </TableCell>
                                                <TableCell className="text-center">
                                                    {getQualityBadge(msg.quality)}
                                                </TableCell>
                                            </TableRow>
                                        );
                                    })
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
