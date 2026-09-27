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
} from 'lucide-react';
import { TimePicker } from './TimePicker';
import { useTrendStore } from '@/stores/useTrendStore';
import { AggregationType } from '@/types/trend';
import { useTranslation } from 'react-i18next';

interface TrendToolbarProps {
    onRefresh: () => void;
    onExportCSV: () => void;
    onAddChart: () => void;
    isLoading?: boolean;
    dataPointsCount?: number;
    tagsCount?: number;
    isMqttConnected?: boolean;
}

const AGGREGATION_OPTIONS: { value: AggregationType; labelKey: string }[] = [
    { value: 'max', labelKey: 'trend.agg_max_bool' },
    { value: 'mean', labelKey: 'trend.agg_mean' },
    { value: 'min', labelKey: 'trend.agg_min' },
    { value: 'first', labelKey: 'trend.agg_first' },
    { value: 'last', labelKey: 'trend.agg_last' },
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
    const { t, i18n } = useTranslation();
    const {
        timeRange,
        setTimeRange,
        liveMode,
        setLiveMode,
        aggregation,
        setAggregation,
        sidebarOpen,
        toggleSidebar,
        dataTableOpen,
        toggleDataTable,
    } = useTrendStore();

    const isLiveCapable = timeRange.preset !== 'custom' && timeRange.preset !== 'yesterday' && timeRange.preset !== 'previousShift';

    return (
        <div className="bg-card border-b px-4 py-2">
            {/* Top Row - Title and Actions */}
            <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-3">
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-9 sm:h-7 w-9 sm:w-7"
                        onClick={toggleSidebar}
                        title={t('trend.toggle_tags')}
                        aria-label={t('trend.toggle_tags')}
                    >
                        <PanelLeft className={`w-4 h-4 ${sidebarOpen ? 'text-primary' : 'text-muted-foreground'}`} />
                    </Button>
                    <div className="p-1.5 bg-primary/10 rounded">
                        <Activity className="w-4 h-4 text-primary" />
                    </div>
                    <div>
                        <h1 className="text-base font-semibold text-foreground">{t('trend.title')}</h1>
                        <p className="text-[10px] text-muted-foreground">
                            {t('trend.points_count', { count: dataPointsCount, formatted: dataPointsCount.toLocaleString(i18n.language) })} | {t('trend.tags_count', { count: tagsCount })}
                            {liveMode && isLiveCapable && isMqttConnected && (
                                <span className="ml-2 inline-flex items-center gap-1 text-green-600">
                                    <span className="w-1.5 h-1.5 rounded-full animate-pulse bg-green-500" />
                                    {t('trend.live_badge')}
                                </span>
                            )}
                        </p>
                    </div>
                </div>

                <div className="flex items-center gap-1.5">
                    {/* Aggregation */}
                    <Select value={aggregation} onValueChange={(v) => setAggregation(v as AggregationType)}>
                        <SelectTrigger className="w-28 h-9 sm:h-7 text-xs">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {AGGREGATION_OPTIONS.map((opt) => (
                                <SelectItem key={opt.value} value={opt.value}>
                                    {t(opt.labelKey)}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>

                    {/* Live Toggle */}
                    <Button
                        onClick={() => setLiveMode(!liveMode)}
                        variant={liveMode ? 'default' : 'outline'}
                        size="sm"
                        className="gap-1 h-9 sm:h-7 text-xs"
                        disabled={!isLiveCapable}
                    >
                        {liveMode ? (
                            <>
                                <Pause className="w-3 h-3" />
                                {t('trend.stop')}
                            </>
                        ) : (
                            <>
                                <Play className="w-3 h-3" />
                                {t('trend.live')}
                            </>
                        )}
                    </Button>

                    {/* Refresh */}
                    <Button
                        onClick={onRefresh}
                        variant="outline"
                        size="icon"
                        className="h-9 sm:h-7 w-9 sm:w-7"
                        disabled={isLoading}
                        title={t('trend.refresh')}
                        aria-label={t('trend.refresh')}
                    >
                        <RefreshCw className={`w-3 h-3 ${isLoading ? 'animate-spin' : ''}`} />
                    </Button>

                    {/* Add Chart */}
                    <Button
                        onClick={onAddChart}
                        variant="outline"
                        size="sm"
                        className="gap-1 h-9 sm:h-7 text-xs"
                    >
                        <Plus className="w-3 h-3" />
                        {t('trend.add_chart')}
                    </Button>

                    {/* Data Table Toggle */}
                    <Button
                        onClick={toggleDataTable}
                        variant={dataTableOpen ? 'default' : 'outline'}
                        size="icon"
                        className="h-9 sm:h-7 w-9 sm:w-7"
                        title={t('trend.toggle_table')}
                        aria-label={t('trend.toggle_table')}
                    >
                        <Table className="w-3 h-3" />
                    </Button>

                    {/* Export */}
                    <Button
                        onClick={onExportCSV}
                        variant="outline"
                        size="sm"
                        className="gap-1 h-9 sm:h-7 text-xs"
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
