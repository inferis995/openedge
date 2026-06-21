import { useEffect, useState } from 'react';
import { SynopticWidget } from '@/api/synoptics';
import { cn } from '@/lib/utils';

export interface LiveValue {
    value: unknown;
    quality: number;
}

const num = (v: unknown): number | null => {
    if (v === null || v === undefined || v === '') return null;
    const n = typeof v === 'boolean' ? (v ? 1 : 0) : Number(v);
    return Number.isFinite(n) ? n : null;
};

const isOn = (n: number | null, cfg: SynopticWidget['config']): boolean => {
    if (n === null) return false;
    return n >= (cfg?.onValue ?? 1);
};

// Resolve color for NUMERIC widgets: colorBands → warnAbove/critAbove → default
// If inAlarm: always returns '#ef4444' (red reserved for alarms)
const resolveNumericColor = (
    n: number | null,
    cfg: SynopticWidget['config'],
    inAlarm: boolean,
): string => {
    if (inAlarm) return '#ef4444';
    if (n === null) return '#64748b';
    const bands = cfg?.colorBands;
    if (bands && bands.length > 0) {
        const sorted = [...bands].sort((a, b) => b.above - a.above);
        for (const band of sorted) {
            if (n >= band.above) return band.color;
        }
        return '#64748b';
    }
    // Legacy warnAbove/critAbove
    if (cfg?.critAbove !== undefined && n >= cfg.critAbove) return '#ef4444';
    if (cfg?.warnAbove !== undefined && n >= cfg.warnAbove) return '#f59e0b';
    return cfg?.color || '#10b981';
};

// Resolve color for BINARY widgets
const resolveBinaryColor = (
    on: boolean,
    cfg: SynopticWidget['config'],
    inAlarm: boolean,
): string => {
    if (inAlarm) return '#ef4444';
    if (on) return cfg?.colorOn ?? cfg?.color ?? '#10b981';
    return cfg?.colorOff ?? '#475569';
};

// ── Industrial SVG symbols ──────────────────────────────────────────────────

