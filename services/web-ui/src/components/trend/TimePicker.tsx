import React, { useState, useCallback } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from '@/components/ui/popover';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Calendar, Clock, ChevronDown, SkipForward, Loader2 } from 'lucide-react';
import { TimePreset, TimeRange } from '@/types/trend';
import { historyApi } from '@/api/history';
import { toast } from 'sonner';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';

interface TimePickerProps {
    value: TimeRange;
    onChange: (timeRange: TimeRange) => void;
    liveMode?: boolean;
}

const QUICK_PRESETS: { value: TimePreset; label: string }[] = [
    { value: '15m', label: '15m' },
    { value: '1h', label: '1h' },
    { value: '6h', label: '6h' },
    { value: '12h', label: '12h' },
    { value: '24h', label: '24h' },
    { value: '7d', label: '7d' },
    { value: '30d', label: '30d' },
];

const INDUSTRIAL_PRESETS: { value: TimePreset; labelKey: string; descriptionKey: string }[] = [
    { value: 'currentShift', labelKey: 'trend.preset_current_shift', descriptionKey: 'trend.preset_current_shift_desc' },
    { value: 'previousShift', labelKey: 'trend.preset_previous_shift', descriptionKey: 'trend.preset_previous_shift_desc' },
    { value: 'today', labelKey: 'trend.preset_today', descriptionKey: 'trend.preset_today_desc' },
    { value: 'yesterday', labelKey: 'trend.preset_yesterday', descriptionKey: 'trend.preset_yesterday_desc' },
];

const formatDateForInput = (date: Date): string => {
    return date.toISOString().split('T')[0];
};

const formatTimeForInput = (date: Date): string => {
    return date.toTimeString().substring(0, 5);
};

