import { useEffect, useRef, useMemo } from 'react';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import i18n from '@/i18n';

export interface PenSeries {
    tagId: number;
    label: string;
    unit?: string;
    /** Decimals shown in the tooltip (the tag's eu_decimals); default 3 significant. */
    decimals?: number;
    color: string;
    visible: boolean;
    width?: number;
    /** Digital signal: drawn as steps (a BOOL is 0 until it is 1, never 0.5). */
    step?: boolean;
}

interface Props {
    /** Unix seconds timestamps (uPlot native) */
    timestamps: number[];
    /**
     * One array per pen, aligned with timestamps. `undefined` = this pen has
     * no sample at that instant (the line joins across it); `null` = a real
     * gap (bad quality, or no data for a long stretch: the line breaks).
     */
    values: (number | null | undefined)[][];
    pens: PenSeries[];
    /**
     * Per pen, the min/max envelope of bucketed values (aligned with
     * timestamps), or null. Drawn as a shaded band around the curve.
     */
    bands?: ({ min: (number | undefined)[]; max: (number | undefined)[] } | null)[];
    /** The window the user chose, in ms. The x axis always shows exactly this. */
    range: { from: number; to: number };
    /**
     * Live: the window keeps the length of `range` and its right edge is now,
     * moving continuously (a pixel at a time) rather than in steps.
     */
    live?: boolean;
    /** Called when the user selects (mouse drag, or pinch) a sub-range to zoom */
    onZoom?: (fromMs: number, toMs: number) => void;
}

const GRID_COLOR = 'rgba(128,128,128,0.15)';
const TICK_COLOR = 'rgba(128,128,128,0.4)';
const TEXT_COLOR = '#94a3b8';
const FONT = '11px Inter, system-ui, sans-serif';

/** One y scale per unit, so bar, l/s and V are not squeezed onto one axis. */
function scaleKey(pen: PenSeries): string {
    return 'y:' + (pen.unit?.trim() || `#${pen.tagId}`);
}

function fmtValue(v: number, decimals?: number): string {
    if (decimals !== undefined && decimals >= 0 && decimals <= 10) return v.toFixed(decimals);
    return v.toFixed(3).replace(/\.?0+$/, '');
}

/** Time labels in the user's language, at a granularity that suits the span. */
function timeLabels(splits: number[], spanSec: number): string[] {
    const lang = i18n.language;
    let prevDay = '';
    return splits.map((s) => {
        const d = new Date(s * 1000);
        const day = d.toLocaleDateString(lang, { day: '2-digit', month: '2-digit' });
        if (spanSec > 3 * 86400) return day;
        const time = d.toLocaleTimeString(lang, {
            hour: '2-digit', minute: '2-digit', ...(spanSec <= 600 ? { second: '2-digit' } : {}),
        });
        // The date is written when it changes, so a 24 h window reads as a day.
        const label = day !== prevDay && spanSec > 6 * 3600 ? `${time}\n${day}` : time;
        prevDay = day;
        return label;
    });
}