function TankSymbol({ pct, color, horizontal }: { pct: number; color: string; horizontal?: boolean }) {
    const clamped = Math.max(0, Math.min(100, pct));
    if (horizontal) {
        const fillW = (clamped / 100) * 84;
        return (
            <svg viewBox="0 0 120 50" className="w-full h-full" preserveAspectRatio="none">
                <rect x="8" y="4" width="104" height="42" rx="8" fill="#1e293b" stroke="#475569" strokeWidth="3" />
                <rect x="11" y="7" width={fillW} height="36" rx="5" fill={color} opacity="0.85" />
                <line x1="47" y1="4" x2="47" y2="46" stroke="#475569" strokeWidth="1" opacity="0.4" />
                <line x1="73" y1="4" x2="73" y2="46" stroke="#475569" strokeWidth="1" opacity="0.4" />
            </svg>
        );
    }
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

function PumpSymbol({ on, color, spinSpeed }: { on: boolean; color: string; spinSpeed?: string }) {
    const dur = spinSpeed === 'slow' ? '4s' : spinSpeed === 'fast' ? '0.8s' : '2s';
    return (
        <svg viewBox="0 0 100 100" className="w-full h-full">
            <circle cx="50" cy="50" r="38" fill="#1e293b" stroke={on ? color : '#475569'} strokeWidth="4" />
            <g style={on ? { transformOrigin: '50px 50px', animation: `spin ${dur} linear infinite` } : {}}>
                <polygon points="50,22 50,78 78,50" fill={on ? color : '#64748b'} />
            </g>
            <rect x="2" y="44" width="16" height="12" fill="#475569" />
            <rect x="82" y="44" width="16" height="12" fill="#475569" />
            <style>{`@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }`}</style>
        </svg>
    );
}

function ValveSymbol({ open, color, valveType, positionPct }: { open: boolean; color: string; valveType?: string; positionPct?: number | null }) {
    const c = open ? color : '#ef4444';
    const pct = positionPct !== null && positionPct !== undefined ? Math.max(0, Math.min(100, positionPct)) : (open ? 100 : 0);
    if (valveType === 'ball') {
        return (
            <svg viewBox="0 0 100 100" className="w-full h-full">
                <rect x="2" y="42" width="20" height="16" fill="#475569" />
                <rect x="78" y="42" width="20" height="16" fill="#475569" />
                <circle cx="50" cy="50" r="24" fill="#1e293b" stroke={c} strokeWidth="4" />
                <rect x="40" y="40" width="20" height="20" rx="3" fill={c} opacity={0.5 + pct / 200} />
                <rect x="46" y="14" width="8" height="20" fill="#475569" />
                <rect x="34" y="8" width="32" height="8" rx="3" fill={c} />
            </svg>
        );
    }
    if (valveType === 'gate') {
        return (
            <svg viewBox="0 0 100 100" className="w-full h-full">
                <rect x="2" y="42" width="22" height="16" fill="#475569" />
                <rect x="76" y="42" width="22" height="16" fill="#475569" />
                <rect x="22" y="28" width="56" height="44" rx="4" fill="#1e293b" stroke={c} strokeWidth="3" />
                <rect x="26" y={28 + (44 - 44 * pct / 100)} width="48" height={44 * pct / 100} rx="2" fill={c} opacity="0.7" />
                <rect x="46" y="8" width="8" height="24" fill="#475569" />
            </svg>
        );
    }
    // butterfly (default)
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

function MotorSymbol({ on, color, showStatus }: { on: boolean; color: string; showStatus?: boolean }) {
    return (
        <svg viewBox="0 0 100 100" className="w-full h-full">
            <rect x="12" y="28" width="64" height="44" rx="6" fill="#1e293b" stroke={on ? color : '#475569'} strokeWidth="4" />
            <text x="44" y="58" textAnchor="middle" fontSize="28" fontWeight="bold" fill={on ? color : '#64748b'}>M</text>
            <rect x="76" y="40" width="14" height="20" fill="#475569" />
            <circle cx="44" cy="80" r="5" fill={on ? color : '#64748b'} className={on ? 'animate-pulse' : ''} />
            {showStatus && (
                <text x="50" y="95" textAnchor="middle" fontSize="8" fill={on ? color : '#64748b'}>{on ? 'RUN' : 'STOP'}</text>
            )}
        </svg>
    );
}

function GaugeSymbol({ n, min, max, color, label, decimals, cfg }: {
    n: number | null; min: number; max: number; color: string; label: string; decimals?: number;
    cfg?: SynopticWidget['config'];
}) {
    const range = max - min || 1;
    const pct = n === null ? 0 : Math.max(0, Math.min(1, (n - min) / range));
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
    const aw = cfg?.arcWidth ?? 9;
    const showTicks = cfg?.showTicks;
    const showMinMax = cfg?.showMinMax;
    const ticks = showTicks ? Array.from({ length: 11 }, (_, i) => i / 10) : [];
    return (
        <svg viewBox="0 0 100 100" className="w-full h-full">
            <path d={arc(startA, startA + sweep, 38)} fill="none" stroke="#334155" strokeWidth={aw} strokeLinecap="round" />
            {n !== null && <path d={arc(startA, endA, 38)} fill="none" stroke={color} strokeWidth={aw} strokeLinecap="round" />}
            {ticks.map((t, i) => {
                const a = startA + t * sweep;
                const [x1, y1] = polar(a, 32);
                const [x2, y2] = polar(a, 38);
                return <line key={i} x1={x1} y1={y1} x2={x2} y2={y2} stroke="#475569" strokeWidth="1" />;
            })}
            {showMinMax && <>
                <text x={polar(startA, 28)[0]} y={polar(startA, 28)[1]} textAnchor="middle" fontSize="7" fill="#94a3b8">{min}</text>
                <text x={polar(startA + sweep, 28)[0]} y={polar(startA + sweep, 28)[1]} textAnchor="middle" fontSize="7" fill="#94a3b8">{max}</text>
            </>}
            <text x="50" y="52" textAnchor="middle" fontSize="20" fontWeight="bold" fill="#e2e8f0">
                {n === null ? '—' : n.toFixed(decimals ?? 1)}
            </text>
            <text x="50" y="68" textAnchor="middle" fontSize="9" fill="#94a3b8">{label}</text>
        </svg>
    );
}

function BargraphSymbol({ pct, color, vertical, showBarValue, value, decimals, unit }: {
    pct: number; color: string; vertical: boolean;
    showBarValue?: boolean; value?: number | null; decimals?: number; unit?: string;
}) {
    const clamped = Math.max(0, Math.min(100, pct));
    const txt = showBarValue && value !== null && value !== undefined
        ? `${value.toFixed(decimals ?? 1)}${unit ? ` ${unit}` : ''}` : '';
    return vertical ? (
        <svg viewBox="0 0 30 100" className="w-full h-full" preserveAspectRatio="none">
            <rect x="4" y="2" width="22" height="96" rx="4" fill="#1e293b" stroke="#475569" strokeWidth="2" />
            <rect x="6" y={2 + (96 - (clamped / 100) * 92)} width="18" height={(clamped / 100) * 92} rx="2" fill={color} opacity="0.9" />
            {txt && <text x="15" y="52" textAnchor="middle" fontSize="6" fill="#e2e8f0" transform="rotate(-90,15,52)">{txt}</text>}
        </svg>
    ) : (
        <svg viewBox="0 0 100 30" className="w-full h-full" preserveAspectRatio="none">
            <rect x="2" y="4" width="96" height="22" rx="4" fill="#1e293b" stroke="#475569" strokeWidth="2" />
            <rect x="4" y="6" width={(clamped / 100) * 92} height="18" rx="2" fill={color} opacity="0.9" />
            {txt && <text x="50" y="19" textAnchor="middle" fontSize="8" fill="#e2e8f0">{txt}</text>}
        </svg>
    );
}

function PipeSymbol({ color, pipeShape, cfg, flowOn }: {
    color: string; pipeShape?: string; cfg?: SynopticWidget['config']; flowOn?: boolean;
}) {
    const stroke = color || '#475569';
    const sw = cfg?.strokeWidth ?? 20;
    const flowColor = cfg?.flowColor || '#ffffff';
    const shape = pipeShape || 'straight';
    const animDir = cfg?.flowDirection ?? 'right';
    const animOffset = animDir === 'left' || animDir === 'up' ? '0;20' : '20;0';
    const animId = `flow-${shape}`;

    const flowPath = (() => {
        if (shape === 'corner') return 'M 0 50 L 50 50 L 50 100';
        if (shape === 'tee') return 'M 0 50 L 100 50 M 50 0 L 50 50';
        if (shape === 'cross') return 'M 0 50 L 100 50 M 50 0 L 50 100';
        return 'M 0 50 L 100 50';
    })();

    return (
        <svg viewBox="0 0 100 100" className="w-full h-full" preserveAspectRatio="xMidYMid meet">
            {shape === 'corner' && <path d="M 0 50 L 50 50 L 50 100" fill="none" stroke={stroke} strokeWidth={sw} strokeLinecap="round" />}
            {shape === 'tee' && <path d="M 0 50 L 100 50 M 50 0 L 50 50" fill="none" stroke={stroke} strokeWidth={sw} strokeLinecap="round" />}
            {shape === 'cross' && <path d="M 0 50 L 100 50 M 50 0 L 50 100" fill="none" stroke={stroke} strokeWidth={sw} strokeLinecap="round" />}
            {shape === 'straight' && <rect x="0" y={50 - sw / 2} width="100" height={sw} rx={sw / 2} fill={stroke} />}
            {flowOn && (
                <>
                    <style>{`@keyframes ${animId} { from { stroke-dashoffset: ${animOffset.split(';')[0]}; } to { stroke-dashoffset: ${animOffset.split(';')[1]}; } }`}</style>
                    <path d={flowPath} fill="none" stroke={flowColor} strokeWidth={sw * 0.4}
                        strokeDasharray="8 8" strokeLinecap="round" opacity="0.7"
                        style={{ animation: `${animId} 0.8s linear infinite` }} />
                </>
            )}
        </svg>
    );
}

function ButtonWidget({ widget, active, color, live, onWrite, onNavigate }: {
    widget: SynopticWidget; active: boolean; color: string;
    live?: LiveValue; onWrite?: (v: number) => Promise<void>;
    onNavigate?: () => void;
}) {
    const [sending, setSending] = useState(false);
    const [feedback, setFeedback] = useState<'ok' | 'err' | null>(null);
    const [confirmPending, setConfirmPending] = useState(false);
    const cfg = widget.config || {};
    const isPreview = live === undefined;
    const isMomentary = !!cfg.momentary;
    const bShape = cfg.buttonShape as string | undefined;
    const radius = bShape === 'rect' ? 2 : bShape === 'circle' ? '50%' : 6;

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

    const getWriteVal = () => active ? (cfg.writeOffValue ?? 0) : (cfg.writeValue ?? 1);

    const handleClick = async (e: React.MouseEvent) => {
        e.stopPropagation();
        if (isMomentary) return;
        if (!onWrite && onNavigate) { onNavigate(); return; }
        if (!onWrite || isPreview) return;
        if (cfg.requireConfirm && !confirmPending) { setConfirmPending(true); return; }
        setConfirmPending(false);
        await doWrite(getWriteVal());
    };

    const handlePointerDown = (e: React.PointerEvent) => {
        e.stopPropagation();
        if (!isMomentary || isPreview) return;
        (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
        void doWrite(cfg.writeValue ?? 1);
    };
    const handlePointerUp = (e: React.PointerEvent) => {
        e.stopPropagation();
        if (!isMomentary || isPreview) return;
        void doWrite(cfg.writeOffValue ?? 0);
    };

    const borderColor = active ? color : '#475569';
    const bgColor = active ? `${color}22` : 'rgba(15,23,42,0.8)';
    const textColor = active ? color : '#94a3b8';

    if (confirmPending && !isMomentary) {
        const confirmMsg = cfg.confirmText || 'Confermare?';
        return (
            <div style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 4, borderRadius: radius, border: '2px solid #f59e0b', background: 'rgba(245,158,11,0.1)' }}>
                <span style={{ fontSize: 9, color: '#f59e0b', fontWeight: 600 }}>{confirmMsg}</span>
                <div style={{ display: 'flex', gap: 4 }}>
                    <button type="button" style={{ fontSize: 9, padding: '2px 8px', borderRadius: 4, background: '#10b981', color: 'white', border: 'none', cursor: 'pointer' }}
                        onClick={(e) => { e.stopPropagation(); setConfirmPending(false); void doWrite(getWriteVal()); }}>Sì</button>
                    <button type="button" style={{ fontSize: 9, padding: '2px 8px', borderRadius: 4, background: '#475569', color: 'white', border: 'none', cursor: 'pointer' }}
                        onClick={(e) => { e.stopPropagation(); setConfirmPending(false); }}>No</button>
                </div>
            </div>
        );
    }

    const iconMap: Record<string, string> = { play: '▶', stop: '■', power: '⏻', reset: '↺' };
    const iconChar = cfg.buttonIcon ? (iconMap[cfg.buttonIcon as string] ?? '') : '';

    return (
        <button onClick={handleClick} onPointerDown={handlePointerDown} onPointerUp={handlePointerUp}
            disabled={sending || isPreview}
            className={cn('w-full h-full flex flex-col items-center justify-center gap-0.5 border-2 transition-all duration-150 font-medium select-none',
                !isPreview && !sending && 'hover:brightness-110 active:scale-95',
                sending && 'opacity-60')}
            style={{ borderColor, background: bgColor, color: textColor, fontSize: cfg.fontSize ?? 13, borderRadius: radius }}>
            {feedback === 'ok' ? <span className="text-emerald-400 text-xs">✓</span>
                : feedback === 'err' ? <span className="text-red-400 text-xs">✗</span>
                : <>
                    {iconChar && <span style={{ fontSize: '1.2em' }}>{iconChar}</span>}
                    <span className="truncate px-1 leading-tight">{widget.label || 'Button'}</span>
                    {!isPreview && <span className="text-[9px] opacity-60 leading-tight">{active ? 'ON' : 'OFF'}</span>}
                </>}
        </button>
    );
}

function ClockWidget({ cfg }: { cfg: SynopticWidget['config'] }) {
    const [now, setNow] = useState(new Date());
    useEffect(() => {
        const id = setInterval(() => setNow(new Date()), 1000);
        return () => clearInterval(id);
    }, []);
    const use12h = cfg?.clockFormat === '12h';
    const timeStr = now.toLocaleTimeString(use12h ? 'en-US' : 'it-IT', {
        hour: '2-digit', minute: '2-digit', second: '2-digit',
        hour12: use12h,
    });
    const dateStr = now.toLocaleDateString('it-IT', { weekday: 'short', day: '2-digit', month: '2-digit', year: '2-digit' });
    const showDate = cfg?.showDate !== false;
    const textColor = cfg?.color || '#e2e8f0';
    return (
        <div className="w-full h-full flex flex-col items-center justify-center bg-slate-900/80 border border-slate-700 rounded-md px-2">
            <span style={{ fontSize: cfg?.fontSize ?? 22, color: textColor, fontFamily: 'monospace', fontWeight: 700, lineHeight: 1.1 }}>{timeStr}</span>
            {showDate && <span style={{ fontSize: cfg?.fontSize ? Math.max(9, Math.round((cfg.fontSize as number) * 0.5)) : 11, color: '#94a3b8', marginTop: 2 }}>{dateStr}</span>}
        </div>
    );
}

function SetpointWidget({ widget, live, onWrite }: {
    widget: SynopticWidget; live?: LiveValue; onWrite?: (v: number) => Promise<void>;
}) {
    const cfg = widget.config ?? {};
    const current = num(live?.value);
    const [input, setInput] = useState('');
    const [sending, setSending] = useState(false);
    const [feedback, setFeedback] = useState<'ok' | 'err' | null>(null);
    const [confirmPending, setConfirmPending] = useState<number | null>(null);
    const isPreview = live === undefined;

    const doWrite = async (v: number) => {
        if (!onWrite) return;
        setSending(true);
        try {
            await onWrite(v);
            setInput('');
            setFeedback('ok');
        } catch {
            setFeedback('err');
        } finally {
            setSending(false);
            setConfirmPending(null);
            setTimeout(() => setFeedback(null), 1500);
        }
    };

    const handleSend = async () => {
        if (!onWrite || isPreview) return;
        const v = parseFloat(input);
        if (!Number.isFinite(v)) return;
        if (cfg.spMin !== undefined && v < (cfg.spMin as number)) { setFeedback('err'); setTimeout(() => setFeedback(null), 1500); return; }
        if (cfg.spMax !== undefined && v > (cfg.spMax as number)) { setFeedback('err'); setTimeout(() => setFeedback(null), 1500); return; }
        if (cfg.confirmWrite) { setConfirmPending(v); return; }
        await doWrite(v);
    };

    const rangeHint = (cfg.spMin !== undefined || cfg.spMax !== undefined)
        ? `[${cfg.spMin ?? '—'} … ${cfg.spMax ?? '—'}]`
        : null;

    return (
        <div className="w-full h-full flex flex-col items-center justify-center gap-1 bg-slate-900/80 border border-slate-700 rounded-md px-2 overflow-hidden">
            {confirmPending !== null && (
                <div className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-1 bg-slate-950/95 rounded-md px-2">
                    <span className="text-[10px] text-amber-400 text-center">Conferma scrittura {confirmPending}{cfg.unit ? ` ${cfg.unit}` : ''}?</span>
                    <div className="flex gap-1">
                        <button type="button" onClick={() => void doWrite(confirmPending)} className="h-5 px-2 text-[10px] rounded bg-green-600 text-white">✓ Sì</button>
                        <button type="button" onClick={() => setConfirmPending(null)} className="h-5 px-2 text-[10px] rounded bg-slate-600 text-white">✗ No</button>
                    </div>
                </div>
            )}
            {widget.label && <span className="text-[10px] text-slate-400 truncate w-full text-center">{widget.label}</span>}
            <span className="text-xs text-slate-400">
                PV: <span className="text-slate-200 font-mono">
                    {current !== null ? current.toFixed(cfg.decimals ?? 1) : '—'}{cfg.unit ? ` ${cfg.unit}` : ''}
                </span>
            </span>
            {rangeHint && <span className="text-[9px] text-slate-500">{rangeHint}</span>}
            <div className="flex gap-1 w-full">
                <input type="number"
                    value={input}
                    onChange={e => setInput(e.target.value)}
                    disabled={isPreview || sending}
                    step={cfg.spStep ?? 1 as number}
                    min={cfg.spMin as number | undefined}
                    max={cfg.spMax as number | undefined}
                    onKeyDown={e => { if (e.key === 'Enter') void handleSend(); }}
                    className="flex-1 h-6 bg-slate-800 border border-slate-600 rounded text-xs text-center text-white px-1 min-w-0"
                    placeholder={cfg.unit ? String(cfg.unit) : 'SP'} />
                <button type="button" onClick={() => void handleSend()}
                    disabled={isPreview || sending || input === ''}
                    className="h-6 px-2 text-xs rounded bg-primary text-primary-foreground disabled:opacity-40 shrink-0">
                    {feedback === 'ok' ? '✓' : feedback === 'err' ? '✗' : sending ? '…' : '↵'}
                </button>
            </div>
        </div>
    );
}

function ImageWidget({ cfg }: { cfg: SynopticWidget['config'] }) {
    const url = cfg?.imageUrl;
    const opacity = cfg?.opacity !== undefined ? cfg.opacity / 100 : 1;
    if (!url) {
        return (
            <div style={{ width: '100%', height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', border: '2px dashed #475569', borderRadius: 4, background: 'rgba(15,23,42,0.5)' }}>
                <span style={{ fontSize: 11, color: '#64748b' }}>Immagine</span>
            </div>
        );
    }
    return (
        <img src={url} alt=""
            style={{ width: '100%', height: '100%', objectFit: (cfg?.imageObjectFit as 'fill' | 'contain' | 'cover') ?? 'fill', display: 'block', opacity }}
            draggable={false} />
    );
}

// ── Widget dispatcher ───────────────────────────────────────────────────────

export function SynopticWidgetView({ widget, live, onWrite, inAlarm, liveSecondary, onNavigate }: {
    widget: SynopticWidget;
    live?: LiveValue;
    onWrite?: (value: number) => Promise<void>;
    inAlarm?: boolean;
    liveSecondary?: LiveValue;
    onNavigate?: () => void;
}) {
    const cfg = widget.config || {};
    const n = num(live?.value);
    const badQuality = live !== undefined && live.quality >= 2;
    const alarm = inAlarm ?? false;
    const blinkClass = alarm && cfg.blinkOnAlarm ? 'animate-pulse' : '';

    switch (widget.type) {
        case 'value': {
            const color = resolveNumericColor(n, cfg, alarm);
            const decimals = cfg.decimals ?? 1;
            const noDataText = (cfg.noDataText as string | undefined) ?? '—';
            const txt = n === null ? (live === undefined ? '123.4' : noDataText) : n.toFixed(decimals);
            const prefix = cfg.prefix as string | undefined;
            const n2 = num(liveSecondary?.value);
            const bgColor = (cfg.bgColor as string | undefined) ?? undefined;
            const borderColor = alarm ? '#ef4444' : undefined;
            return (
                <div className={cn('w-full h-full flex flex-col items-center justify-center rounded-md border border-slate-700 px-2 overflow-hidden', blinkClass)}
                    style={{ background: bgColor ?? 'rgba(15,23,42,0.8)', borderColor }}>
                    {widget.label && <span className="text-[10px] text-slate-400 truncate w-full text-center leading-tight">{widget.label}</span>}
                    <span className="font-mono font-bold tabular-nums leading-none" style={{ color, fontSize: cfg.fontSize ?? 22 }}>
                        {prefix ? <span className="text-slate-400 text-[0.6em] mr-0.5">{prefix}</span> : null}
                        {txt}{cfg.unit ? <span className="text-slate-400 text-[0.6em] ml-0.5">{cfg.unit}</span> : null}
                    </span>
                    {n2 !== null && <span className="text-[9px] text-slate-500 font-mono">SP: {n2.toFixed(decimals)}</span>}
                    {badQuality && <span className="text-[8px] text-red-500 uppercase tracking-wider">bad</span>}
                    {cfg.showTimestamp && live !== undefined && (
                        <span className="text-[8px] text-slate-600">{new Date().toLocaleTimeString('it-IT')}</span>
                    )}
                </div>
            );
        }
        case 'indicator': {
            const on = isOn(n, cfg);
            const color = resolveBinaryColor(on, cfg, alarm);
            const shape = cfg.indicatorShape as string | undefined;
            const borderR = shape === 'square' ? '4px' : shape === 'diamond' ? '0' : '50%';
            const transform = shape === 'diamond' ? 'rotate(45deg)' : undefined;
            const labelPos = cfg.labelPosition as string | undefined;
            const dot = (
                <span className={cn(on && !alarm && 'animate-pulse')}
                    style={{ width: '55%', height: '55%', maxWidth: 40, maxHeight: 40, background: color, boxShadow: `0 0 12px ${color}`, borderRadius: borderR, transform, display: 'block' }} />
            );
            if (labelPos === 'right') {
                return (
                    <div className={cn('w-full h-full flex items-center gap-2 px-2', blinkClass)}>
                        {dot}
                        {widget.label && <span className="text-[9px] text-slate-300 truncate">{widget.label}</span>}
                    </div>
                );
            }
            if (labelPos === 'left') {
                return (
                    <div className={cn('w-full h-full flex items-center gap-2 px-2', blinkClass)}>
                        {widget.label && <span className="text-[9px] text-slate-300 truncate">{widget.label}</span>}
                        {dot}
                    </div>
                );
            }
            return (
                <div className={cn('w-full h-full flex flex-col items-center justify-center gap-1', blinkClass)}>
                    {dot}
                    {widget.label && <span className="text-[9px] text-slate-300 truncate w-full text-center">{widget.label}</span>}
                </div>
            );
        }
        case 'gauge': {
            const color = resolveNumericColor(n, cfg, alarm);
            return (
                <div className={blinkClass} style={{ width: '100%', height: '100%' }}>
                    <GaugeSymbol n={n} min={cfg.min ?? 0} max={cfg.max ?? 100} color={color}
                        label={widget.label || (cfg.showUnit ? (cfg.unit ?? '') : '')}
                        decimals={cfg.decimals ?? 1} cfg={cfg} />
                </div>
            );
        }
        case 'tank': {
            const min = cfg.min ?? 0, max = cfg.max ?? 100;
            const color = resolveNumericColor(n, cfg, alarm);
            const pct = n === null ? (live === undefined ? 60 : 0) : ((n - min) / ((max - min) || 1)) * 100;
            const clamped = Math.max(0, Math.min(100, pct));
            const showPct = cfg.showPercentage;
            const showVal = cfg.showValue;
            const isHoriz = cfg.tankOrientation === 'horizontal';
            return (
                <div className={cn('w-full h-full flex flex-col', blinkClass)}>
                    <div className="flex-1 relative">
                        <TankSymbol pct={clamped} color={color} horizontal={isHoriz} />
                        {(showPct || showVal) && n !== null && (
                            <span className="absolute inset-0 flex items-center justify-center text-xs text-white font-mono font-bold">
                                {showPct ? `${clamped.toFixed(0)}%` : `${n.toFixed(cfg.decimals ?? 1)}${cfg.unit ? ` ${cfg.unit}` : ''}`}
                            </span>
                        )}
                    </div>
                    {widget.label && <span className="text-[9px] text-slate-300 text-center truncate -mt-1">{widget.label}</span>}
                </div>
            );
        }
        case 'pump': {
            const on = live === undefined ? true : isOn(n, cfg);
            const color = resolveBinaryColor(on, cfg, alarm);
            return (
                <div className={cn('w-full h-full flex flex-col', blinkClass)}>
                    <div className="flex-1"><PumpSymbol on={on} color={color} spinSpeed={cfg.spinSpeed as string | undefined} /></div>
                    {cfg.showStatus && <span className="text-[9px] text-center" style={{ color }}>{on ? 'RUN' : 'STOP'}</span>}
                </div>
            );
        }
        case 'valve': {
            const on = live === undefined ? true : isOn(n, cfg);
            const color = resolveBinaryColor(on, cfg, alarm);
            const posTag = num(liveSecondary?.value);
            return (
                <div className={cn('w-full h-full flex flex-col', blinkClass)}>
                    <div className="flex-1">
                        <ValveSymbol open={on} color={color} valveType={cfg.valveType as string | undefined}
                            positionPct={cfg.tagPosition != null ? posTag : null} />
                    </div>
                    {cfg.showPosition && posTag !== null && (
                        <span className="text-[9px] text-center text-slate-300">{posTag.toFixed(0)}%</span>
                    )}
                </div>
            );
        }
        case 'motor': {
            const on = live === undefined ? true : isOn(n, cfg);
            const color = resolveBinaryColor(on, cfg, alarm);
            return (
                <div className={cn('w-full h-full', blinkClass)}>
                    <MotorSymbol on={on} color={color} showStatus={cfg.showStatus as boolean | undefined} />
                </div>
            );
        }
        case 'pipe': {
            const flowTagValue = num(liveSecondary?.value);
            const flowOn = cfg.flowEnabled ? (
                cfg.tagFlow != null ? isOn(flowTagValue, cfg) : true
            ) : false;
            return (
                <PipeSymbol color={cfg.color || '#475569'}
                    pipeShape={cfg.pipeShape as string | undefined}
                    cfg={cfg}
                    flowOn={live === undefined ? false : flowOn} />
            );
        }
        case 'label': {
            const boundValue = num(live?.value);
            const rawText = widget.label || 'Label';
            const displayText = cfg.tagBinding != null && boundValue !== null
                ? rawText.replace('{{value}}', boundValue.toFixed(cfg.decimals ?? 1))
                : rawText;
            const align = (cfg.textAlign as 'left' | 'center' | 'right' | undefined) ?? 'center';
            return (
                <div className="w-full h-full flex items-center justify-center"
                    style={{
                        color: cfg.color || '#e2e8f0',
                        fontSize: cfg.fontSize ?? 16,
                        fontWeight: cfg.bold ? 700 : 400,
                        fontStyle: cfg.italic ? 'italic' : 'normal',
                        textAlign: align,
                        background: (cfg.labelBgColor as string | undefined) ?? 'transparent',
                        borderRadius: 4,
                        padding: '2px 4px',
                    }}>
                    <span className="truncate">{displayText}</span>
                </div>
            );
        }
        case 'bargraph': {
            const min = cfg.min ?? 0, max = cfg.max ?? 100;
            const color = resolveNumericColor(n, cfg, alarm);
            const pct = n === null ? (live === undefined ? 50 : 0) : ((n - min) / ((max - min) || 1)) * 100;
            return (
                <div className={cn('w-full h-full flex flex-col', blinkClass)}>
                    <BargraphSymbol pct={pct} color={color} vertical={!!cfg.vertical}
                        showBarValue={cfg.showBarValue as boolean | undefined}
                        value={n} decimals={cfg.decimals ?? 1} unit={cfg.unit as string | undefined} />
                    {widget.label && <span className="text-[9px] text-slate-300 text-center truncate">{widget.label}</span>}
                </div>
            );
        }
        case 'button': {
            const on = isOn(n, cfg);
            const color = alarm ? '#ef4444' : (cfg.color || '#3b82f6');
            return <ButtonWidget widget={widget} active={on} color={color} live={live} onWrite={onWrite} onNavigate={onNavigate} />;
        }
        case 'clock':
            return <ClockWidget cfg={cfg} />;
        case 'setpoint':
            return <SetpointWidget widget={widget} live={live} onWrite={onWrite} />;
        case 'image':
            return <ImageWidget cfg={cfg} />;
        default:
            return null;
    }
}

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
    { type: 'setpoint', label: 'Setpoint', needsTag: true,  defaultW: 110, defaultH: 70 },
    { type: 'pipe',     label: 'Pipe',     needsTag: false, defaultW: 120, defaultH: 14 },
    { type: 'label',    label: 'Label',    needsTag: false, defaultW: 100, defaultH: 30 },
    { type: 'image',    label: 'Image',    needsTag: false, defaultW: 200, defaultH: 150 },
    { type: 'clock',    label: 'Clock',    needsTag: false, defaultW: 130, defaultH: 56 },
];
