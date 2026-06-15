import { useState } from 'react';
import { SynopticWidget } from '@/api/synoptics';
import { cn } from '@/lib/utils';

// Live value passed to a widget at runtime. In the designer there is no value
// (undefined) and widgets render a neutral preview state.
export interface LiveValue {
    value: unknown;
    quality: number; // internal scale: 0 = GOOD, 1 = UNCERTAIN, 2 = BAD
}

const num = (v: unknown): number | null => {
    if (v === null || v === undefined || v === '') return null;
    const n = typeof v === 'boolean' ? (v ? 1 : 0) : Number(v);
    return Number.isFinite(n) ? n : null;
};

// Resolve the display color of a threshold-driven widget (value/indicator).
const thresholdColor = (n: number | null, cfg: SynopticWidget['config']): string => {
    if (n === null) return '#64748b'; // slate-500 — no data
    if (cfg?.critAbove !== undefined && n >= cfg.critAbove) return '#ef4444'; // red-500
    if (cfg?.warnAbove !== undefined && n >= cfg.warnAbove) return '#f59e0b'; // amber-500
    return cfg?.color || '#10b981'; // emerald-500
};

const isOn = (n: number | null, cfg: SynopticWidget['config']): boolean => {
    if (n === null) return false;
    const on = cfg?.onValue ?? 1;
    return n >= on;
};

// ── Industrial SVG symbols (authored in-house, no external dependency) ──────

function TankSymbol({ pct, color }: { pct: number; color: string }) {
    const clamped = Math.max(0, Math.min(100, pct));
    const fillH = (clamped / 100) * 84;
    return (
        <svg viewBox="0 0 100 120" className="w-full h-full" preserveAspectRatio="none">
            <rect x="14" y="8" width="72" height="104" rx="10" fill="#1e293b" stroke="#475569" strokeWidth="3" />
            <rect x="17" y={11 + (84 - fillH)} width="66" height={fillH} rx="6" fill={color} opacity="0.85" />
            <line x1="14" y1="50" x2="86" y2="50" stroke="#475569" strokeWidth="1" opacity="0.4" />
            <line x1="14" y1="71" x2="86" y2="71" stroke="#475569" strokeWidth="1" opacity="0.4" />
        </svg>
    );
}

function PumpSymbol({ on, color }: { on: boolean; color: string }) {
    return (
        <svg viewBox="0 0 100 100" className="w-full h-full">
            <circle cx="50" cy="50" r="38" fill="#1e293b" stroke={on ? color : '#475569'} strokeWidth="4" />
            <g className={on ? 'origin-center animate-spin' : ''} style={{ transformOrigin: '50px 50px', animationDuration: '2s' }}>
                <polygon points="50,22 50,78 78,50" fill={on ? color : '#64748b'} />
            </g>
            <rect x="2" y="44" width="16" height="12" fill="#475569" />
            <rect x="82" y="44" width="16" height="12" fill="#475569" />
        </svg>
    );
}

function ValveSymbol({ open, color }: { open: boolean; color: string }) {
    const c = open ? color : '#ef4444';
    return (
        <svg viewBox="0 0 100 100" className="w-full h-full">
            <rect x="2" y="42" width="20" height="16" fill="#475569" />
            <rect x="78" y="42" width="20" height="16" fill="#475569" />
            <polygon points="22,30 50,50 22,70" fill={c} stroke="#0f172a" strokeWidth="2" />
            <polygon points="78,30 50,50 78,70" fill={c} stroke="#0f172a" strokeWidth="2" />
            <rect x="46" y="14" width="8" height="20" fill="#475569" />
            <rect x="34" y="8" width="32" height="8" rx="3" fill={c} />
        </svg>
    );
}

function MotorSymbol({ on, color }: { on: boolean; color: string }) {
    return (
        <svg viewBox="0 0 100 100" className="w-full h-full">
            <rect x="12" y="28" width="64" height="44" rx="6" fill="#1e293b" stroke={on ? color : '#475569'} strokeWidth="4" />
            <text x="44" y="58" textAnchor="middle" fontSize="28" fontWeight="bold" fill={on ? color : '#64748b'}>M</text>
            <rect x="76" y="40" width="14" height="20" fill="#475569" />
            <circle cx="44" cy="80" r="5" fill={on ? color : '#64748b'} className={on ? 'animate-pulse' : ''} />
        </svg>
    );
}

