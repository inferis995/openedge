import { useState, useMemo } from 'react';
import { useQueries } from '@tanstack/react-query';
import { historyApi } from '@/api/history';
import {
    TrendingUp,
    Download,
    Filter,
    RefreshCw,
    Loader2,
    X
} from "lucide-react";
import {
    LineChart,
    Line,
    XAxis,
    YAxis,
    CartesianGrid,
    Tooltip,
    Legend,
    ResponsiveContainer
} from 'recharts';
import { TagSearch } from '@/components/TagSearch';
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue
} from "@/components/ui/select";
// Removed unused imports: Input, Badge, Calendar
import { Tag, HistoryDataPoint } from '@/types';
import { useTags } from '@/hooks/useTags';
import { toast } from 'sonner';

// Color palette for multiple tags
const TAG_COLORS = [
    '#7c3aed', // violet-600
    '#db2777', // pink-600
    '#059669', // emerald-600
    '#2563eb', // blue-600
    '#d97706', // amber-600
];

const TrendPage = () => {
    // State for selected tags
    const [selectedTagIds, setSelectedTagIds] = useState<number[]>([]);

    // State for date range (default last 24 hours)
    const [startDate, setStartDate] = useState<Date>(new Date(Date.now() - 24 * 60 * 60 * 1000));
    const [endDate, setEndDate] = useState<Date>(new Date());

    // State for aggregation controls
    const [aggregation, setAggregation] = useState<'mean' | 'max' | 'min' | 'sum'>('mean');
    const [interval, setInterval] = useState('1m');

    // State for query trigger
    const [shouldQuery, setShouldQuery] = useState(false);
    const [refreshTrigger, setRefreshTrigger] = useState(0);

    // Fetch all tags (without gatewayId filter to get all tags)
    const { tags } = useTags(null);
    const safeTags = useMemo(() => Array.isArray(tags) ? tags : [], [tags]);

    // Fetch historical data for selected tags
    // Fetch historical data for selected tags using useQueries to avoid hook rules violation
    const historyQueries = useQueries({
        queries: selectedTagIds.map(tagId => {
            const params = {
                tag_id: tagId,
                start: startDate.toISOString(),
                end: endDate.toISOString(),
                agg: aggregation,
                interval: interval,
            };
            return {
                queryKey: ['history', params, refreshTrigger],
                queryFn: () => historyApi.query(params),
                enabled: shouldQuery && selectedTagIds.includes(tagId),
            };
        })
    });

    const isLoadingHistory = historyQueries.some(q => q.isLoading);
    const hasError = historyQueries.some(q => q.isError);

    // Combine all history data
    const chartData = useMemo(() => {
        try {
            // Create a map of all timestamps
            const timestampMap = new Map<number, Record<string, string | number | null>>();

            historyQueries.forEach((query, index) => {
                const tagId = selectedTagIds[index];
                const tag = safeTags.find(t => t.id === tagId);
                const tagKey = tag?.alias || tag?.code || `Tag ${tagId}`;

                // Defensive check: ensure query.data is an array before iterating
                if (!query.data || !Array.isArray(query.data)) return;

                query.data.forEach((point: HistoryDataPoint) => {
                    if (!point) return;
                    const ts = point.timestamp * 1000; // Convert to milliseconds
                    if (!timestampMap.has(ts)) {
                        timestampMap.set(ts, { timestamp: ts, date: new Date(ts).toLocaleString() });
                    }
                    const entry = timestampMap.get(ts)!;
                    const key = `${tagKey}_value`;
                    const qualityKey = `${tagKey}_quality`;

                    entry[qualityKey] = point.quality;

                    if (point.quality === 2) {
                        entry[key] = null;
                        return;
                    }

                    if (typeof point.value === 'boolean') {
                        entry[key] = point.value ? 1 : 0;
                    } else if (typeof point.value === 'number') {
                        entry[key] = point.value;
                    } else if (typeof point.value === 'string') {
                        // Try to parse string
                        const strValue = point.value as string;
                        const lower = strValue.toLowerCase();
                        if (lower === 'true') entry[key] = 1;
                        else if (lower === 'false') entry[key] = 0;
                        else {
                            const num = parseFloat(strValue);
                            entry[key] = isNaN(num) ? 0 : num;
                        }
                    } else {
                        entry[key] = 0;
                    }
                });
            });

            // Convert map to array and sort by timestamp
            return Array.from(timestampMap.values()).sort((a, b) => (a.timestamp as number) - (b.timestamp as number));
        } catch (error) {
            console.error("Error calculating chart data", error);
            return [];
        }
    }, [historyQueries, selectedTagIds, safeTags]);

    // Get selected tag objects
    const selectedTags = useMemo(() => {
        return selectedTagIds.map(id => safeTags.find(t => t.id === id)).filter(Boolean) as Tag[];
    }, [selectedTagIds, safeTags]);

    // Handle tag selection
    const handleAddTag = (tagIdStr: string) => {
        const tagId = parseInt(tagIdStr);
        if (!selectedTagIds.includes(tagId)) {
            setSelectedTagIds([...selectedTagIds, tagId]);
            setShouldQuery(true); // Auto-enable query to show data immediately
        }
    };

    const handleRemoveTag = (tagId: number) => {
        setSelectedTagIds(selectedTagIds.filter(id => id !== tagId));
    };

    // Handle query
    const handleQuery = () => {
        if (selectedTagIds.length === 0) {
            toast.error('Please select at least one tag');
            return;
        }
        setShouldQuery(true);
        setRefreshTrigger(prev => prev + 1);
    };

    // Handle export CSV
    const handleExportCSV = () => {
        if (chartData.length === 0) {
            toast.error('No data to export');
            return;
        }

        // Create CSV header
        const headers = ['Timestamp', 'Date', ...selectedTags.map(tag => tag.alias || tag.code)];

        // Create CSV rows
        const rows = chartData.map(point => {
            const values = selectedTags.map(tag => {
                const key = tag.alias || tag.code;
                return point[`${key}_value`] ?? '';
            });
            return [point.timestamp, point.date, ...values];
        });

        // Combine header and rows
        const csvContent = [
            headers.join(','),
            ...rows.map(row => row.join(','))
        ].join('\n');

        // Create download link
        const blob = new Blob([csvContent], { type: 'text/csv' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `trend_data_${new Date().toISOString().split('T')[0]}.csv`;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(url);

        toast.success('CSV exported successfully');
    };

    // Quick date range selectors
    const setQuickRange = (label: string) => {
        const end = new Date();
        let start = new Date();

        switch (label) {
            case '1h': start = new Date(Date.now() - 1 * 60 * 60 * 1000); setInterval('1m'); break;
            case '6h': start = new Date(Date.now() - 6 * 60 * 60 * 1000); setInterval('5m'); break;
            case '24h': start = new Date(Date.now() - 24 * 60 * 60 * 1000); setInterval('15m'); break;
            case '7d': start = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000); setInterval('1h'); break;
            case '30d': start = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000); setInterval('1d'); break;
        }

        setStartDate(start);
        setEndDate(end);
    };

    // Helper for input value
    const formatInputValue = (date: Date) => {
        const tzOffset = date.getTimezoneOffset() * 60000;
        return new Date(date.getTime() - tzOffset).toISOString().slice(0, 16);
    };

    return (
        <div className="h-[calc(100vh-6rem)] flex flex-col gap-4">
            {/* Header */}
            <div className="flex items-center justify-between pb-2 border-b">
                <div>
                    <h1 className="text-2xl font-bold flex items-center gap-2">
                        <TrendingUp className="w-6 h-6 text-violet-600" />
                        Professional Trend Analysis
                    </h1>
                    <p className="text-sm text-muted-foreground">Real-time historical data visualization</p>
                </div>
                <div className="flex items-center gap-2">
                    <Button
                        onClick={handleQuery}
                        disabled={selectedTagIds.length === 0 || isLoadingHistory}
                        className={shouldQuery ? "bg-green-600 hover:bg-green-700" : "bg-violet-600 hover:bg-violet-700"}
                    >
                        {isLoadingHistory ? (
                            <>
                                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                                Loading...
                            </>
                        ) : (
                            <>
                                <RefreshCw className="w-4 h-4 mr-2" />
                                {shouldQuery ? "Refresh Data" : "Load Chart"}
                            </>
                        )}
                    </Button>
                    <Button
                        onClick={handleExportCSV}
                        disabled={chartData.length === 0}
                        variant="outline"
                        size="icon"
                        title="Export CSV"
                    >
                        <Download className="w-4 h-4" />
                    </Button>
                </div>
            </div>

            <div className="grid grid-cols-12 gap-6 h-full min-h-0">
                {/* Sidebar Controls */}
                <div className="col-span-12 md:col-span-3 flex flex-col gap-4 overflow-y-auto pr-2">
                    <Card>
                        <CardHeader className="py-3 px-4">
                            <CardTitle className="text-sm font-medium">Tag Selection</CardTitle>
                        </CardHeader>
                        <CardContent className="p-4 pt-0 space-y-4">
                            <div className="space-y-2">
                                <Label className="text-xs">Search & Add Tag</Label>
                                <TagSearch
                                    tags={safeTags}
                                    selectedTags={selectedTagIds}
                                    onSelect={(tag) => handleAddTag(tag.id.toString())}
                                />
                            </div>

                            <div className="space-y-2">
                                <Label className="text-xs">Active Traces ({selectedTags.length})</Label>
                                <div className="space-y-2 max-h-[300px] overflow-y-auto pr-1">
                                    {selectedTags.length === 0 && (
                                        <p className="text-xs text-muted-foreground italic">No tags selected</p>
                                    )}
                                    {selectedTags.map((tag, index) => (
                                        <div key={tag.id} className="flex items-center justify-between p-2 rounded-md bg-secondary/50 border text-sm group">
                                            <div className="flex items-center gap-2 overflow-hidden">
                                                <div
                                                    className="w-3 h-3 rounded-full shrink-0"
                                                    style={{ backgroundColor: TAG_COLORS[index % TAG_COLORS.length] }}
                                                />
                                                <div className="flex flex-col truncate">
                                                    <span className="font-medium truncate" title={tag.alias}>{tag.alias || tag.code}</span>
                                                    <span className="text-[10px] text-muted-foreground">{tag.code}</span>
                                                </div>
                                            </div>
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity"
                                                onClick={() => handleRemoveTag(tag.id)}
                                            >
                                                <X className="w-3 h-3" />
                                            </Button>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        </CardContent>
                    </Card>

                    <Card>
                        <CardHeader className="py-3 px-4">
                            <CardTitle className="text-sm font-medium">Time Range</CardTitle>
                        </CardHeader>
                        <CardContent className="p-4 pt-0 space-y-4">
                            <div className="grid grid-cols-2 gap-2">
                                {['1h', '6h', '12h', '24h', '7d', '30d'].map(preset => (
                                    <Button
                                        key={preset}
                                        variant="outline"
                                        size="sm"
                                        className="text-xs h-7"
                                        onClick={() => setQuickRange(preset)}
                                    >
                                        {preset}
                                    </Button>
                                ))}
                            </div>

                            <div className="space-y-2 border-t pt-2">
                                <div className="grid gap-1">
                                    <Label className="text-xs">Start</Label>
                                    <input
                                        type="datetime-local"
                                        value={formatInputValue(startDate)}
                                        onChange={(e) => setStartDate(new Date(e.target.value))}
                                        className="w-full text-xs px-2 py-1 border rounded"
                                    />
                                </div>
                                <div className="grid gap-1">
                                    <Label className="text-xs">End</Label>
                                    <input
                                        type="datetime-local"
                                        value={formatInputValue(endDate)}
                                        onChange={(e) => setEndDate(new Date(e.target.value))}
                                        className="w-full text-xs px-2 py-1 border rounded"
                                    />
                                </div>
                            </div>
                        </CardContent>
                    </Card>
                </div>

                {/* Main Content - Chart */}
                <div className="col-span-12 md:col-span-9 h-full min-h-[500px]">
                    <Card className="h-full flex flex-col shadow-sm">
                        <CardHeader className="py-3 px-6 border-b flex flex-row items-center justify-between">
                            <div className="space-y-0.5">
                                <CardTitle className="text-base">Trend View</CardTitle>
                                <CardDescription className="text-xs">
                                    {startDate.toLocaleString()} — {endDate.toLocaleString()}
                                </CardDescription>
                            </div>
                            <div className="flex items-center gap-2">
                                <Select value={aggregation} onValueChange={(value: any) => setAggregation(value)}>
                                    <SelectTrigger className="w-[110px] h-8 text-xs">
                                        <SelectValue placeholder="Agg" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="mean">Mean</SelectItem>
                                        <SelectItem value="max">Max</SelectItem>
                                        <SelectItem value="min">Min</SelectItem>
                                        <SelectItem value="sum">Sum</SelectItem>
                                    </SelectContent>
                                </Select>
                                <Select value={interval} onValueChange={setInterval}>
                                    <SelectTrigger className="w-[80px] h-8 text-xs">
                                        <SelectValue placeholder="Int" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="1m">1m</SelectItem>
                                        <SelectItem value="5m">5m</SelectItem>
                                        <SelectItem value="15m">15m</SelectItem>
                                        <SelectItem value="1h">1h</SelectItem>
                                        <SelectItem value="1d">1d</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>
                        </CardHeader>
                        <CardContent className="flex-1 p-2 min-h-0 relative">
                            {/* Error State */}
                            {hasError && (
                                <div className="absolute inset-0 z-10 flex items-center justify-center bg-white/80 backdrop-blur-sm">
                                    <div className="text-center text-red-600 p-4 border border-red-200 rounded bg-red-50 shadow-lg">
                                        <p className="font-bold">Error loading data</p>
                                        <p className="text-sm">Please check your query settings.</p>
                                    </div>
                                </div>
                            )}

                            {/* Loading State */}
                            {isLoadingHistory && (
                                <div className="absolute inset-0 z-10 flex items-center justify-center bg-white/50 backdrop-blur-sm">
                                    <div className="flex items-center gap-2 px-4 py-2 bg-white rounded-full shadow-lg border">
                                        <Loader2 className="w-4 h-4 animate-spin text-violet-600" />
                                        <span className="text-sm font-medium">Fetching historical data...</span>
                                    </div>
                                </div>
                            )}

                            {/* Chart or Empty State */}
                            {chartData.length > 0 ? (
                                <SafeChart>
                                    <ResponsiveContainer width="100%" height="100%">
                                        <LineChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 20 }}>
                                            <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" vertical={false} />
                                            <XAxis
                                                dataKey="timestamp"
                                                tick={{ fontSize: 10, fill: '#6b7280' }}
                                                tickFormatter={(value) => {
                                                    const date = new Date(value);
                                                    const duration = endDate.getTime() - startDate.getTime();
                                                    if (duration > 24 * 60 * 60 * 1000) {
                                                        return date.toLocaleDateString('it-IT', { month: 'short', day: 'numeric', hour: '2-digit' });
                                                    }
                                                    return date.toLocaleTimeString('it-IT', { hour: '2-digit', minute: '2-digit' });
                                                }}
                                                height={30}
                                            />
                                            <YAxis
                                                tick={{ fontSize: 10, fill: '#6b7280' }}
                                                tickFormatter={(value) => {
                                                    if (Math.abs(value) >= 1000000) return `${(value / 1000000).toFixed(1)}M`;
                                                    if (Math.abs(value) >= 1000) return `${(value / 1000).toFixed(1)}k`;
                                                    // Integers for counts/bools
                                                    if (Number.isInteger(value)) return value.toString();
                                                    return value.toFixed(1);
                                                }}
                                                width={40}
                                            />
                                            <Tooltip
                                                contentStyle={{
                                                    backgroundColor: 'rgba(255, 255, 255, 0.95)',
                                                    border: '1px solid #e2e8f0',
                                                    borderRadius: '6px',
                                                    boxShadow: '0 4px 6px -1px rgba(0, 0, 0, 0.1)',
                                                    fontSize: '12px',
                                                    padding: '8px 12px'
                                                }}
                                                labelFormatter={(value) => new Date(value).toLocaleString()}
                                            />
                                            <Legend
                                                verticalAlign="top"
                                                height={36}
                                                iconType="circle"
                                                iconSize={8}
                                                wrapperStyle={{ fontSize: '12px' }}
                                            />
                                            {selectedTags.map((tag, index) => {
                                                const key = tag.alias || tag.code;
                                                const color = TAG_COLORS[index % TAG_COLORS.length];
                                                const isBool = tag.data_type === 'BOOL';

                                                return (
                                                    <Line
                                                        key={tag.id}
                                                        type={isBool ? "stepAfter" : "monotone"}
                                                        dataKey={`${key}_value`}
                                                        name={tag.alias || tag.code}
                                                        stroke={color}
                                                        strokeWidth={isBool ? 2 : 1.5}
                                                        dot={false}
                                                        activeDot={{ r: 4, strokeWidth: 0 }}
                                                        connectNulls={false}
                                                        isAnimationActive={false}
                                                    />
                                                );
                                            })}
                                        </LineChart>
                                    </ResponsiveContainer>
                                </SafeChart>
                            ) : (
                                <div className="h-full flex flex-col items-center justify-center text-muted-foreground bg-slate-50/50 border-2 border-dashed border-slate-200 rounded-lg m-4">
                                    {selectedTagIds.length === 0 ? (
                                        <>
                                            <TrendingUp className="w-12 h-12 mb-4 text-slate-300" />
                                            <p className="font-medium">No Tags Selected</p>
                                            <p className="text-sm">Use the sidebar to search and add tags to the chart.</p>
                                        </>
                                    ) : (
                                        <>
                                            <Filter className="w-12 h-12 mb-4 text-slate-300" />
                                            <p className="font-medium">No Data Found</p>
                                            <p className="text-sm">Try adjusting the time range or check if data is being recorded.</p>
                                            {shouldQuery && !isLoadingHistory && (
                                                <Button size="sm" variant="outline" className="mt-4" onClick={handleQuery}>
                                                    <RefreshCw className="w-3 h-3 mr-2" /> Try Again
                                                </Button>
                                            )}
                                        </>
                                    )}
                                </div>
                            )}
                        </CardContent>
                    </Card>
                </div>
            </div>
        </div>
    );
};

// Error Boundary for Chart
import React from 'react';

class SafeChart extends React.Component<{ children: React.ReactNode }, { hasError: boolean }> {
    constructor(props: { children: React.ReactNode }) {
        super(props);
        this.state = { hasError: false };
    }

    static getDerivedStateFromError(_error: any) {
        return { hasError: true };
    }

    componentDidCatch(error: any, errorInfo: any) {
        console.error("Chart Error caught:", error, errorInfo);
    }

    render() {
        if (this.state.hasError) {
            return (
                <div className="flex items-center justify-center h-[400px] border border-red-200 bg-red-50 text-red-600 rounded-md p-4">
                    <div className="text-center">
                        <TrendingUp className="w-12 h-12 mx-auto mb-2 text-red-500 opacity-50" />
                        <p className="font-semibold">Chart Render Error</p>
                        <p className="text-sm mt-1">Unable to display graph. Data might be invalid.</p>
                    </div>
                </div>
            );
        }

        return this.props.children;
    }
}

export default TrendPage;
