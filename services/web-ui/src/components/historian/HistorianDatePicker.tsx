import { useState } from 'react';
import { DayPicker, type DateRange } from 'react-day-picker';
import { format, subDays, startOfDay, endOfDay } from 'date-fns';
import { it } from 'date-fns/locale';
import { Calendar, ChevronDown, SkipForward, Loader2 } from 'lucide-react';
import {
    Popover, PopoverContent, PopoverTrigger,
} from '@/components/ui/popover';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from 'sonner';
import { historyApi } from '@/api/history';
import 'react-day-picker/style.css';

export interface HistorianRange {
    from: Date;
    to: Date;
    preset?: string;
}

interface Props {
    value: HistorianRange;
    onChange: (range: HistorianRange) => void;
}

const QUICK_PRESETS = [
    { label: '1h',  ms: 60 * 60_000 },
    { label: '4h',  ms: 4  * 60 * 60_000 },
    { label: '8h',  ms: 8  * 60 * 60_000 },
    { label: '12h', ms: 12 * 60 * 60_000 },
    { label: '24h', ms: 24 * 60 * 60_000 },
    { label: '3d',  ms: 3  * 24 * 60 * 60_000 },
    { label: '7d',  ms: 7  * 24 * 60 * 60_000 },
    { label: '30d', ms: 30 * 24 * 60 * 60_000 },
];

function fmt(d: Date) {
    return format(d, 'dd/MM HH:mm');
}

export function HistorianDatePicker({ value, onChange }: Props) {
    const [open, setOpen] = useState(false);
    const [calRange, setCalRange] = useState<DateRange | undefined>({
        from: value.from,
        to: value.to,
    });
    const [startTime, setStartTime] = useState(format(value.from, 'HH:mm'));
    const [endTime, setEndTime] = useState(format(value.to, 'HH:mm'));
    const [jumping, setJumping] = useState(false);

    const applyPreset = (ms: number, label: string) => {
        const to = new Date();
        const from = new Date(to.getTime() - ms);
        onChange({ from, to, preset: label });
    };

    const applyToday = () => {
        const from = startOfDay(new Date());
        const to = new Date();
        onChange({ from, to, preset: 'today' });
    };

    const applyYesterday = () => {
        const from = startOfDay(subDays(new Date(), 1));
        const to = endOfDay(subDays(new Date(), 1));
        onChange({ from, to, preset: 'yesterday' });
    };

    const applyCalRange = () => {
        if (!calRange?.from) return;
        const [sh, sm] = startTime.split(':').map(Number);
        const [eh, em] = endTime.split(':').map(Number);
        const from = new Date(calRange.from);
        from.setHours(sh || 0, sm || 0, 0, 0);
        const to = new Date(calRange.to ?? calRange.from);
        to.setHours(eh || 23, em || 59, 59, 999);
        onChange({ from, to, preset: 'custom' });
        setOpen(false);
    };

    const jumpToLastData = async () => {
        setJumping(true);
        try {
            const range = await historyApi.getDataRange();
            if (!range.hasData || !range.newest) { toast.info('No data in DB'); return; }
            const newest = new Date(range.newest);
            const from = new Date(newest.getTime() - 30 * 60_000);
            const to = new Date(newest.getTime() + 30 * 60_000);
            onChange({ from, to, preset: 'custom' });
            toast.success(`Last data: ${format(newest, 'dd/MM HH:mm:ss')}`);
        } catch {
            toast.error('Failed to fetch data range');
        } finally {
            setJumping(false);
        }
    };

    const isPresetActive = (label: string) => value.preset === label;

    return (
        <div className="flex items-center gap-1.5 flex-wrap">
            {/* Quick presets */}
            <div className="flex bg-muted rounded-md p-0.5 gap-0">
                {QUICK_PRESETS.map(p => (
                    <button
                        key={p.label}
                        onClick={() => applyPreset(p.ms, p.label)}
                        className={`px-2 py-1 text-xs font-medium rounded transition-all ${
                            isPresetActive(p.label)
                                ? 'bg-background text-primary shadow-sm'
                                : 'text-muted-foreground hover:text-foreground'
                        }`}
                    >
                        {p.label}
                    </button>
                ))}
            </div>

            {/* Today / Yesterday */}
            <button
                onClick={applyToday}
                className={`px-2 py-1 text-xs font-medium rounded transition-all ${
                    isPresetActive('today') ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground hover:bg-muted'
                }`}
            >
                Today
            </button>
            <button
                onClick={applyYesterday}
                className={`px-2 py-1 text-xs font-medium rounded transition-all ${
                    isPresetActive('yesterday') ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground hover:bg-muted'
                }`}
            >
                Yesterday
            </button>

            {/* Calendar range picker */}
            <Popover open={open} onOpenChange={setOpen}>
                <PopoverTrigger asChild>
                    <Button
                        variant={value.preset === 'custom' ? 'default' : 'outline'}
                        size="sm"
                        className="h-7 text-xs gap-1.5 px-2.5"
                    >
                        <Calendar className="w-3 h-3" />
                        {value.preset === 'custom'
                            ? `${fmt(value.from)} → ${fmt(value.to)}`
                            : 'Custom'}
                        <ChevronDown className="w-3 h-3 opacity-60" />
                    </Button>
                </PopoverTrigger>
                <PopoverContent className="w-auto p-3" align="start">
                    <DayPicker
                        mode="range"
                        selected={calRange}
                        onSelect={(r) => {
                            setCalRange(r);
                            if (r?.from) setStartTime('00:00');
                            if (r?.to) setEndTime('23:59');
                        }}
                        numberOfMonths={2}
                        locale={it}
                        defaultMonth={subDays(new Date(), 7)}
                        weekStartsOn={1}
                        classNames={{
                            today: 'font-bold text-primary',
                            selected: 'bg-primary text-primary-foreground',
                            range_middle: 'bg-primary/20',
                        }}
                    />
                    <div className="flex gap-2 mt-2 border-t pt-2">
                        <div className="flex-1 space-y-1">
                            <p className="text-[10px] text-muted-foreground">From time</p>
                            <Input
                                type="time"
                                value={startTime}
                                onChange={e => setStartTime(e.target.value)}
                                className="h-7 text-xs"
                            />
                        </div>
                        <div className="flex-1 space-y-1">
                            <p className="text-[10px] text-muted-foreground">To time</p>
                            <Input
                                type="time"
                                value={endTime}
                                onChange={e => setEndTime(e.target.value)}
                                className="h-7 text-xs"
                            />
                        </div>
                    </div>
                    <Button
                        size="sm"
                        className="w-full mt-2 h-8 text-xs"
                        disabled={!calRange?.from}
                        onClick={applyCalRange}
                    >
                        Apply Range
                    </Button>
                </PopoverContent>
            </Popover>

            {/* Jump to last data */}
            <Button
                variant="ghost"
                size="sm"
                className="h-7 text-xs gap-1 text-muted-foreground hover:text-foreground"
                onClick={jumpToLastData}
                disabled={jumping}
                title="Jump to most recent data"
            >
                {jumping
                    ? <Loader2 className="w-3 h-3 animate-spin" />
                    : <SkipForward className="w-3 h-3" />
                }
                Last data
            </Button>

            {/* Current range display */}
            <span className="text-xs text-muted-foreground font-mono">
                {fmt(value.from)} – {fmt(value.to)}
            </span>
        </div>
    );
}
