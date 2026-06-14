import { Eye, EyeOff, X, GripVertical, TrendingUp } from 'lucide-react';
import { Button } from '@/components/ui/button';

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
    if (pens.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2 p-4">
                <TrendingUp className="w-8 h-8 opacity-30" />
                <p className="text-xs text-center">Select tags from the browser to add pens</p>
            </div>
        );
    }

    return (
        <div className="divide-y divide-border overflow-y-auto">
            {pens.map((pen) => (
                <div key={pen.tagId} className="p-2 hover:bg-muted/40 transition-colors">
                    {/* Pen header row */}
                    <div className="flex items-center gap-1.5 mb-1">
                        <GripVertical className="w-3 h-3 text-muted-foreground/40 cursor-grab flex-shrink-0" />

                        {/* Color swatch / picker */}
                        <div className="relative flex-shrink-0">
                            <div
                                className="w-3 h-3 rounded-sm cursor-pointer ring-1 ring-border"
                                style={{ background: pen.color }}
                                title="Click to change color"
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
                            className="h-5 w-5 shrink-0"
                            onClick={() => onToggleVisibility(pen.tagId)}
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
                        >
                            <X className="w-3 h-3" />
                        </Button>
                    </div>

                    {/* Stats row */}
                    {pen.stats && (
                        <div className="grid grid-cols-4 gap-0.5 pl-5">
                            {[
                                { label: 'Lst', value: pen.stats.last },
                                { label: 'Min', value: pen.stats.min },
                                { label: 'Max', value: pen.stats.max },
                                { label: 'Avg', value: pen.stats.avg },
                            ].map(({ label, value }) => (
                                <div key={label} className="text-center">
                                    <div className="text-[9px] text-muted-foreground uppercase tracking-wide">{label}</div>
                                    <div className="text-[11px] font-mono font-medium">{fmt(value)}</div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            ))}
        </div>
    );
}