function tooltipPlugin(pens: PenSeries[]): uPlot.Plugin {
    let tooltip: HTMLDivElement;
    return {
        hooks: {
            init: (u) => {
                tooltip = document.createElement('div');
                tooltip.className = [
                    'absolute z-50 pointer-events-none hidden',
                    'bg-popover border border-border rounded-md shadow-lg',
                    'text-[11px] text-popover-foreground px-2.5 py-2 min-w-[140px] max-w-[260px]',
                ].join(' ');
                u.over.appendChild(tooltip);
            },
            setCursor: (u) => {
                const { left, top, idx } = u.cursor;
                if (idx === undefined || idx === null || left === undefined || left < 0) {
                    tooltip.classList.add('hidden');
                    return;
                }
                const ts = u.data[0][idx];
                if (ts === undefined) { tooltip.classList.add('hidden'); return; }

                // Built with the DOM API rather than innerHTML: pen labels and
                // units come from tag aliases, which are attacker-influenced
                // (an admin names a tag, and imports derive aliases from
                // device-supplied fields). Interpolating them into HTML was
                // stored XSS, and the JWT lives in localStorage.
                tooltip.replaceChildren();
                const header = document.createElement('div');
                header.className = 'font-semibold text-foreground mb-1.5 border-b border-border pb-1';
                header.textContent = new Date(ts * 1000).toLocaleString(i18n.language, {
                    day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
                });
                tooltip.appendChild(header);

                for (let i = 0; i < pens.length; i++) {
                    const pen = pens[i];
                    if (!pen.visible) continue;
                    // The value at the cursor, or this pen's nearest earlier
                    // sample: pens sampled at different instants would
                    // otherwise read "—" at most positions.
                    const col = u.data[i + 1];
                    let j = idx;
                    while (j >= 0 && col?.[j] === undefined) j--;
                    const raw = j >= 0 ? col?.[j] : undefined;
                    const val = raw === null || raw === undefined || !Number.isFinite(raw) ? '—' : fmtValue(raw, pen.decimals);

                    const row = document.createElement('div');
                    row.className = 'flex items-center gap-1.5 mt-0.5';
                    const swatch = document.createElement('span');
                    swatch.style.cssText = 'width:10px;height:10px;border-radius:2px;flex-shrink:0;display:inline-block';
                    swatch.style.background = pen.color;
                    row.appendChild(swatch);
                    const label = document.createElement('span');
                    label.className = 'text-muted-foreground truncate max-w-[120px]';
                    label.textContent = pen.label;
                    row.appendChild(label);
                    const value = document.createElement('span');
                    value.className = 'ml-auto pl-2 font-mono font-medium whitespace-nowrap';
                    value.textContent = `${val}${pen.unit && val !== '—' ? ' ' + pen.unit : ''}`;
                    row.appendChild(value);
                    tooltip.appendChild(row);
                    // A bucket's extremes, when this value stands for several samples.
                    const n = pens.length;
                    const hi = j >= 0 ? u.data[n + 1 + 2 * i]?.[j] : undefined;
                    const lo = j >= 0 ? u.data[n + 2 + 2 * i]?.[j] : undefined;
                    if (hi !== undefined && hi !== null && lo !== undefined && lo !== null) {
                        const ext = document.createElement('div');
                        ext.className = 'pl-[16px] text-[10px] text-muted-foreground font-mono';
                        ext.textContent = `min ${fmtValue(lo, pen.decimals)} · max ${fmtValue(hi, pen.decimals)}`;
                        tooltip.appendChild(ext);
                    }
                }
                tooltip.classList.remove('hidden');

                const w = u.over.clientWidth;
                const h = u.over.clientHeight;
                const ttW = tooltip.offsetWidth || 160;
                const ttH = tooltip.offsetHeight || 80;
                const x = left + 14 + ttW > w ? left - ttW - 14 : left + 14;
                tooltip.style.left = Math.max(0, x) + 'px';
                tooltip.style.top = Math.max(0, Math.min((top ?? 0) - ttH / 2, h - ttH)) + 'px';
            },
            setData: () => { tooltip?.classList.add('hidden'); },
        },
    };
}

/**
 * Touch: one finger moves the cursor (and its tooltip) along the curves, two
 * fingers pinch to zoom the time window. uPlot listens to the mouse only, so
 * on a phone a tap did nothing and a drag scrolled the page.
 */