function GaugeSymbol({ n, min, max, color, label, decimals }: { n: number | null; min: number; max: number; color: string; label: string; decimals?: number }) {
    const range = max - min || 1;
    const pct = n === null ? 0 : Math.max(0, Math.min(1, (n - min) / range));
    // 270° sweep from 135° to 405°.
    const startA = 135, sweep = 270;
    const endA = startA + pct * sweep;
    const polar = (a: number, r: number) => {
        const rad = (a * Math.PI) / 180;
        return [50 + r * Math.cos(rad), 50 + r * Math.sin(rad)];
    };
    const arc = (a0: number, a1: number, r: number) => {
        const [x0, y0] = polar(a0, r);
        const [x1, y1] = polar(a1, r);
        const large = a1 - a0 > 180 ? 1 : 0;
        return `M ${x0} ${y0} A ${r} ${r} 0 ${large} 1 ${x1} ${y1}`;
    };
    return (
        <svg viewBox="0 0 100 100" className="w-full h-full">
            <path d={arc(startA, startA + sweep, 38)} fill="none" stroke="#334155" strokeWidth="9" strokeLinecap="round" />
            {n !== null && <path d={arc(startA, endA, 38)} fill="none" stroke={color} strokeWidth="9" strokeLinecap="round" />}
            <text x="50" y="52" textAnchor="middle" fontSize="20" fontWeight="bold" fill="#e2e8f0">
                {n === null ? '—' : n.toFixed(decimals ?? 1)}
            </text>
            <text x="50" y="68" textAnchor="middle" fontSize="9" fill="#94a3b8">{label}</text>
        </svg>
    );
}

