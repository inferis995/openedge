import { useEffect, useRef, useCallback, useMemo } from 'react';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';

export interface PenSeries {
    tagId: number;
    label: string;
    unit?: string;
    color: string;
    visible: boolean;
    width?: number;
}

interface Props {
    /** Unix seconds timestamps (uPlot native) */
    timestamps: number[];
    /** One Float64Array (or null-padded number[]) per pen, aligned with timestamps */
    values: (number | null | undefined)[][];
    pens: PenSeries[];
    height?: number;
    /** Called when the user drag-selects a sub-range to zoom */
    onZoom?: (fromMs: number, toMs: number) => void;
}

const GRID_COLOR = 'rgba(128,128,128,0.15)';
const TICK_COLOR = 'rgba(128,128,128,0.4)';
const TEXT_COLOR = '#94a3b8';

function buildTooltipPlugin(pens: PenSeries[]): uPlot.Plugin {
    let tooltip: HTMLDivElement;

    return {
        hooks: {
            init: (u) => {
                tooltip = document.createElement('div');
                tooltip.className = [
                    'absolute z-50 pointer-events-none hidden',
                    'bg-popover border border-border rounded-md shadow-lg',
                    'text-[11px] text-popover-foreground px-2.5 py-2 min-w-[140px]',
                ].join(' ');
                u.over.appendChild(tooltip);
            },
            setCursor: (u) => {
                const { left, top, idx } = u.cursor;
                if (idx === undefined || idx === null || left === undefined) {
                    tooltip.classList.add('hidden');
                    return;
                }
                const ts = u.data[0][idx];
                if (ts === undefined) { tooltip.classList.add('hidden'); return; }

                const date = new Date(ts * 1000);
                const timeStr = date.toLocaleString('it-IT', {
                    day: '2-digit', month: '2-digit',
                    hour: '2-digit', minute: '2-digit', second: '2-digit',
                });

                // Built with the DOM API rather than innerHTML: pen.label and
                // pen.unit come from tag aliases, which are attacker-influenced
                // (an admin names a tag, and the LoRaWAN import derives aliases
                // from device-supplied fields). Interpolating them into HTML was
                // stored XSS, and since the JWT lives in localStorage the payload
                // could exfiltrate an operator's — or a global admin's — session.
                tooltip.replaceChildren();

                const header = document.createElement('div');
                header.className = 'font-semibold text-foreground mb-1.5 border-b border-border pb-1';
                header.textContent = timeStr;
                tooltip.appendChild(header);

                for (let i = 0; i < pens.length; i++) {
                    const pen = pens[i];
                    if (!pen.visible) continue;
                    const raw = u.data[i + 1]?.[idx];
                    const val = raw === null || raw === undefined ? '—' :
                        Number.isFinite(raw) ? raw.toFixed(3).replace(/\.?0+$/, '') : '—';

                    const row = document.createElement('div');
                    row.className = 'flex items-center gap-1.5 mt-0.5';

                    const swatch = document.createElement('span');
                    swatch.style.cssText = 'width:10px;height:10px;border-radius:2px;flex-shrink:0;display:inline-block';
                    swatch.style.background = pen.color;
                    row.appendChild(swatch);

                    const label = document.createElement('span');
                    label.className = 'text-muted-foreground truncate max-w-[80px]';
                    label.textContent = pen.label;
                    row.appendChild(label);

                    const value = document.createElement('span');
                    value.className = 'ml-auto font-mono font-medium';
                    value.textContent = `${val}${pen.unit ? ' ' + pen.unit : ''}`;
                    row.appendChild(value);

                    tooltip.appendChild(row);
                }
                tooltip.classList.remove('hidden');

                const overRect = u.over.getBoundingClientRect();
                const ttW = tooltip.offsetWidth || 160;
                const ttH = tooltip.offsetHeight || 80;
                const tLeft = (left ?? 0) + 14;
                const tTop = (top ?? 0) - ttH / 2;
                tooltip.style.left = (tLeft + ttW > overRect.width ? tLeft - ttW - 28 : tLeft) + 'px';
                tooltip.style.top = Math.max(0, Math.min(tTop, overRect.height - ttH)) + 'px';
            },
            setData: () => { tooltip?.classList.add('hidden'); },
        },
    };
}