function touchPlugin(onZoom?: (fromMs: number, toMs: number) => void): uPlot.Plugin {
    return {
        hooks: {
            init: (u) => {
                const over = u.over;
                let pinch: { d0: number; mid: number; min: number; max: number } | null = null;
                const rel = (t: Touch) => {
                    const r = over.getBoundingClientRect();
                    return { x: t.clientX - r.left, y: t.clientY - r.top };
                };
                const onStart = (e: TouchEvent) => {
                    if (e.touches.length === 2) {
                        const a = rel(e.touches[0]), b = rel(e.touches[1]);
                        pinch = {
                            d0: Math.abs(a.x - b.x) || 1,
                            mid: u.posToVal((a.x + b.x) / 2, 'x'),
                            min: u.scales.x.min ?? 0,
                            max: u.scales.x.max ?? 0,
                        };
                    } else {
                        const p = rel(e.touches[0]);
                        u.setCursor({ left: p.x, top: p.y });
                    }
                };
                const onMove = (e: TouchEvent) => {
                    e.preventDefault(); // the finger is on the chart: do not scroll the page
                    if (pinch && e.touches.length === 2) {
                        const a = rel(e.touches[0]), b = rel(e.touches[1]);
                        const k = pinch.d0 / (Math.abs(a.x - b.x) || 1);
                        const span = (pinch.max - pinch.min) * k;
                        const f = (pinch.mid - pinch.min) / (pinch.max - pinch.min || 1);
                        u.setScale('x', { min: pinch.mid - span * f, max: pinch.mid + span * (1 - f) });
                        return;
                    }
                    const p = rel(e.touches[0]);
                    u.setCursor({ left: p.x, top: p.y });
                };
                const onEnd = (e: TouchEvent) => {
                    if (pinch && e.touches.length < 2) {
                        const min = u.scales.x.min, max = u.scales.x.max;
                        pinch = null;
                        if (onZoom && min !== undefined && max !== undefined && max > min) {
                            onZoom(Math.round(min * 1000), Math.round(max * 1000));
                        }
                    }
                };
                over.addEventListener('touchstart', onStart, { passive: true });
                over.addEventListener('touchmove', onMove, { passive: false });
                over.addEventListener('touchend', onEnd, { passive: true });
                over.style.touchAction = 'none';
            },
        },
    };
}

/**
 * Last value held: from each pen's newest good sample to the right edge of the
 * window (or now, if the window reaches past it), a dashed line at that value.
 * A historian shows a value until the next one, and a logger reporting every
 * six hours, or a value that has not changed, otherwise looked like data that
 * stopped — in live mode the curve simply ended while the window moved on.
 */
function holdLastPlugin(pens: PenSeries[]): uPlot.Plugin {
    return {
        hooks: {
            draw: (u) => {
                const ctx = u.ctx;
                const xs = u.data[0];
                const xMax = Math.min(u.scales.x.max ?? 0, Date.now() / 1000);
                ctx.save();
                ctx.beginPath();
                ctx.rect(u.bbox.left, u.bbox.top, u.bbox.width, u.bbox.height);
                ctx.clip();
                for (let i = 0; i < pens.length; i++) {
                    const s = u.series[i + 1];
                    if (!s?.show) continue;
                    const col = u.data[i + 1];
                    if (!col) continue;
                    let j = col.length - 1;
                    while (j >= 0 && col[j] === undefined) j--;
                    const v = j >= 0 ? col[j] : null;
                    if (v === null || v === undefined || xs[j] >= xMax) continue; // last cell bad, or nothing to extend
                    const y = u.valToPos(v, s.scale ?? 'y', true);
                    ctx.beginPath();
                    ctx.setLineDash([5 * devicePixelRatio, 4 * devicePixelRatio]);
                    ctx.lineWidth = Math.max(1, (pens[i].width ?? 2) - 0.5) * devicePixelRatio;
                    ctx.strokeStyle = pens[i].color;
                    ctx.globalAlpha = 0.55;
                    ctx.moveTo(u.valToPos(xs[j], 'x', true), y);
                    ctx.lineTo(u.valToPos(xMax, 'x', true), y);
                    ctx.stroke();
                }
                ctx.restore();
            },
        },
    };
}

/**
 * The historian chart. Fills its parent: give the parent a height (flex-1,
 * h-full…) and the chart follows it, on resize and on rotation.
 */