export const TimePicker: React.FC<TimePickerProps> = ({
    value,
    onChange,
}) => {
    const [customStart, setCustomStart] = useState(
        value.customStart ? formatDateForInput(value.customStart) : ''
    );
    const [customStartTime, setCustomStartTime] = useState(
        value.customStart ? formatTimeForInput(value.customStart) : '00:00'
    );
    const [customEnd, setCustomEnd] = useState(
        value.customEnd ? formatDateForInput(value.customEnd) : ''
    );
    const [customEndTime, setCustomEndTime] = useState(
        value.customEnd ? formatTimeForInput(value.customEnd) : '23:59'
    );
    const [isLoadingLastData, setIsLoadingLastData] = useState(false);
    const { t } = useTranslation();

    const handlePresetClick = (preset: TimePreset) => {
        onChange({ preset });
    };

    const handleCustomApply = () => {
        if (customStart && customEnd) {
            let start = new Date(`${customStart}T${customStartTime || '00:00'}`);
            let end = new Date(`${customEnd}T${customEndTime || '23:59'}`);

            if (start.getTime() > end.getTime()) {
                const temp = start;
                start = end;
                end = temp;
                setCustomStart(formatDateForInput(start));
                setCustomStartTime(formatTimeForInput(start));
                setCustomEnd(formatDateForInput(end));
                setCustomEndTime(formatTimeForInput(end));
            }

            onChange({
                preset: 'custom',
                customStart: start,
                customEnd: end,
            });
        }
    };

    // "Go to Last Data" — fetch data range from API and set custom range
    const handleGoToLastData = useCallback(async () => {
        setIsLoadingLastData(true);
        try {
            const range = await historyApi.getDataRange();
            if (!range.hasData || !range.newest) {
                toast.info(i18n.t('trend.no_history_data'));
                return;
            }

            // Center a 1-hour window around the newest data point
            const newest = new Date(range.newest);
            const windowStart = new Date(newest.getTime() - 30 * 60 * 1000); // 30 min before
            const windowEnd = new Date(newest.getTime() + 30 * 60 * 1000);   // 30 min after

            setCustomStart(formatDateForInput(windowStart));
            setCustomStartTime(formatTimeForInput(windowStart));
            setCustomEnd(formatDateForInput(windowEnd));
            setCustomEndTime(formatTimeForInput(windowEnd));

            onChange({
                preset: 'custom',
                customStart: windowStart,
                customEnd: windowEnd,
            });

            toast.success(i18n.t('trend.moved_to_last_data', { time: newest.toLocaleString(i18n.language) }));
        } catch {
            toast.error(i18n.t('trend.data_range_failed'));
        } finally {
            setIsLoadingLastData(false);
        }
    }, [onChange]);

    const isCustom = value.preset === 'custom';

    return (
        <div className="flex items-center gap-2 flex-wrap">
            {/* Quick Presets */}
            <div className="flex bg-muted rounded p-0.5">
                {QUICK_PRESETS.map((preset) => (
                    <button
                        key={preset.value}
                        onClick={() => handlePresetClick(preset.value)}
                        className={`px-2.5 py-1 text-xs font-medium rounded transition-all ${value.preset === preset.value
                            ? 'bg-background text-primary shadow-sm'
                            : 'text-muted-foreground hover:text-foreground'
                            }`}
                    >
                        {preset.label}
                    </button>
                ))}
            </div>

            {/* Industrial Presets */}
            <Select
                value={INDUSTRIAL_PRESETS.some(p => p.value === value.preset) ? value.preset : ''}
                onValueChange={(v) => v && handlePresetClick(v as TimePreset)}
            >
                <SelectTrigger className="w-36 h-9 sm:h-7 text-xs">
                    <Clock className="w-3 h-3 mr-1" />
                    <SelectValue placeholder={t('trend.shift_day')} />
                </SelectTrigger>
                <SelectContent>
                    {INDUSTRIAL_PRESETS.map((preset) => (
                        <SelectItem key={preset.value} value={preset.value}>
                            <div>
                                <div className="font-medium">{t(preset.labelKey)}</div>
                                <div className="text-[10px] text-muted-foreground">{t(preset.descriptionKey)}</div>
                            </div>
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>

            {/* Go to Last Data Button */}
            <Button
                variant="outline"
                size="sm"
                className="h-9 sm:h-7 text-xs gap-1"
                onClick={handleGoToLastData}
                disabled={isLoadingLastData}
                title={t('trend.jump_last_title')}
            >
                {isLoadingLastData ? (
                    <Loader2 className="w-3 h-3 animate-spin" />
                ) : (
                    <SkipForward className="w-3 h-3" />
                )}
                {t('trend.last_data')}
            </Button>

            {/* Custom Range Picker */}
            <Popover>
                <PopoverTrigger asChild>
                    <Button
                        variant={isCustom ? 'default' : 'outline'}
                        size="sm"
                        className="h-9 sm:h-7 text-xs gap-1"
                    >
                        <Calendar className="w-3 h-3" />
                        {t('trend.custom')}
                        <ChevronDown className="w-3 h-3" />
                    </Button>
                </PopoverTrigger>
                <PopoverContent className="w-80" align="end">
                    <div className="space-y-3">
                        <div className="text-sm font-medium text-foreground">{t('trend.custom_range_title')}</div>

                        {/* Start Date/Time */}
                        <div className="space-y-1.5">
                            <label className="text-xs text-muted-foreground">{t('trend.from')}</label>
                            <div className="flex gap-2">
                                <Input
                                    type="date"
                                    value={customStart}
                                    onChange={(e) => setCustomStart(e.target.value)}
                                    className="h-10 sm:h-8 text-xs flex-1"
                                />
                                <Input
                                    type="time"
                                    value={customStartTime}
                                    onChange={(e) => setCustomStartTime(e.target.value)}
                                    className="h-10 sm:h-8 text-xs w-24"
                                />
                            </div>
                        </div>

                        {/* End Date/Time */}
                        <div className="space-y-1.5">
                            <label className="text-xs text-muted-foreground">{t('trend.to')}</label>
                            <div className="flex gap-2">
                                <Input
                                    type="date"
                                    value={customEnd}
                                    onChange={(e) => setCustomEnd(e.target.value)}
                                    className="h-10 sm:h-8 text-xs flex-1"
                                />
                                <Input
                                    type="time"
                                    value={customEndTime}
                                    onChange={(e) => setCustomEndTime(e.target.value)}
                                    className="h-10 sm:h-8 text-xs w-24"
                                />
                            </div>
                        </div>

                        {/* Quick set buttons */}
                        <div className="flex gap-1 flex-wrap">
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-9 sm:h-6 text-[10px]"
                                onClick={() => {
                                    const now = new Date();
                                    const hourAgo = new Date(now.getTime() - 60 * 60 * 1000);
                                    setCustomStart(formatDateForInput(hourAgo));
                                    setCustomStartTime(formatTimeForInput(hourAgo));
                                    setCustomEnd(formatDateForInput(now));
                                    setCustomEndTime(formatTimeForInput(now));
                                }}
                            >
                                {t('trend.last_hour')}
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-9 sm:h-6 text-[10px]"
                                onClick={() => {
                                    const now = new Date();
                                    const dayAgo = new Date(now.getTime() - 24 * 60 * 60 * 1000);
                                    setCustomStart(formatDateForInput(dayAgo));
                                    setCustomStartTime('00:00');
                                    setCustomEnd(formatDateForInput(now));
                                    setCustomEndTime(formatTimeForInput(now));
                                }}
                            >
                                {t('trend.last_24h')}
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-9 sm:h-6 text-[10px]"
                                onClick={() => {
                                    const now = new Date();
                                    const weekAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
                                    setCustomStart(formatDateForInput(weekAgo));
                                    setCustomStartTime('00:00');
                                    setCustomEnd(formatDateForInput(now));
                                    setCustomEndTime(formatTimeForInput(now));
                                }}
                            >
                                {t('trend.last_7_days')}
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-9 sm:h-6 text-[10px]"
                                onClick={() => {
                                    const now = new Date();
                                    const monthAgo = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
                                    setCustomStart(formatDateForInput(monthAgo));
                                    setCustomStartTime('00:00');
                                    setCustomEnd(formatDateForInput(now));
                                    setCustomEndTime(formatTimeForInput(now));
                                }}
                            >
                                {t('trend.last_30_days')}
                            </Button>
                        </div>

                        <Button
                            onClick={handleCustomApply}
                            size="sm"
                            className="w-full h-10 sm:h-8 text-xs"
                            disabled={!customStart || !customEnd}
                        >
                            {t('trend.apply_range')}
                        </Button>
                    </div>
                </PopoverContent>
            </Popover>

            {/* Current range display */}
            {isCustom && value.customStart && value.customEnd && (
                <div className="text-xs text-muted-foreground">
                    {value.customStart.toLocaleDateString(i18n.language)} {value.customStart.toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' })}
                    {' - '}
                    {value.customEnd.toLocaleDateString(i18n.language)} {value.customEnd.toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' })}
                </div>
            )}
        </div>
    );
};

export default TimePicker;
