import { useState, useMemo, useEffect, useRef, useCallback } from 'react';
import { useQueries } from '@tanstack/react-query';
import { tagsApi } from '@/api/tags';
import { historyApi } from '@/api/history';
import { toast } from 'sonner';
import mqtt, { type MqttClient } from 'mqtt';

import { TrendToolbar } from '@/components/trend/TrendToolbar';
import { TagBrowser } from '@/components/trend/TagBrowser';
import { TrendDashboard } from '@/components/trend/TrendDashboard';
import { TrendDataTable } from '@/components/trend/TrendDataTable';
import { useTrendStore } from '@/stores/useTrendStore';
import {
    TagWithHierarchy,
    TrendDataPoint,
    calculateTimeRange,
    getAutoInterval,
} from '@/types/trend';

interface RealtimeValue {
    tagId: number;
    value: number;
    timestamp: number;
    quality: number;
}

const TrendPage = () => {
    const {
        charts,
        timeRange,
        liveMode,
        aggregation,
        sidebarOpen,
        dataTableOpen,
        dataTableHeight,
        addChart,
    } = useTrendStore();

    const [refreshTrigger, setRefreshTrigger] = useState(0);
    const [realtimeValues, setRealtimeValues] = useState<Map<number, RealtimeValue>>(new Map());
    const [isConnected, setIsConnected] = useState(false);
    const [sidebarWidth, setSidebarWidth] = useState(288);
    const mqttClientRef = useRef<MqttClient | null>(null);
    const timerRef = useRef<number | null>(null);

    // Drag-to-resize sidebar
    const handleSidebarResizeStart = useCallback((e: React.MouseEvent) => {
        e.preventDefault();
        const startX = e.clientX;
        const startWidth = sidebarWidth;

        const onMove = (ev: MouseEvent) => {
            const newWidth = Math.min(Math.max(startWidth + ev.clientX - startX, 180), 540);
            setSidebarWidth(newWidth);
        };
        const onUp = () => {
            document.removeEventListener('mousemove', onMove);
            document.removeEventListener('mouseup', onUp);
            document.body.style.cursor = '';
            document.body.style.userSelect = '';
        };
        document.body.style.cursor = 'col-resize';
        document.body.style.userSelect = 'none';
        document.addEventListener('mousemove', onMove);
        document.addEventListener('mouseup', onUp);
    }, [sidebarWidth]);

    // Calculate actual date range.
    // refreshTrigger is included so live mode shifts the window forward every 10 s.
    const { start: startDate, end: endDate } = useMemo(() => {
        if (timeRange.preset === 'custom' && timeRange.customStart && timeRange.customEnd) {
            return { start: timeRange.customStart, end: timeRange.customEnd };
        }
        return calculateTimeRange(timeRange.preset);
    }, [timeRange, refreshTrigger]);

    // Calculate interval
    const interval = useMemo(() => {
        return getAutoInterval(startDate, endDate);
    }, [startDate, endDate]);

    // Fetch all tags with hierarchy
    const tagsQuery = useQueries({
        queries: [
            {
                queryKey: ['tagsWithHierarchy'],
                queryFn: (): Promise<TagWithHierarchy[]> => tagsApi.getAllWithHierarchy(),
                staleTime: 60000,
            },
        ],
    });

    const tags = useMemo(() => {
        return tagsQuery[0].data || [];
    }, [tagsQuery]);

    // Get all tag IDs from all charts
    const allTagIds = useMemo(() => {
        const ids = new Set<number>();
        charts.forEach(chart => {
            chart.tagIds.forEach(id => ids.add(id));
        });
        return Array.from(ids);
    }, [charts]);

    // Fetch historical data
    const historyQueries = useQueries({
        queries: allTagIds.map(tagId => ({
            queryKey: ['history', tagId, startDate.toISOString(), endDate.toISOString(), aggregation, interval, refreshTrigger],
            queryFn: async () => {
                const response = await historyApi.query({
                    tag_id: tagId,
                    start: startDate.toISOString(),
                    end: endDate.toISOString(),
                    agg: aggregation,
                    interval,
                });
                return { tagId, data: response };
            },
            enabled: allTagIds.includes(tagId),
            staleTime: 5000,
        })),
    });

    // Build history data map
    const historyDataMap = useMemo(() => {
        const map = new Map<number, TrendDataPoint[]>();
        historyQueries.forEach(query => {
            if (query.data) {
                map.set(query.data.tagId, query.data.data);
            }
        });
        return map;
    }, [historyQueries]);

    // Current values queries
    const currentQueries = useQueries({
        queries: allTagIds.map(tagId => ({
            queryKey: ['current', tagId],
            queryFn: () => tagsApi.getCurrentValue(tagId),
            refetchInterval: liveMode ? 3000 : 0,
            staleTime: 2000,
            enabled: allTagIds.includes(tagId),
        })),
    });

    // Process current query results
    useEffect(() => {
        currentQueries.forEach((query, index) => {
            if (query.data && query.isSuccess) {
                const tagId = allTagIds[index];
                const ts = query.data.timestamp || Date.now();

                setRealtimeValues(prev => {
                    const existing = prev.get(tagId);
                    if (existing && existing.timestamp >= ts) {
                        return prev;
                    }
                    const next = new Map(prev);
                    next.set(tagId, {
                        tagId,
                        value: typeof query.data!.value === 'boolean'
                            ? (query.data!.value ? 1 : 0)
                            : Number(query.data!.value) || 0,
                        timestamp: ts,
                        quality: query.data!.quality ?? 0,
                    });
                    return next;
                });
            }
        });
    }, [currentQueries.reduce((acc, q) => acc + (q.dataUpdatedAt || 0), 0), allTagIds.join(',')]);

    // MQTT subscription for real-time updates
    useEffect(() => {
        if (!liveMode || allTagIds.length === 0) {
            if (mqttClientRef.current) {
                mqttClientRef.current.end();
                mqttClientRef.current = null;
            }
            setIsConnected(false);
            return;
        }

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/mqtt`;

        const client = mqtt.connect(wsUrl, {
            clientId: `trend-${Date.now()}`,
            clean: true,
            keepalive: 60,
            reconnectPeriod: 5000,
        });

        client.on('connect', () => {
            client.subscribe('data/#');
            setIsConnected(true);
        });

        client.on('disconnect', () => setIsConnected(false));
        client.on('offline', () => setIsConnected(false));

        client.on('message', (topic, payload) => {
            try {
                const data = JSON.parse(payload.toString());
                const parts = topic.split('/');
                const alias = parts[parts.length - 1];

                const tag = tags.find(t => {
                    const tagAlias = t.alias?.replace(/_/g, '-').toLowerCase();
                    return tagAlias === alias?.toLowerCase() ||
                        t.alias?.toLowerCase() === alias?.toLowerCase();
                });

                if (tag && allTagIds.includes(tag.id)) {
                    let numValue: number;
                    if (typeof data.v === 'boolean') {
                        numValue = data.v ? 1 : 0;
                    } else if (typeof data.v === 'number') {
                        numValue = data.v;
                    } else {
                        numValue = parseFloat(data.v) || 0;
                    }

                    const ts = data.ts || Date.now();

                    setRealtimeValues(prev => {
                        const next = new Map(prev);
                        next.set(tag.id, {
                            tagId: tag.id,
                            value: numValue,
                            timestamp: ts,
                            quality: data.q ?? 0,
                        });
                        return next;
                    });
                }
            } catch (e) { }
        });

        mqttClientRef.current = client;

        return () => {
            client.end();
            setIsConnected(false);
        };
    }, [liveMode, allTagIds, tags]);

    // Auto-refresh timer
    useEffect(() => {
        if (!liveMode || timeRange.preset === 'custom' || timeRange.preset === 'yesterday') {
            if (timerRef.current) {
                window.clearInterval(timerRef.current);
                timerRef.current = null;
            }
            return;
        }

        timerRef.current = window.setInterval(() => {
            setRefreshTrigger(prev => prev + 1);
        }, 10000);

        return () => {
            if (timerRef.current) {
                window.clearInterval(timerRef.current);
            }
        };
    }, [liveMode, timeRange.preset]);

    // Handlers
    const handleRefresh = useCallback(() => {
        setRefreshTrigger(prev => prev + 1);
    }, []);

    const handleExportCSV = useCallback(() => {
        if (historyDataMap.size === 0 || allTagIds.length === 0) {
            toast.error('No data to export');
            return;
        }

        // Build CSV from all data
        const timestampMap = new Map<number, any>();

        historyDataMap.forEach((data, tagId) => {
            const tag = tags.find(t => t.id === tagId);
            const key = tag?.alias || tag?.code || `Tag_${tagId}`;

            data.forEach(point => {
                const ts = point.timestamp * 1000;
                if (!timestampMap.has(ts)) {
                    const date = new Date(ts);
                    timestampMap.set(ts, {
                        timestamp: ts,
                        date: date.toLocaleDateString('it-IT'),
                        time: date.toLocaleTimeString('it-IT'),
                    });
                }
                const entry = timestampMap.get(ts);
                entry[key] = point.quality === 0 ? point.value : null;
            });
        });

        // Add realtime values
        realtimeValues.forEach((rv, tagId) => {
            if (!allTagIds.includes(tagId)) return;
            const tag = tags.find(t => t.id === tagId);
            const key = tag?.alias || tag?.code;

            if (rv.timestamp >= startDate.getTime() && rv.timestamp <= endDate.getTime()) {
                if (!timestampMap.has(rv.timestamp)) {
                    const date = new Date(rv.timestamp);
                    timestampMap.set(rv.timestamp, {
                        timestamp: rv.timestamp,
                        date: date.toLocaleDateString('it-IT'),
                        time: date.toLocaleTimeString('it-IT'),
                    });
                }
                const entry = timestampMap.get(rv.timestamp);
                if (entry && key) {
                    entry[key] = rv.quality === 0 ? rv.value : null;
                }
            }
        });

        const sortedData = Array.from(timestampMap.values()).sort((a, b) => a.timestamp - b.timestamp);

        const selectedTags = allTagIds.map(id => tags.find(t => t.id === id)).filter(Boolean);
        const headers = ['Timestamp', 'Date', 'Time', ...selectedTags.map(t => t!.alias || t!.code)];
        const rows = sortedData.map(row => {
            const values = selectedTags.map(tag => {
                const key = tag!.alias || tag!.code;
                return row[key] ?? '';
            });
            return [
                new Date(row.timestamp).toISOString(),
                row.date,
                row.time,
                ...values,
            ];
        });

        const csvContent = [headers.join(','), ...rows.map(r => r.join(','))].join('\n');
        const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `trend_export_${new Date().toISOString().split('T')[0]}.csv`;
        link.click();
        URL.revokeObjectURL(url);
        toast.success('CSV exported successfully');
    }, [historyDataMap, allTagIds, tags, realtimeValues, startDate, endDate]);

    const handleAddChart = useCallback(() => {
        addChart();
    }, [addChart]);

    const handleAddTagToChart = useCallback((_tagId: number) => {
        // Tag is added by the store's addTagToChart which is called from TagBrowser
    }, []);

    // Merge realtime values into historyDataMap so the data table always shows
    // the current live state even before the historian has written it to InfluxDB.
    // Realtime timestamps are in ms; TrendDataPoint.timestamp is in seconds.
    const mergedDataMap = useMemo(() => {
        const merged = new Map<number, TrendDataPoint[]>(historyDataMap);
        realtimeValues.forEach((rv, tagId) => {
            if (!allTagIds.includes(tagId)) return;
            const existing = merged.get(tagId) || [];
            const rvTsSec = Math.floor(rv.timestamp / 1000);
            const lastTsSec = existing.length > 0 ? existing[existing.length - 1].timestamp : 0;
            // Only append if the realtime point is newer than the last historical point
            if (rvTsSec > lastTsSec) {
                merged.set(tagId, [...existing, {
                    timestamp: rvTsSec,
                    value: rv.value,
                    quality: rv.quality,
                }]);
            }
        });
        return merged;
    }, [historyDataMap, realtimeValues, allTagIds]);

    // Calculate stats
    const totalDataPoints = useMemo(() => {
        let count = 0;
        historyDataMap.forEach(data => {
            count += data.length;
        });
        return count;
    }, [historyDataMap]);

    const isLoading = historyQueries.some(q => q.isLoading);

    return (
        <div className="h-[calc(100vh-5rem)] flex flex-col bg-gray-50">
            {/* Toolbar */}
            <TrendToolbar
                onRefresh={handleRefresh}
                onExportCSV={handleExportCSV}
                onAddChart={handleAddChart}
                isLoading={isLoading}
                dataPointsCount={totalDataPoints}
                tagsCount={allTagIds.length}
                isMqttConnected={isConnected}
            />

            {/* Main Content */}
            <div className="flex-1 flex overflow-hidden">
                {/* Sidebar - Tag Browser */}
                {sidebarOpen && (
                    <>
                        <div style={{ width: sidebarWidth }} className="flex-shrink-0 overflow-hidden">
                            <TagBrowser
                                onAddTagToChart={handleAddTagToChart}
                                selectedTagIds={allTagIds}
                                realtimeValues={realtimeValues}
                            />
                        </div>
                        {/* Resize handle */}
                        <div
                            className="w-1.5 flex-shrink-0 hover:bg-blue-400 bg-gray-200 cursor-col-resize transition-colors"
                            onMouseDown={handleSidebarResizeStart}
                            title="Trascina per ridimensionare"
                        />
                    </>
                )}

                {/* Chart Area */}
                <div className="flex-1 flex flex-col overflow-hidden">
                    {/* Dashboard */}
                    <div className="flex-1 p-3 overflow-y-auto">
                        <TrendDashboard
                            tags={tags}
                            timeRange={{ start: startDate, end: endDate }}
                            aggregation={aggregation}
                            realtimeValues={realtimeValues}
                            refreshTrigger={refreshTrigger}
                        />
                    </div>

                    {/* Data Table */}
                    {dataTableOpen && (
                        <TrendDataTable
                            data={mergedDataMap}
                            tags={tags}
                            selectedTagIds={allTagIds}
                            onClose={() => useTrendStore.getState().toggleDataTable()}
                            height={dataTableHeight}
                        />
                    )}
                </div>
            </div>
        </div>
    );
};

export default TrendPage;
