import { Eye, EyeOff, X, GripVertical, TrendingUp } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useTranslation } from 'react-i18next';
import type { ForecastStatus } from '@/api/forecast';

export interface PenStats {
    min: number | null;
    max: number | null;
    avg: number | null;
    last: number | null;
    count: number;
}

export interface Pen {
    tagId: number;
    label: string;
    unit?: string;
    color: string;
    visible: boolean;
    stats?: PenStats;
    /** When the newest good sample in the window was taken (ms). */
    lastSampleAt?: number | null;
    /** The tag's gateway: when it last reported, and whether it is online. */
    contact?: { at: number | null; online: boolean } | null;
    /** The tag's forecast, when forecasts are shown: whether it is drawn, and why not. */
    forecast?: { status: ForecastStatus; reason?: string } | null;
}

/** "4 h ago", in the user's language. */
function ago(ms: number, lang: string): string {
    const s = (ms - Date.now()) / 1000;
    // A time the server sent in another shape is shown as unknown, not a crash.
    if (!Number.isFinite(s)) return '—';
    const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'auto', style: 'short' });
    const a = Math.abs(s);
    if (a < 60) return rtf.format(Math.round(s), 'second');
    if (a < 3600) return rtf.format(Math.round(s / 60), 'minute');
    if (a < 86400) return rtf.format(Math.round(s / 3600), 'hour');
    return rtf.format(Math.round(s / 86400), 'day');
}

interface Props {
    pens: Pen[];
    onToggleVisibility: (tagId: number) => void;
    onRemove: (tagId: number) => void;
    onColorChange: (tagId: number, color: string) => void;
}

function fmt(v: number | null | undefined, digits = 3): string {
    if (v === null || v === undefined) return '—';
    const n = Number(v);
    if (!isFinite(n)) return '—';
    if (Number.isInteger(n)) return n.toString();
    return n.toFixed(digits).replace(/\.?0+$/, '');
}

const PALETTE = [
    '#3b82f6', '#ef4444', '#22c55e', '#f59e0b', '#8b5cf6',
    '#06b6d4', '#ec4899', '#f97316', '#6366f1', '#14b8a6',
    '#a855f7', '#eab308', '#84cc16', '#f43f5e', '#22d3ee',
];

export function PenPanel({ pens, onToggleVisibility, onRemove, onColorChange }: Props) {
    const { t, i18n } = useTranslation();
    if (pens.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2 p-4">
                <TrendingUp className="w-10 sm:w-8 h-10 sm:h-8 opacity-30" />
                <p className="text-xs text-center">{t('historian.pens_empty')}</p>
            </div>
        );
    }

    return (
        <div className="divide-y divide-border overflow-y-auto">
            {pens.map((pen) => (
                <div key={pen.tagId} className="p-2 hover:bg-muted/40 transition-colors">
                    {/* Pen header row */}
                    <div className="flex items-center gap-1.5 mb-1">
                        <GripVertical className="w-3 h-3 text-muted-foreground/40 cursor-grab shrink-0" />

                        {/* Color swatch / picker */}
                        <div className="relative shrink-0">
                            <div
                                className="w-3 h-3 rounded-sm cursor-pointer ring-1 ring-border"
                                style={{ background: pen.color }}
                                title={t('historian.change_color')}
                            />
                            <select
                                className="absolute inset-0 opacity-0 cursor-pointer w-3 h-3"
                                value={pen.color}
                                onChange={e => onColorChange(pen.tagId, e.target.value)}
                            >
                                {PALETTE.map(c => (
                                    <option key={c} value={c} style={{ background: c }}>
                                        {c}
                                    </option>
                                ))}
                            </select>
                        </div>

                        <span
                            className="text-xs font-medium truncate flex-1"
                            style={{ color: pen.visible ? pen.color : '#6b7280' }}
                            title={pen.label}
                        >
                            {pen.label}
                        </span>
                        {pen.unit && (
                            <span className="text-[10px] text-muted-foreground shrink-0">{pen.unit}</span>
                        )}

                        <Button
                            variant="ghost"
                            size="icon"
                            className="h-5 w-5 shrink-0 hover:bg-muted hover:text-foreground"
                            onClick={() => onToggleVisibility(pen.tagId)}
                            title={pen.visible ? t('historian.hide_pen') : t('historian.show_pen')}
                        >
                            {pen.visible
                                ? <Eye className="w-3 h-3" />
                                : <EyeOff className="w-3 h-3 opacity-40" />
                            }
                        </Button>
                        <Button
                            variant="ghost"
                            size="icon"
                            className="h-5 w-5 shrink-0 text-destructive/60 hover:text-destructive"
                            onClick={() => onRemove(pen.tagId)}
                            title={t('historian.remove_pen')}
                        >
                            <X className="w-3 h-3" />
                        </Button>
                    </div>

                    {/* Stats row */}
                    {pen.stats && (
                        <div className="grid grid-cols-2 sm:grid-cols-2 lg:grid-cols-4 gap-0.5 pl-5">
                            {[
                                { label: t('historian.stat_last'), value: pen.stats.last },
                                { label: t('historian.stat_min'), value: pen.stats.min },
                                { label: t('historian.stat_max'), value: pen.stats.max },
                                { label: t('historian.stat_avg'), value: pen.stats.avg },
                            ].map(({ label, value }, i) => (
                                <div key={label} className="text-center min-w-0">
                                    <div className="text-[11px] text-muted-foreground truncate">{label}</div>
                                    <div
                                        className={i === 0 ? 'text-lg font-mono font-bold leading-tight tabular-nums truncate' : 'text-[11px] font-mono font-medium tabular-nums truncate'}
                                        style={i === 0 ? { color: pen.visible ? pen.color : undefined } : undefined}
                                        title={fmt(value)}
                                    >{fmt(value)}</div>
                                </div>
                            ))}
                        </div>
                    )}

                    {/* Data age and gateway contact: a logger can be in touch
                        and still send old readings, or be silent with a recent one. */}
                    {(pen.lastSampleAt || pen.contact) && (
                        <div className="pl-5 mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[10px] text-muted-foreground">
                            {pen.lastSampleAt && <span>{t('historian.last_sample', { age: ago(pen.lastSampleAt, i18n.language) })}</span>}
                            {pen.contact && (
                                <span className={pen.contact.online ? '' : 'text-amber-600 dark:text-amber-500'}>
                                    {pen.contact.at
                                        ? t('historian.contact', { age: ago(pen.contact.at, i18n.language) })
                                        : t('historian.contact_never')}
                                    {!pen.contact.online && ` · ${t('historian.gateway_offline')}`}
                                </span>
                            )}
                        </div>
                    )}
                    {pen.forecast && (
                        <div className={`pl-5 mt-0.5 text-[10px] ${pen.forecast.status === 'ok' ? 'text-muted-foreground' : 'text-amber-600 dark:text-amber-500'}`}
                            title={pen.forecast.reason}>
                            {t(`forecast.pen_${pen.forecast.status}`)}
                        </div>
                    )}
                </div>
            ))}
        </div>
    );
}