function ButtonWidget({ widget, active, color, live, onWrite }: {
    widget: SynopticWidget;
    active: boolean;
    color: string;
    live?: LiveValue;
    onWrite?: (value: number) => Promise<void>;
}) {
    const [sending, setSending] = useState(false);
    const [feedback, setFeedback] = useState<'ok' | 'err' | null>(null);
    const cfg = widget.config || {};
    const isPreview = live === undefined;
    const isMomentary = !!cfg.momentary;

    const doWrite = async (value: number) => {
        if (!onWrite || isPreview) return;
        setSending(true);
        try {
            await onWrite(value);
            setFeedback('ok');
        } catch {
            setFeedback('err');
        } finally {
            setSending(false);
            setTimeout(() => setFeedback(null), 1500);
        }
    };

    const handleClick = async (e: React.MouseEvent) => {
        e.stopPropagation();
        if (isMomentary) return; // momentary is handled by pointer events
        if (!onWrite || isPreview) return;
        // Toggle: if currently active write off-value, else write on-value.
        const writeVal = active
            ? (cfg.writeOffValue ?? 0)
            : (cfg.writeValue ?? 1);
        await doWrite(writeVal);
    };

    const handlePointerDown = (e: React.PointerEvent) => {
        e.stopPropagation();
        if (!isMomentary || isPreview) return;
        (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
        doWrite(cfg.writeValue ?? 1);
    };

    const handlePointerUp = (e: React.PointerEvent) => {
        e.stopPropagation();
        if (!isMomentary || isPreview) return;
        doWrite(cfg.writeOffValue ?? 0);
    };

    const borderColor = active ? color : '#475569';
    const bgColor = active ? `${color}22` : 'rgba(15,23,42,0.8)';
    const textColor = active ? color : '#94a3b8';

    return (
        <button
            onClick={handleClick}
            onPointerDown={handlePointerDown}
            onPointerUp={handlePointerUp}
            disabled={sending || isPreview}
            className={cn(
                'w-full h-full rounded-md flex flex-col items-center justify-center gap-0.5',
                'border-2 transition-all duration-150 font-medium select-none',
                !isPreview && !sending && 'hover:brightness-110 active:scale-95',
                sending && 'opacity-60',
            )}
            style={{ borderColor, background: bgColor, color: textColor, fontSize: cfg.fontSize ?? 13 }}
        >
            {feedback === 'ok' ? (
                <span className="text-emerald-400 text-xs">✓</span>
            ) : feedback === 'err' ? (
                <span className="text-red-400 text-xs">✗</span>
            ) : (
                <>
                    <span className="truncate px-1 leading-tight">{widget.label || 'Button'}</span>
                    {!isPreview && (
                        <span className="text-[9px] opacity-60 leading-tight">{active ? 'ON' : 'OFF'}</span>
                    )}
                </>
            )}
        </button>
    );
}

function PipeSymbol({ color, pipeShape }: { color: string; pipeShape?: string }) {
    const stroke = color || '#475569';
    const shape = pipeShape || 'straight';
    if (shape === 'corner') {
        return (
            <svg viewBox="0 0 100 100" className="w-full h-full" preserveAspectRatio="xMidYMid meet">
                <path d="M 0 50 L 50 50 L 50 100" fill="none" stroke={stroke} strokeWidth="20" strokeLinecap="round" />
            </svg>
        );
    }
    if (shape === 'tee') {
        return (
            <svg viewBox="0 0 100 100" className="w-full h-full" preserveAspectRatio="xMidYMid meet">
                <path d="M 0 50 L 100 50 M 50 0 L 50 50" fill="none" stroke={stroke} strokeWidth="20" strokeLinecap="round" />
            </svg>
        );
    }
    if (shape === 'cross') {
        return (
            <svg viewBox="0 0 100 100" className="w-full h-full" preserveAspectRatio="xMidYMid meet">
                <path d="M 0 50 L 100 50 M 50 0 L 50 100" fill="none" stroke={stroke} strokeWidth="20" strokeLinecap="round" />
            </svg>
        );
    }
    // straight (default)
    return (
        <svg viewBox="0 0 100 100" className="w-full h-full" preserveAspectRatio="xMidYMid meet">
            <rect x="0" y="30" width="100" height="40" rx="20" fill={stroke} />
        </svg>
    );
}

function BargraphSymbol({ pct, color, vertical }: { pct: number; color: string; vertical: boolean }) {
    const clamped = Math.max(0, Math.min(100, pct));
    return vertical ? (
        <svg viewBox="0 0 30 100" className="w-full h-full" preserveAspectRatio="none">
            <rect x="4" y="2" width="22" height="96" rx="4" fill="#1e293b" stroke="#475569" strokeWidth="2" />
            <rect x="6" y={2 + (96 - (clamped / 100) * 92)} width="18" height={(clamped / 100) * 92} rx="2" fill={color} opacity="0.9" />
        </svg>
    ) : (
        <svg viewBox="0 0 100 30" className="w-full h-full" preserveAspectRatio="none">
            <rect x="2" y="4" width="96" height="22" rx="4" fill="#1e293b" stroke="#475569" strokeWidth="2" />
            <rect x="4" y="6" width={(clamped / 100) * 92} height="18" rx="2" fill={color} opacity="0.9" />
        </svg>
    );
}

// ── Widget dispatcher ───────────────────────────────────────────────────────

// onWrite is provided only in view mode; it writes a value to the tag via i3x.
export function SynopticWidgetView({ widget, live, onWrite }: {
    widget: SynopticWidget;
    live?: LiveValue;
    onWrite?: (value: number) => Promise<void>;
}) {
    const cfg = widget.config || {};
    const n = num(live?.value);
    const badQuality = live !== undefined && live.quality >= 2;

    switch (widget.type) {
        case 'value': {
            const color = thresholdColor(n, cfg);
            const decimals = cfg.decimals ?? 1;
            const txt = n === null
                ? (live === undefined ? '123.4' : '—')
                : n.toFixed(decimals);
            return (
                <div className="w-full h-full flex flex-col items-center justify-center rounded-md bg-slate-900/80 border border-slate-700 px-2 overflow-hidden">
                    {widget.label && <span className="text-[10px] text-slate-400 truncate w-full text-center leading-tight">{widget.label}</span>}
                    <span className="font-mono font-bold tabular-nums leading-none" style={{ color, fontSize: cfg.fontSize ?? 22 }}>
                        {txt}{cfg.unit ? <span className="text-slate-400 text-[0.6em] ml-0.5">{cfg.unit}</span> : null}
                    </span>
                    {badQuality && <span className="text-[8px] text-red-500 uppercase tracking-wider">bad</span>}
                </div>
            );
        }
        case 'indicator': {
            const on = isOn(n, cfg);
            const color = badQuality ? '#ef4444' : on ? (cfg.color || '#10b981') : '#475569';
            return (
                <div className="w-full h-full flex flex-col items-center justify-center gap-1">
                    <span className={cn('rounded-full', on && !badQuality && 'animate-pulse')}
                        style={{ width: '60%', height: '60%', maxWidth: 40, maxHeight: 40, background: color, boxShadow: `0 0 12px ${color}` }} />
                    {widget.label && <span className="text-[9px] text-slate-300 truncate w-full text-center">{widget.label}</span>}
                </div>
            );
        }
        case 'gauge':
            return <GaugeSymbol n={n} min={cfg.min ?? 0} max={cfg.max ?? 100} color={thresholdColor(n, cfg)} label={widget.label || cfg.unit || ''} decimals={cfg.decimals ?? 1} />;
        case 'tank': {
            const min = cfg.min ?? 0, max = cfg.max ?? 100;
            const pct = n === null ? (live === undefined ? 60 : 0) : ((n - min) / ((max - min) || 1)) * 100;
            return (
                <div className="w-full h-full flex flex-col">
                    <TankSymbol pct={pct} color={thresholdColor(n, cfg)} />
                    {widget.label && <span className="text-[9px] text-slate-300 text-center truncate -mt-1">{widget.label}</span>}
                </div>
            );
        }
        case 'pump':
            return <PumpSymbol on={live === undefined ? true : isOn(n, cfg)} color={cfg.color || '#10b981'} />;
        case 'valve':
            return <ValveSymbol open={live === undefined ? true : isOn(n, cfg)} color={cfg.color || '#10b981'} />;
        case 'motor':
            return <MotorSymbol on={live === undefined ? true : isOn(n, cfg)} color={cfg.color || '#10b981'} />;
        case 'pipe':
            return <PipeSymbol color={cfg.color || '#475569'} pipeShape={cfg.pipeShape as string | undefined} />;
        case 'label':
            return (
                <div className="w-full h-full flex items-center justify-center text-center"
                    style={{ color: cfg.color || '#e2e8f0', fontSize: cfg.fontSize ?? 16 }}>
                    <span className="truncate">{widget.label || 'Label'}</span>
                </div>
            );
        case 'bargraph': {
            const min = cfg.min ?? 0, max = cfg.max ?? 100;
            const pct = n === null ? (live === undefined ? 50 : 0) : ((n - min) / ((max - min) || 1)) * 100;
            return (
                <div className="w-full h-full flex flex-col">
                    <BargraphSymbol pct={pct} color={thresholdColor(n, cfg)} vertical={!!cfg.vertical} />
                    {widget.label && <span className="text-[9px] text-slate-300 text-center truncate">{widget.label}</span>}
                </div>
            );
        }
        case 'button': {
            // Button widget — clickable in view mode, decorative in edit/preview.
            const active = isOn(n, cfg);
            const color = cfg.color || '#3b82f6'; // blue-500 default
            return <ButtonWidget widget={widget} active={active} color={color} live={live} onWrite={onWrite} />;
        }
        default:
            return null;
    }
}

// Catalog used by the designer palette.
export const WIDGET_CATALOG: { type: SynopticWidget['type']; label: string; needsTag: boolean; defaultW: number; defaultH: number }[] = [
    { type: 'value',    label: 'Value',    needsTag: true,  defaultW: 110, defaultH: 56 },
    { type: 'gauge',    label: 'Gauge',    needsTag: true,  defaultW: 90,  defaultH: 90 },
    { type: 'tank',     label: 'Tank',     needsTag: true,  defaultW: 70,  defaultH: 110 },
    { type: 'bargraph', label: 'Bar',      needsTag: true,  defaultW: 120, defaultH: 30 },
    { type: 'indicator',label: 'LED',      needsTag: true,  defaultW: 50,  defaultH: 56 },
    { type: 'pump',     label: 'Pump',     needsTag: true,  defaultW: 70,  defaultH: 70 },
    { type: 'valve',    label: 'Valve',    needsTag: true,  defaultW: 70,  defaultH: 70 },
    { type: 'motor',    label: 'Motor',    needsTag: true,  defaultW: 80,  defaultH: 70 },
    { type: 'button',   label: 'Button',   needsTag: true,  defaultW: 90,  defaultH: 44 },
    { type: 'pipe',     label: 'Pipe',     needsTag: false, defaultW: 120, defaultH: 14 },
    { type: 'label',    label: 'Label',    needsTag: false, defaultW: 100, defaultH: 30 },
];
