import React from 'react';
import { Button } from '@/components/ui/button';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import {
    Play,
    Pause,
    RefreshCw,
    Download,
    Plus,
    Table,
    PanelLeft,
    Activity,
    Layers,
} from 'lucide-react';
import { TimePicker } from './TimePicker';
import { useTrendStore } from '@/stores/useTrendStore';
import { AggregationType, CompareMode } from '@/types/trend';

interface TrendToolbarProps {
    onRefresh: () => void;
    onExportCSV: () => void;
    onAddChart: () => void;
    isLoading?: boolean;
    dataPointsCount?: number;
    tagsCount?: number;
    isMqttConnected?: boolean;
}

const AGGREGATION_OPTIONS: { value: AggregationType; label: string }[] = [
    { value: 'max', label: 'Max (BOOL)' },
    { value: 'mean', label: 'Mean' },
    { value: 'min', label: 'Min' },
    { value: 'first', label: 'First' },
    { value: 'last', label: 'Last' },
];

const COMPARE_OPTIONS: { value: CompareMode; label: string }[] = [
    { value: 'none', label: 'None' },
    { value: 'todayVsYesterday', label: 'Today vs Yesterday' },
    { value: 'thisShiftVsLastShift', label: 'This Shift vs Last' },
];

export const TrendToolbar: React.FC<TrendToolbarProps> = ({
    onRefresh,
    onExportCSV,
    onAddChart,
    isLoading = false,
    dataPointsCount = 0,
    tagsCount = 0,
    isMqttConnected = false,
}) => {
    const {
        timeRange,
        setTimeRange,
        liveMode,
        setLiveMode,
        aggregation,
        setAggregation,
        compareMode,
        setCompareMode,
        sidebarOpen,
        toggleSidebar,
        dataTableOpen,
        toggleDataTable,
    } = useTrendStore();

    const isLiveCapable = timeRange.preset !== 'custom' && timeRange.preset !== 'yesterday' && timeRange.preset !== 'previousShift';

    return (
        <div className="bg-white border-b px-4 py-2">
            {/* Top Row - Title and Actions */}
            <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-3">
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7"
                        onClick={toggleSidebar}
                    >
                        <PanelLeft className={`w-4 h-4 ${sidebarOpen ? 'text-blue-600' : 'text-gray-400'}`} />
                    </Button>
                    <div className="p-1.5 bg-blue-50 rounded">
                        <Activity className="w-4 h-4 text-blue-600" />
                    </div>
                    <div>
                        <h1 className="text-base font-semibold text-gray-900">Trend Analysis</h1>
                        <p className="text-[10px] text-gray-500">
                            {dataPointsCount.toLocaleString()} points | {tagsCount} tags
                            {liveMode && isLiveCapable && isMqttConnected && (
                                <span className="ml-2 inline-flex items-center gap-1 text-green-600">
                                    <span className="w-1.5 h-1.5 rounded-full animate-pulse bg-green-500" />
                                    LIVE
                                </span>
                            )}
                        </p>
                    </div>
                </div>

                <div className="flex items-center gap-1.5">
                    {/* Compare Mode */}
                    <Select value={compareMode} onValueChange={(v) => setCompareMode(v as CompareMode)}>
                        <SelectTrigger className="w-40 h-7 text-xs">
                            <Layers className="w-3 h-3 mr-1" />
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {COMPARE_OPTIONS.map((opt) => (
                                <SelectItem key={opt.value} value={opt.value}>
                                    {opt.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>

                    {/* Aggregation */}
                    <Select value={aggregation} onValueChange={(v) => setAggregation(v as AggregationType)}>
                        <SelectTrigger className="w-28 h-7 text-xs">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {AGGREGATION_OPTIONS.map((opt) => (
                                <SelectItem key={opt.value} value={opt.value}>
                                    {opt.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>

                    {/* Live Toggle */}
                    <Button
                        onClick={() => setLiveMode(!liveMode)}
                        variant={liveMode ? 'default' : 'outline'}
                        size="sm"
                        className="gap-1 h-7 text-xs"
                        disabled={!isLiveCapable}
                    >
                        {liveMode ? (
                            <>
                                <Pause className="w-3 h-3" />
                                Stop
                            </>
                        ) : (
                            <>
                                <Play className="w-3 h-3" />
                                Live
                            </>
                        )}
                    </Button>

                    {/* Refresh */}
                    <Button
                        onClick={onRefresh}
                        variant="outline"
                        size="icon"
                        className="h-7 w-7"
                        disabled={isLoading}
                    >
                        <RefreshCw className={`w-3 h-3 ${isLoading ? 'animate-spin' : ''}`} />
                    </Button>

                    {/* Add Chart */}
                    <Button
                        onClick={onAddChart}
                        variant="outline"
                        size="sm"
                        className="gap-1 h-7 text-xs"
                    >
                        <Plus className="w-3 h-3" />
                        Chart
                    </Button>

                    {/* Data Table Toggle */}
                    <Button
                        onClick={toggleDataTable}
                        variant={dataTableOpen ? 'default' : 'outline'}
                        size="icon"
                        className="h-7 w-7"
                    >
                        <Table className="w-3 h-3" />
                    </Button>

                    {/* Export */}
                    <Button
                        onClick={onExportCSV}
                        variant="outline"
                        size="sm"
                        className="gap-1 h-7 text-xs"
                        disabled={dataPointsCount === 0}
                    >
                        <Download className="w-3 h-3" />
                        CSV
                    </Button>
                </div>
            </div>

            {/* Time Range Row */}
            <div className="flex items-center gap-2">
                <TimePicker
                    value={timeRange}
                    onChange={setTimeRange}
                    liveMode={liveMode}
                />
            </div>
        </div>
    );
};

export default TrendToolbar;