export function UPlotChart({ timestamps, values, pens, bands, range, live = false, onZoom }: Props) {
    const containerRef = useRef<HTMLDivElement>(null);
    const plotRef = useRef<uPlot | null>(null);
    // The latest window, read by uPlot's callbacks (axis labels) between renders.
    const rangeRef = useRef(range);

    // One column per pen, always: pens restored from the address are there
    // before their data, and uPlot (and the plugins) read a column per series.
    const data = useMemo<uPlot.AlignedData>(() => {
        const aligned: uPlot.AlignedData = [new Float64Array(timestamps)];
        const empty = () => new Array(timestamps.length).fill(undefined);
        for (let i = 0; i < pens.length; i++) {
            aligned.push((values[i] ?? empty()) as unknown as uPlot.TypedArray);
        }
        // Two more columns per pen, max then min, after all the pens: the
        // series built for them are invisible, uPlot fills between them.
        for (let i = 0; i < pens.length; i++) {
            aligned.push((bands?.[i]?.max ?? empty()) as unknown as uPlot.TypedArray);
            aligned.push((bands?.[i]?.min ?? empty()) as unknown as uPlot.TypedArray);
        }
        return aligned;
    }, [timestamps, values, bands, pens.length]);

    // Units in display order: the first goes left, the second right, then
    // alternating, so two units read like a classic two-axis trend.
    const units = useMemo(() => {
        const seen = new Map<string, PenSeries>();
        for (const p of pens) if (!seen.has(scaleKey(p))) seen.set(scaleKey(p), p);
        return [...seen.entries()];
    }, [pens]);

    useEffect(() => {
        const el = containerRef.current;
        if (!el) return;

        const scales: uPlot.Scales = {
            // Always exactly the window the user chose — never derived from
            // the data, which put "2027 2028 2029" under a single sample.
            // (auto: false, set right after creation and on every change.)
            x: { time: true, auto: false },
        };
        for (const [key] of units) {
            scales[key] = {
                range: (_u, min, max) => {
                    if (min === null || max === null || !Number.isFinite(min) || !Number.isFinite(max)) return [0, 1];
                    if (min === max) { const pad = Math.abs(min) * 0.1 || 1; return [min - pad, max + pad]; }
                    const pad = (max - min) * 0.08;
                    return [min - pad, max + pad];
                },
            };
        }

        const series: uPlot.Series[] = [
            {},
            ...pens.map((pen) => ({
                label: pen.label,
                stroke: pen.color,
                width: pen.width ?? 2,
                show: pen.visible,
                scale: scaleKey(pen),
                ...(pen.step ? { paths: uPlot.paths.stepped?.({ align: 1 }) } : {}),
                // undefined (no sample here) joins; null (bad / outage) breaks.
                spanGaps: false,
                // Markers when the points are far apart — a logger reporting
                // every few hours has one or two samples in the window, and a
                // single sample drew nothing at all.
                points: {
                    show: (u: uPlot, sidx: number) => {
                        const col = u.data[sidx];
                        if (!col) return false;
                        let n = 0;
                        for (let i = 0; i < col.length; i++) if (col[i] !== undefined && col[i] !== null) n++;
                        return n > 0 && n <= Math.max(2, u.bbox.width / devicePixelRatio / 12);
                    },
                    size: 6,
                    fill: pen.color,
                },
                value: (_u: uPlot, v: number | null) => (v === null || v === undefined ? '—' : fmtValue(v, pen.decimals)),
            } as uPlot.Series)),
            ...pens.flatMap((pen) => [0, 1].map(() => ({
                scale: scaleKey(pen),
                // The envelope's edges, faint: a spike one bucket wide is
                // narrower than a pixel and its fill alone would not show.
                stroke: pen.color + '73',
                width: 1,
                show: pen.visible,
                points: { show: false },
                spanGaps: false,
            } as uPlot.Series))),
        ];
        const n = pens.length;
        const bandOpts: uPlot.Band[] = pens.map((pen, i) => ({
            series: [n + 1 + 2 * i, n + 2 + 2 * i] as [number, number],
            fill: pen.color + '2e',
        }));

        const axes: uPlot.Axis[] = [{
            stroke: TEXT_COLOR,
            ticks: { stroke: TICK_COLOR, width: 1, size: 4 },
            grid: { stroke: GRID_COLOR, width: 1 },
            font: FONT,
            space: 70,
            size: 44,
            values: (_u, splits) => timeLabels(splits, (rangeRef.current.to - rangeRef.current.from) / 1000),
        }];
        // On a narrow screen a third axis took the width the curves need:
        // two are drawn, the others' values stay in the tooltip.
        const maxAxes = el.clientWidth < 640 ? 2 : 4;
        units.forEach(([key, first], i) => {
            axes.push({
                show: i < maxAxes,
                space: 36,
                scale: key,
                side: i % 2 === 0 ? 3 : 1,
                stroke: units.length > 1 ? first.color : TEXT_COLOR,
                ticks: { stroke: TICK_COLOR, width: 1, size: 4 },
                grid: { stroke: GRID_COLOR, width: 1, show: i === 0 },
                font: FONT,
                label: first.unit || undefined,
                labelFont: FONT,
                labelSize: first.unit ? 16 : 0,
                size: 52,
                values: (_u, splits) => splits.map((v) => {
                    const a = Math.abs(v);
                    if (a >= 10000) return (v / 1000).toFixed(0) + 'k';
                    if (a >= 1000) return (v / 1000).toFixed(1) + 'k';
                    return Number(v.toFixed(3)).toString();
                }),
            });
        });

        const opts: uPlot.Options = {
            width: el.clientWidth || 300,
            height: el.clientHeight || 300,
            scales,
            series,
            bands: bandOpts,
            axes,
            legend: { show: false },
            plugins: [tooltipPlugin(pens), touchPlugin(onZoom), holdLastPlugin(pens)],
            cursor: { sync: { key: 'historian-sync' }, drag: { x: true, y: false } },
            hooks: {
                setSelect: [(u) => {
                    if (u.select.width > 0 && onZoom) {
                        const min = u.posToVal(u.select.left, 'x');
                        const max = u.posToVal(u.select.left + u.select.width, 'x');
                        onZoom(Math.round(min * 1000), Math.round(max * 1000));
                        u.setSelect({ left: 0, width: 0, top: 0, height: 0 }, false);
                    }
                }],
            },
        };

        plotRef.current?.destroy();
        plotRef.current = new uPlot(opts, data, el);
        plotRef.current.setScale('x', { min: rangeRef.current.from / 1000, max: rangeRef.current.to / 1000 });

        // Follow the parent's size in both directions (rotation, panels opening).
        const ro = new ResizeObserver(() => {
            const w = el.clientWidth, h = el.clientHeight;
            if (plotRef.current && w > 0 && h > 0) plotRef.current.setSize({ width: w, height: h });
        });
        ro.observe(el);
        return () => {
            ro.disconnect();
            plotRef.current?.destroy();
            plotRef.current = null;
        };
        // data is pushed by the effect below; rebuilding on it would reset the cursor.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [pens, units, onZoom]);

    // The window is applied again with the data: set on a chart that had no
    // data yet (pens restored from the address, before their history came),
    // it was dropped, and the chart stayed blank with no time axis.
    const liveRef = useRef(live);
    useEffect(() => { liveRef.current = live; }, [live]);
    useEffect(() => {
        const u = plotRef.current;
        if (!u) return;
        u.setData(data);
        const r = rangeRef.current;
        const span = r.to - r.from;
        const to = liveRef.current ? Date.now() : r.to;
        u.setScale('x', { min: (to - span) / 1000, max: to / 1000 });
    }, [data]);

    // A new window with the same data still has to move the axis.
    useEffect(() => {
        rangeRef.current = { from: range.from, to: range.to };
        if (!live) plotRef.current?.setScale('x', { min: range.from / 1000, max: range.to / 1000 });
    }, [range.from, range.to, live]);

    // Live: slide the window with the clock. One redraw per pixel of time
    // (a 1 h window 1000 px wide moves every 3.6 s, a 5 min one every 0.3 s),
    // never faster than 20 a second, and none while the tab is hidden.
    useEffect(() => {
        if (!live) return;
        let raf = 0;
        let last = 0;
        const tick = (t: number) => {
            const u = plotRef.current;
            if (u && !document.hidden) {
                const span = rangeRef.current.to - rangeRef.current.from;
                const pxWidth = Math.max(1, u.bbox.width / devicePixelRatio);
                if (t - last >= Math.max(50, span / pxWidth)) {
                    last = t;
                    const now = Date.now();
                    u.setScale('x', { min: (now - span) / 1000, max: now / 1000 });
                }
            }
            raf = requestAnimationFrame(tick);
        };
        raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
    }, [live]);

    return <div ref={containerRef} className="w-full h-full min-h-[220px] overflow-hidden relative" />;
}