export function UPlotChart({ timestamps, values, pens, height = 420, onZoom }: Props) {
    const containerRef = useRef<HTMLDivElement>(null);
    const plotRef = useRef<uPlot | null>(null);

    // Align data: [timestamps, ...series_values]
    const data = useMemo<uPlot.AlignedData>(() => {
        const aligned: uPlot.AlignedData = [new Float64Array(timestamps)];
        for (const v of values) {
            // uPlot accepts null for gaps
            aligned.push(v as unknown as uPlot.TypedArray);
        }
        return aligned;
    }, [timestamps, values]);

    const buildOptions = useCallback((width: number): uPlot.Options => {
        // Build one scale per pen (so each can auto-scale independently)
        const scales: uPlot.Scales = {
            x: {},
            // one shared y scale is fine for most cases; advanced: per-tag scales
            y: {},
        };

        const series: uPlot.Series[] = [
            // x (time) series — required first entry
            {},
            // one entry per pen
            ...pens.map((pen) => ({
                label: pen.label,
                stroke: pen.color,
                width: pen.width ?? 2,
                show: pen.visible,
                spanGaps: true,
                points: {
                    show: false,
                },
                scale: 'y',
            } satisfies uPlot.Series)),
        ];

        const opts: uPlot.Options = {
            width,
            height,
            scales,
            series,
            plugins: [buildTooltipPlugin(pens)],
            cursor: {
                sync: { key: 'historian-sync' },
                drag: { x: true, y: false },
            },
            axes: [
                {
                    // X axis
                    stroke: TEXT_COLOR,
                    ticks: { stroke: TICK_COLOR, width: 1, size: 4 },
                    grid: { stroke: GRID_COLOR, width: 1 },
                    font: '11px Inter, system-ui, sans-serif',
                    labelFont: '11px Inter, system-ui, sans-serif',
                },
                {
                    // Y axis
                    stroke: TEXT_COLOR,
                    ticks: { stroke: TICK_COLOR, width: 1, size: 4 },
                    grid: { stroke: GRID_COLOR, width: 1 },
                    font: '11px Inter, system-ui, sans-serif',
                    size: 60,
                },
            ],
            hooks: {
                setSelect: [
                    (u) => {
                        if (u.select.width > 0 && onZoom) {
                            const min = u.posToVal(u.select.left, 'x');
                            const max = u.posToVal(u.select.left + u.select.width, 'x');
                            onZoom(Math.round(min * 1000), Math.round(max * 1000));
                            u.setSelect({ left: 0, width: 0, top: 0, height: 0 }, false);
                        }
                    },
                ],
            },
        };
        return opts;
    }, [pens, height, onZoom]);

    // Create / recreate when pens config changes
    useEffect(() => {
        if (!containerRef.current) return;
        const w = containerRef.current.clientWidth || 800;

        plotRef.current?.destroy();
        plotRef.current = new uPlot(buildOptions(w), data, containerRef.current);

        return () => {
            plotRef.current?.destroy();
            plotRef.current = null;
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [pens, height, onZoom]);

    // Update data without recreating chart
    useEffect(() => {
        if (plotRef.current) {
            plotRef.current.setData(data);
        }
    }, [data]);

    // Resize observer
    useEffect(() => {
        if (!containerRef.current) return;
        const ro = new ResizeObserver((entries) => {
            const w = entries[0].contentRect.width;
            if (plotRef.current && w > 0) {
                plotRef.current.setSize({ width: w, height });
            }
        });
        ro.observe(containerRef.current);
        return () => ro.disconnect();
    }, [height]);

    return (
        <div ref={containerRef} className="w-full overflow-hidden" />
    );
}
