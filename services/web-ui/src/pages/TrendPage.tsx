import { useState, useMemo } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Badge } from '@/components/ui/badge';
import { useTags } from '@/hooks/useTags';
import { useHistory } from '@/hooks/useHistory';
import { Tag, HistoryDataPoint } from '@/types';
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts';
import { TrendingUp, Download, Calendar, Filter, X, Loader2 } from 'lucide-react';
import { toast } from 'sonner';

// Color palette for multiple tags
const TAG_COLORS = [
    '#8b5cf6', // violet
    '#3b82f6', // blue
    '#10b981', // emerald
    '#f59e0b', // amber
    '#ef4444', // red
    '#ec4899', // pink
    '#06b6d4', // cyan
    '#84cc16', // lime
];

const TrendPage = () => {
    console.log('TrendPage v3 (Paranoid Mode) rendering...');
    // State for selected tags
    const [selectedTagIds, setSelectedTagIds] = useState<number[]>([]);

    // State for date range (default last 24 hours)
    const [endDate, setEndDate] = useState(new Date());
    const [startDate, setStartDate] = useState(new Date(Date.now() - 24 * 60 * 60 * 1000));

    // State for aggregation controls
    const [aggregation, setAggregation] = useState<'mean' | 'max' | 'min' | 'sum'>('mean');
    const [interval, setInterval] = useState('1m');

    // State for query trigger
    const [shouldQuery, setShouldQuery] = useState(false);

    // Fetch all tags (without gatewayId filter to get all tags)
    const { tags } = useTags(null);
    const safeTags = useMemo(() => Array.isArray(tags) ? tags : [], [tags]);

    // Fetch historical data for selected tags
    const historyQueries = selectedTagIds.map(tagId => {
        return useHistory({
            tag_id: tagId,
            start: startDate.toISOString(),
            end: endDate.toISOString(),
            agg: aggregation,
            interval: interval,
        }, shouldQuery && selectedTagIds.includes(tagId));
    });

    const isLoadingHistory = historyQueries.some(q => q.isLoading);
    const hasError = historyQueries.some(q => q.isError);

    // Combine all history data
    const chartData = useMemo(() => {
        try {
            // Create a map of all timestamps
            const timestampMap = new Map<number, Record<string, string | number>>();

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
                    entry[`${tagKey}_value`] = typeof point.value === 'number' ? point.value : 0;
                    entry[`${tagKey}_quality`] = point.quality;
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

    // Available tags (excluding already selected)
    const availableTags = safeTags.filter(tag => !selectedTagIds.includes(tag.id));

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-3xl font-bold flex items-center gap-2">
                        <TrendingUp className="w-8 h-8 text-violet-600" />
                        Trend / Historian
                    </h1>
                    <p className="text-gray-600 mt-1">View and compare historical tag data</p>
                </div>
            </div>

            {/* Controls Card */}
            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                        <Filter className="w-5 h-5" />
                        Query Controls
                    </CardTitle>
                    <CardDescription>Configure your historical data query</CardDescription>
                </CardHeader>
                <CardContent className="space-y-6">
                    {/* Tag Selection */}
                    <div className="space-y-2">
                        <Label htmlFor="tag-select">Add Tags to Query</Label>
                        <Select onValueChange={handleAddTag} value="">
                            <SelectTrigger id="tag-select">
                                <SelectValue placeholder="Select tags to add..." />
                            </SelectTrigger>
                            <SelectContent>
                                {availableTags.map(tag => (
                                    <SelectItem key={tag.id} value={tag.id.toString()}>
                                        {tag.alias || tag.code} ({tag.code})
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    {/* Selected Tags Display */}
                    {selectedTags.length > 0 && (
                        <div className="space-y-2">
                            <Label>Selected Tags</Label>
                            <div className="flex flex-wrap gap-2">
                                {selectedTags.map((tag, index) => (
                                    <Badge
                                        key={tag.id}
                                        variant="secondary"
                                        className="px-3 py-1 text-sm flex items-center gap-2"
                                        style={{ backgroundColor: `${TAG_COLORS[index % TAG_COLORS.length]}20`, border: `1px solid ${TAG_COLORS[index % TAG_COLORS.length]}` }}
                                    >
                                        <span className="w-2 h-2 rounded-full" style={{ backgroundColor: TAG_COLORS[index % TAG_COLORS.length] }} />
                                        {tag.alias || tag.code}
                                        <button
                                            onClick={() => handleRemoveTag(tag.id)}
                                            className="ml-1 hover:bg-gray-200 rounded-full p-0.5"
                                        >
                                            <X className="w-3 h-3" />
                                        </button>
                                    </Badge>
                                ))}
                            </div>
                        </div>
                    )}

                    {/* Date Range */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label htmlFor="start-date">
                                <Calendar className="w-4 h-4 inline mr-1" />
                                Start Date
                            </Label>
                            <input
                                id="start-date"
                                type="datetime-local"
                                value={formatInputValue(startDate)}
                                onChange={(e) => setStartDate(new Date(e.target.value))}
                                className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-violet-500"
                            />
                        </div>
                        <div className="space-y-2">
                            <Label htmlFor="end-date">
                                <Calendar className="w-4 h-4 inline mr-1" />
                                End Date
                            </Label>
                            <input
                                id="end-date"
                                type="datetime-local"
                                value={formatInputValue(endDate)}
                                onChange={(e) => setEndDate(new Date(e.target.value))}
                                className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-violet-500"
                            />
                        </div>
                    </div>

                    {/* Quick Range Selectors */}
                    <div className="flex flex-wrap gap-2 items-center">
                        <Label className="mr-2 text-sm text-muted-foreground">Quick Presets:</Label>
                        <Button variant="outline" size="sm" onClick={() => setQuickRange('1h')}>1 Hour</Button>
                        <Button variant="outline" size="sm" onClick={() => setQuickRange('6h')}>6 Hours</Button>
                        <Button variant="outline" size="sm" onClick={() => setQuickRange('24h')}>24 Hours</Button>
                        <Button variant="outline" size="sm" onClick={() => setQuickRange('7d')}>7 Days</Button>
                        <Button variant="outline" size="sm" onClick={() => setQuickRange('30d')}>30 Days</Button>
                    </div>

                    {/* Aggregation Controls */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label htmlFor="aggregation">Aggregation</Label>
                            <Select value={aggregation} onValueChange={(value: any) => setAggregation(value)}>
                                <SelectTrigger id="aggregation">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="mean">Mean (Average)</SelectItem>
                                    <SelectItem value="max">Maximum</SelectItem>
                                    <SelectItem value="min">Minimum</SelectItem>
                                    <SelectItem value="sum">Sum</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-2">
                            <Label htmlFor="interval">Interval</Label>
                            <Select value={interval} onValueChange={setInterval}>
                                <SelectTrigger id="interval">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="1m">1 Minute</SelectItem>
                                    <SelectItem value="5m">5 Minutes</SelectItem>
                                    <SelectItem value="15m">15 Minutes</SelectItem>
                                    <SelectItem value="1h">1 Hour</SelectItem>
                                    <SelectItem value="1d">1 Day</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    {/* Action Buttons */}
                    <div className="flex gap-3">
                        <Button
                            onClick={handleQuery}
                            disabled={selectedTagIds.length === 0 || isLoadingHistory}
                            className="bg-violet-600 hover:bg-violet-700"
                        >
                            {isLoadingHistory ? (
                                <>
                                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                                    Querying...
                                </>
                            ) : (
                                'Query Data'
                            )}
                        </Button>
                        <Button
                            onClick={handleExportCSV}
                            disabled={chartData.length === 0}
                            variant="outline"
                        >
                            <Download className="w-4 h-4 mr-2" />
                            Export CSV
                        </Button>
                    </div>
                </CardContent>
            </Card>

            {/* Error Display */}
            {hasError && (
                <Card className="border-red-200 bg-red-50">
                    <CardContent className="pt-6">
                        <p className="text-red-600">Error fetching historical data. Please check your query parameters and try again.</p>
                    </CardContent>
                </Card>
            )}

            {/* Chart */}
            {chartData.length > 0 && (
                <Card>
                    <CardHeader>
                        <CardTitle>Historical Data</CardTitle>
                        <CardDescription>
                            {selectedTags.map(tag => tag.alias || tag.code).join(', ')} | {startDate.toLocaleString()} to {endDate.toLocaleString()}
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <ResponsiveContainer width="100%" height={400}>
                            <LineChart data={chartData}>
                                <CartesianGrid strokeDasharray="3 3" />
                                <XAxis
                                    dataKey="date"
                                    tick={{ fontSize: 12 }}
                                    tickFormatter={(value) => new Date(value).toLocaleTimeString()}
                                />
                                <YAxis tick={{ fontSize: 12 }} />
                                <Tooltip
                                    labelFormatter={(value) => new Date(value).toLocaleString()}
                                    formatter={(value: any) => {
                                        return [(typeof value === 'number' ? value.toFixed(2) : 'N/A'), ''];
                                    }}
                                />
                                <Legend />
                                {selectedTags.map((tag, index) => {
                                    const key = tag.alias || tag.code;
                                    const color = TAG_COLORS[index % TAG_COLORS.length];
                                    return (
                                        <Line
                                            key={tag.id}
                                            type="monotone"
                                            dataKey={`${key}_value`}
                                            name={key}
                                            stroke={color}
                                            strokeWidth={2}
                                            dot={false}
                                            connectNulls={false}
                                        />
                                    );
                                })}
                            </LineChart>
                        </ResponsiveContainer>
                    </CardContent>
                </Card>
            )}

            {/* Empty State */}
            {!isLoadingHistory && chartData.length === 0 && selectedTagIds.length > 0 && shouldQuery && (
                <Card>
                    <CardContent className="pt-6 text-center text-gray-500">
                        <TrendingUp className="w-12 h-12 mx-auto mb-4 text-gray-400" />
                        <p>No data found for the selected query parameters.</p>
                        <p className="text-sm mt-2">Try adjusting the date range or selecting different tags.</p>
                    </CardContent>
                </Card>
            )}

            {/* Initial Empty State */}
            {selectedTagIds.length === 0 && (
                <Card>
                    <CardContent className="pt-6 text-center text-gray-500">
                        <TrendingUp className="w-12 h-12 mx-auto mb-4 text-gray-400" />
                        <p>Select tags and configure your query to view historical data.</p>
                    </CardContent>
                </Card>
            )}
        </div>
    );
};

export default TrendPage;
