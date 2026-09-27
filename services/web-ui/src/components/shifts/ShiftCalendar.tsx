import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { Shift } from '@/api/shifts';
import { cn } from '@/lib/utils';

// Monday first: a plant's week starts on Monday.
const DAYS = [1, 2, 3, 4, 5, 6, 0];
const HOURS = [0, 3, 6, 9, 12, 15, 18, 21, 24];
const COLORS = [
    'bg-sky-500/80 border-sky-600', 'bg-amber-500/80 border-amber-600', 'bg-indigo-500/80 border-indigo-600',
    'bg-emerald-500/80 border-emerald-600', 'bg-rose-500/80 border-rose-600', 'bg-teal-500/80 border-teal-600',
];

const minutes = (hhmm: string) => {
    const [h, m] = hhmm.split(':').map(Number);
    return (h || 0) * 60 + (m || 0);
};

interface Block {
    shift: Shift;
    color: string;
    from: number; // minutes into the day column
    to: number;
    continues: boolean; // the second half of a night shift
}

/**
 * The week at a glance: every active shift as a block on the days it runs.
 *
 * The table listed "22:00–06:00, Mon–Fri" and left the reader to work out
 * that Friday night runs into Saturday morning, that nobody covers Sunday,
 * and where two shifts overlap. Drawn, each of those is visible at once. A
 * night shift is split at midnight and its morning half drawn on the next day.
 */
export default function ShiftCalendar({ shifts, onSelect }: { shifts: Shift[]; onSelect?: (s: Shift) => void }) {
    const { t } = useTranslation();
    const [now, setNow] = useState(() => new Date());
    useEffect(() => {
        const id = setInterval(() => setNow(new Date()), 60_000);
        return () => clearInterval(id);
    }, []);

    const byDay = new Map<number, Block[]>(DAYS.map((d) => [d, []]));
    shifts.filter((s) => s.active).forEach((s, i) => {
        const color = COLORS[i % COLORS.length];
        const a = minutes(s.start_time);
        const b = minutes(s.end_time);
        for (const d of s.weekdays) {
            if (a < b) {
                byDay.get(d)?.push({ shift: s, color, from: a, to: b, continues: false });
            } else {
                byDay.get(d)?.push({ shift: s, color, from: a, to: 24 * 60, continues: false });
                byDay.get((d + 1) % 7)?.push({ shift: s, color, from: 0, to: b, continues: true });
            }
        }
    });

    const today = now.getDay();
    const nowMin = now.getHours() * 60 + now.getMinutes();
    const uncovered = DAYS.filter((d) => (byDay.get(d) ?? []).length === 0);

    return (
        <div className="rounded-md border bg-card p-3 space-y-2">
            <div className="grid grid-cols-[2.5rem_repeat(7,minmax(0,1fr))] gap-1 text-xs">
                <div />
                {DAYS.map((d) => (
                    <div key={d} className={cn('text-center font-medium py-1 rounded', d === today && 'bg-primary/10 text-primary')}>
                        {t(`shiftsPage.wd_${d}`)}
                    </div>
                ))}

                {/* Hour labels */}
                <div className="relative h-72">
                    {HOURS.map((h) => (
                        <span key={h} className="absolute right-1 -translate-y-1/2 text-[10px] text-muted-foreground tabular-nums"
                            style={{ top: `${(h / 24) * 100}%` }}>
                            {String(h).padStart(2, '0')}
                        </span>
                    ))}
                </div>

                {DAYS.map((d) => (
                    <div key={d} className="relative h-72 rounded bg-muted/40 overflow-hidden">
                        {HOURS.slice(1, -1).map((h) => (
                            <div key={h} className="absolute inset-x-0 border-t border-border/60" style={{ top: `${(h / 24) * 100}%` }} />
                        ))}
                        {(byDay.get(d) ?? []).map((b, i) => (
                            <button
                                key={`${b.shift.id}-${i}`}
                                type="button"
                                onClick={() => onSelect?.(b.shift)}
                                title={`${b.shift.name} ${b.shift.start_time}–${b.shift.end_time}`}
                                className={cn('absolute inset-x-0.5 rounded border text-white text-[10px] leading-tight px-1 overflow-hidden text-left hover:brightness-110', b.color,
                                    b.continues && 'border-t-0 rounded-t-none')}
                                style={{ top: `${(b.from / 1440) * 100}%`, height: `${((b.to - b.from) / 1440) * 100}%` }}
                            >
                                {!b.continues && <span className="font-semibold">{b.shift.name}</span>}
                            </button>
                        ))}
                        {d === today && (
                            <div className="absolute inset-x-0 border-t-2 border-red-500 z-10" style={{ top: `${(nowMin / 1440) * 100}%` }}
                                title={t('shiftsPage.now')} />
                        )}
                    </div>
                ))}
            </div>
            {uncovered.length > 0 && shifts.length > 0 && (
                <p className="text-xs text-muted-foreground">
                    {t('shiftsPage.uncovered', { days: uncovered.map((d) => t(`shiftsPage.wd_${d}`)).join(', ') })}
                </p>
            )}
        </div>
    );
}
