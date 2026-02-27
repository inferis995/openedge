import React, { useState } from 'react';
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
import { Calendar, Clock, ChevronDown } from 'lucide-react';
import { TimePreset, TimeRange } from '@/types/trend';

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

const INDUSTRIAL_PRESETS: { value: TimePreset; label: string; description: string }[] = [
    { value: 'currentShift', label: 'Current Shift', description: 'This shift so far' },
    { value: 'previousShift', label: 'Previous Shift', description: 'Last completed shift' },
    { value: 'today', label: 'Today', description: 'From midnight to now' },
    { value: 'yesterday', label: 'Yesterday', description: 'Previous calendar day' },
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

    const handlePresetClick = (preset: TimePreset) => {
        onChange({ preset });
    };

    const handleCustomApply = () => {
        if (customStart && customEnd) {
            const start = new Date(`${customStart}T${customStartTime || '00:00'}`);
            const end = new Date(`${customEnd}T${customEndTime || '23:59'}`);
            onChange({
                preset: 'custom',
                customStart: start,
                customEnd: end,
            });
        }
    };

    const isCustom = value.preset === 'custom';

    return (
        <div className="flex items-center gap-2 flex-wrap">
            {/* Quick Presets */}
            <div className="flex bg-gray-100 rounded p-0.5">
                {QUICK_PRESETS.map((preset) => (
                    <button
                        key={preset.value}
                        onClick={() => handlePresetClick(preset.value)}
                        className={`px-2.5 py-1 text-xs font-medium rounded transition-all ${value.preset === preset.value
                            ? 'bg-white text-blue-600 shadow-sm'
                            : 'text-gray-600 hover:text-gray-900'
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
                <SelectTrigger className="w-36 h-7 text-xs">
                    <Clock className="w-3 h-3 mr-1" />
                    <SelectValue placeholder="Shift/Day" />
                </SelectTrigger>
                <SelectContent>
                    {INDUSTRIAL_PRESETS.map((preset) => (
                        <SelectItem key={preset.value} value={preset.value}>
                            <div>
                                <div className="font-medium">{preset.label}</div>
                                <div className="text-[10px] text-gray-400">{preset.description}</div>
                            </div>
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>

            {/* Custom Range Picker */}
            <Popover>
                <PopoverTrigger asChild>
                    <Button
                        variant={isCustom ? 'default' : 'outline'}
                        size="sm"
                        className="h-7 text-xs gap-1"
                    >
                        <Calendar className="w-3 h-3" />
                        Custom
                        <ChevronDown className="w-3 h-3" />
                    </Button>
                </PopoverTrigger>
                <PopoverContent className="w-80" align="end">
                    <div className="space-y-3">
                        <div className="text-sm font-medium text-gray-700">Custom Time Range</div>

                        {/* Start Date/Time */}
                        <div className="space-y-1.5">
                            <label className="text-xs text-gray-500">From</label>
                            <div className="flex gap-2">
                                <Input
                                    type="date"
                                    value={customStart}
                                    onChange={(e) => setCustomStart(e.target.value)}
                                    className="h-8 text-xs flex-1"
                                />
                                <Input
                                    type="time"
                                    value={customStartTime}
                                    onChange={(e) => setCustomStartTime(e.target.value)}
                                    className="h-8 text-xs w-24"
                                />
                            </div>
                        </div>

                        {/* End Date/Time */}
                        <div className="space-y-1.5">
                            <label className="text-xs text-gray-500">To</label>
                            <div className="flex gap-2">
                                <Input
                                    type="date"
                                    value={customEnd}
                                    onChange={(e) => setCustomEnd(e.target.value)}
                                    className="h-8 text-xs flex-1"
                                />
                                <Input
                                    type="time"
                                    value={customEndTime}
                                    onChange={(e) => setCustomEndTime(e.target.value)}
                                    className="h-8 text-xs w-24"
                                />
                            </div>
                        </div>

                        {/* Quick set buttons */}
                        <div className="flex gap-1 flex-wrap">
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-6 text-[10px]"
                                onClick={() => {
                                    const now = new Date();
                                    const hourAgo = new Date(now.getTime() - 60 * 60 * 1000);
                                    setCustomStart(formatDateForInput(hourAgo));
                                    setCustomStartTime(formatTimeForInput(hourAgo));
                                    setCustomEnd(formatDateForInput(now));
                                    setCustomEndTime(formatTimeForInput(now));
                                }}
                            >
                                Last Hour
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-6 text-[10px]"
                                onClick={() => {
                                    const now = new Date();
                                    const dayAgo = new Date(now.getTime() - 24 * 60 * 60 * 1000);
                                    setCustomStart(formatDateForInput(dayAgo));
                                    setCustomStartTime('00:00');
                                    setCustomEnd(formatDateForInput(now));
                                    setCustomEndTime(formatTimeForInput(now));
                                }}
                            >
                                Last 24h
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                className="h-6 text-[10px]"
                                onClick={() => {
                                    const now = new Date();
                                    const weekAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
                                    setCustomStart(formatDateForInput(weekAgo));
                                    setCustomStartTime('00:00');
                                    setCustomEnd(formatDateForInput(now));
                                    setCustomEndTime(formatTimeForInput(now));
                                }}
                            >
                                Last 7 Days
                            </Button>
                        </div>

                        <Button
                            onClick={handleCustomApply}
                            size="sm"
                            className="w-full h-8 text-xs"
                            disabled={!customStart || !customEnd}
                        >
                            Apply Custom Range
                        </Button>
                    </div>
                </PopoverContent>
            </Popover>

            {/* Current range display */}
            {isCustom && value.customStart && value.customEnd && (
                <div className="text-xs text-gray-500">
                    {value.customStart.toLocaleDateString('it-IT')} {value.customStart.toLocaleTimeString('it-IT', { hour: '2-digit', minute: '2-digit' })}
                    {' - '}
                    {value.customEnd.toLocaleDateString('it-IT')} {value.customEnd.toLocaleTimeString('it-IT', { hour: '2-digit', minute: '2-digit' })}
                </div>
            )}
        </div>
    );
};

export default TimePicker;
