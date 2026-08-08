import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
    ArrowLeft, Save, Trash2, Loader2, Pencil, Monitor, Check, AlertTriangle, Copy,
    Lock, Unlock, AlignLeft, AlignCenterHorizontal, AlignRight,
    AlignStartVertical, AlignCenterVertical, AlignEndVertical,
    AlignHorizontalDistributeCenter, AlignVerticalDistributeCenter,
    Maximize2, ChevronsUpDown,
} from 'lucide-react';
import { synopticsApi, Synoptic, SynopticWidget } from '@/api/synoptics';
import { tagsApi } from '@/api/tags';
import { alarmsApi } from '@/api/alarms';
import { i3xApi } from '@/api/i3x';
import { TagWithHierarchy } from '@/types/trend';
import { useRealtime } from '@/hooks/useRealtime';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useAuthStore } from '@/stores/useAuthStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import {
    Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList,
} from '@/components/ui/command';
import { cn } from '@/lib/utils';
import { SynopticWidgetView, WIDGET_CATALOG, LiveValue } from '@/components/synoptics/SynopticWidget';

// Searchable tag combobox for the properties panel
function TagCombobox({
    tags,
    value,
    onChange,
}: {
    tags: TagWithHierarchy[];
    value: string;
    onChange: (val: string) => void;
}) {
    const [open, setOpen] = useState(false);
    const current = value === 'none' ? null : tags.find(t => String(t.id) === value);
    const label = current ? (current.alias || current.code) : 'Nessun tag';

    return (
        <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
                <Button variant="outline" role="combobox" className="h-8 text-xs w-full justify-between font-normal">
                    <span className="truncate">{label}</span>
                    <ChevronsUpDown className="ml-1 h-3 w-3 shrink-0 opacity-50" />
                </Button>
            </PopoverTrigger>
            <PopoverContent className="w-64 p-0" align="start">
                <Command>
                    <CommandInput placeholder="Cerca tag…" className="h-8 text-xs" />
                    <CommandList>
                        <CommandEmpty className="text-xs">Nessun tag trovato.</CommandEmpty>
                        <CommandGroup>
                            <CommandItem value="none" onSelect={() => { onChange('none'); setOpen(false); }}
                                className="text-xs">
                                Nessun tag
                            </CommandItem>
                            {tags.map(t => (
                                <CommandItem key={t.id} value={`${t.alias || t.code} ${t.code}`}
                                    onSelect={() => { onChange(String(t.id)); setOpen(false); }}
                                    className="text-xs">
                                    <span className="truncate">{t.alias || t.code}</span>
                                    {t.scaling_enabled && t.eu_unit && (
                                        <span className="ml-1 text-[10px] text-muted-foreground">[{t.eu_unit}]</span>
                                    )}
                                </CommandItem>
                            ))}
                        </CommandGroup>
                    </CommandList>
                </Command>
            </PopoverContent>
        </Popover>
    );
}

const uid = () => Math.random().toString(36).slice(2, 10);
const SNAP = 8; // grid snap size in canvas-pixels

const snap = (v: number) => Math.round(v / SNAP) * SNAP;

interface DragState {
    startX: number;
    startY: number;
    origPositions: Map<string, { x: number; y: number }>;
}

interface ResizeState {
    handleId: string;
    startX: number;
    startY: number;
    origX: number;
    origY: number;
    origW: number;
    origH: number;
    widgetId: string;
}

const RESIZE_HANDLES: { id: string; xF: number; yF: number; cursor: string }[] = [
    { id: 'nw', xF: 0,   yF: 0,   cursor: 'nw-resize' },
    { id: 'n',  xF: 0.5, yF: 0,   cursor: 'n-resize'  },
    { id: 'ne', xF: 1,   yF: 0,   cursor: 'ne-resize' },
    { id: 'e',  xF: 1,   yF: 0.5, cursor: 'e-resize'  },
    { id: 'se', xF: 1,   yF: 1,   cursor: 'se-resize' },
    { id: 's',  xF: 0.5, yF: 1,   cursor: 's-resize'  },
    { id: 'sw', xF: 0,   yF: 1,   cursor: 'sw-resize' },
    { id: 'w',  xF: 0,   yF: 0.5, cursor: 'w-resize'  },
];

const SynopticEditorPage = ({ mode }: { mode: 'view' | 'edit' }) => {
    const { id } = useParams<{ id: string }>();
    const navigate = useNavigate();
    const { isAdmin } = useAuthStore();
    const { selectedOrgId } = useNavigationStore();
    const [synoptic, setSynoptic] = useState<Synoptic | null>(null);
    const [widgets, setWidgets] = useState<SynopticWidget[]>([]);
    const [tags, setTags] = useState<TagWithHierarchy[]>([]);
    const [synopticList, setSynopticList] = useState<Array<{id: number; name: string}>>([]);
    const [selectedIds, setSelectedIds] = useState<string[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isSaving, setIsSaving] = useState(false);
    const [saveStatus, setSaveStatus] = useState<'idle' | 'ok' | 'error'>('idle');
    const [loadError, setLoadError] = useState<string | null>(null);
    const [scale, setScale] = useState(1);
    const scaleRef = useRef(scale);

    const isEdit = mode === 'edit';
    const canvasWrapRef = useRef<HTMLDivElement>(null);
    const dragRef = useRef<DragState | null>(null);
    const resizeRef = useRef<ResizeState | null>(null);

    // Keep scaleRef in sync so pinch-zoom closure can read the current value
    useEffect(() => { scaleRef.current = scale; }, [scale]);

    // Undo/Redo history
    const historyRef = useRef<SynopticWidget[][]>([]);
    const historyIdxRef = useRef<number>(-1);

    const pushHistory = useCallback((state: SynopticWidget[]) => {
        historyRef.current = historyRef.current.slice(0, historyIdxRef.current + 1);
        historyRef.current.push(JSON.parse(JSON.stringify(state)));
        if (historyRef.current.length > 50) historyRef.current.shift();
        else historyIdxRef.current++;
    }, []);

    // single selected widget (null when 0 or 2+ selected)
    const selected = useMemo(
        () => selectedIds.length === 1 ? (widgets.find(w => w.id === selectedIds[0]) ?? null) : null,
        [widgets, selectedIds],
    );

    const canvasTagIds = useMemo(() => {
        const ids = new Set<number>();
        for (const w of widgets) {
            if (w.tagId != null) ids.add(w.tagId);
            const cfg = w.config;
            if (!cfg) continue;
            for (const key of ['tagSecondary', 'tagFlow', 'tagPosition', 'tagBinding'] as const) {
                const v = cfg[key];
                if (typeof v === 'number') ids.add(v);
            }
        }
        return ids;
    }, [widgets]);

    const { values: liveValues, connected: wsConnected } = useRealtime(selectedOrgId ?? undefined, canvasTagIds);

    // Fetch active alarms for tags on this canvas (view mode only)
    const [alarmTagIds, setAlarmTagIds] = useState<Set<number>>(new Set());

    useEffect(() => {
        if (mode !== 'view' || canvasTagIds.size === 0) return;
        // Use the shared API client: it attaches the JWT via its interceptor.
        // This used to be a raw fetch reading localStorage['token'] — a key that
        // does not exist (the store persists under 'auth-storage'), so every
        // request went out unauthenticated, 401'd, and was swallowed. The result
        // was a mimic page where alarm borders and badges NEVER lit up: an active
        // alarm on the plant showed as a normal green screen to the operator.
        const fetchAlarms = async () => {
            try {
                const data = await alarmsApi.getActiveAlarms();
                const ids = new Set<number>();
                for (const a of data) { if (a.tag_id != null) ids.add(a.tag_id); }
                setAlarmTagIds(ids);
            } catch (err) {
                // Surface the failure instead of hiding it — a silent catch here
                // is what let the broken auth go unnoticed.
                console.error('[synoptic] failed to load active alarms', err);
            }
        };
        void fetchAlarms();
        const interval = setInterval(() => void fetchAlarms(), 30_000);
        return () => clearInterval(interval);
    }, [mode, canvasTagIds]);

    // Redirect non-admin users who land directly on the /edit URL to view mode.
    useEffect(() => {
        if (mode === 'edit' && !isAdmin() && id) {
            navigate(`/synoptics/${id}`, { replace: true });
        }
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [mode, id]);

    // Load synoptic + tags.
    useEffect(() => {
        if (!id) return;
        (async () => {
            try {
                const [s, tagList, synRes] = await Promise.all([
                    synopticsApi.get(Number(id)),
                    tagsApi.getAllWithHierarchy().catch(() => []),
                    synopticsApi.list().catch(() => ({ items: [] })),
                ]);
                setSynoptic(s);
                const initial = s.layout || [];
                setWidgets(initial);
                historyRef.current = [JSON.parse(JSON.stringify(initial))];
                historyIdxRef.current = 0;
                setTags(tagList);
                setSynopticList(synRes.items.map(x => ({ id: x.id, name: x.name })));
            } catch (e) {
                console.error('Failed to load synoptic', e);
                setLoadError('Errore nel caricamento del sinottico. Riprova.');
            } finally {
                setIsLoading(false);
            }
        })();
    }, [id]);

    // Fit-to-width scaling so large canvases stay usable on any screen.
    useEffect(() => {
        const recompute = () => {
            if (!canvasWrapRef.current || !synoptic) return;
            const avail = canvasWrapRef.current.clientWidth - 4;
            setScale(Math.min(1, avail / synoptic.canvas_w));
        };
        recompute();
        window.addEventListener('resize', recompute);
        return () => window.removeEventListener('resize', recompute);
    }, [synoptic]);

    // Pinch-to-zoom for the viewer on mobile/tablet (view mode only).
    useEffect(() => {
        if (isEdit) return;
        const wrapper = canvasWrapRef.current;
        if (!wrapper) return;
        let startDist = 0;
        let startScale = 1;
        const onTouchStart = (e: TouchEvent) => {
            if (e.touches.length === 2) {
                startDist = Math.hypot(
                    e.touches[0].clientX - e.touches[1].clientX,
                    e.touches[0].clientY - e.touches[1].clientY,
                );
                startScale = scaleRef.current;
            }
        };
        const onTouchMove = (e: TouchEvent) => {
            if (e.touches.length !== 2 || startDist === 0) return;
            e.preventDefault();
            const dist = Math.hypot(
                e.touches[0].clientX - e.touches[1].clientX,
                e.touches[0].clientY - e.touches[1].clientY,
            );
            setScale(Math.max(0.15, Math.min(4, startScale * (dist / startDist))));
        };
        wrapper.addEventListener('touchstart', onTouchStart, { passive: true });
        wrapper.addEventListener('touchmove', onTouchMove, { passive: false });
        return () => {
            wrapper.removeEventListener('touchstart', onTouchStart);
            wrapper.removeEventListener('touchmove', onTouchMove);
        };
    }, [isEdit]);

    const tagValue = useCallback((tagId?: number | null): LiveValue | undefined => {
        if (tagId == null) return undefined; // unbound widget → always preview
        if (mode !== 'view') return undefined; // edit mode → preview
        const v = liveValues.get(tagId);
        // Tag is bound but no live data yet (edge offline / WS not connected) →
        // return BAD-quality null so widgets show "—" / grey / off state.
        return v ? { value: v.value, quality: v.quality } : { value: null, quality: 2 };
    }, [liveValues, mode]);

    // onWrite: called by button widgets in view mode to write a value to a tag
    // via the i3X interface (PUT /api/i3x/v1/properties/tag-{id}/value).
    // Applies reverse EU scaling when the tag has scaling enabled.
    const onWrite = useCallback(async (tagId: number, value: number): Promise<void> => {
        if (mode !== 'view') return;
        const tag = tags.find(t => t.id === tagId);
        let rawValue = value;
        // `invert` deliberately does NOT gate this. Server-side (internal/scaling)
        // invert applies only to BOOL tags; numeric tags are always linearly
        // scaled on read. Skipping the reverse map when invert happened to be set
        // sent an engineering-unit number straight to the raw PLC register — e.g.
        // a 0–27648 ↔ 0–100 tag received 50 counts (~0.2% of span) for a
        // requested 50%, while the EU-scaled read-back looked consistent.
        if (tag?.scaling_enabled) {
            const euMin = tag.scaling_eu_min ?? 0;
            const euMax = tag.scaling_eu_max ?? 100;
            const rawMin = tag.scaling_raw_min ?? 0;
            const rawMax = tag.scaling_raw_max ?? 100;
            const euSpan = euMax - euMin;
            if (euSpan !== 0) {
                rawValue = (value - euMin) / euSpan * (rawMax - rawMin) + rawMin;
            }
        }
        await i3xApi.writePropertyValue(`tag-${tagId}`, rawValue);
    }, [mode, tags]);

    // ── Editing actions ────────────────────────────────────────────────────
    const addWidget = (type: SynopticWidget['type']) => {
        const meta = WIDGET_CATALOG.find(w => w.type === type)!;
        const w: SynopticWidget = {
            id: uid(), type, x: 40, y: 40, w: meta.defaultW, h: meta.defaultH,
            label: type === 'label' ? 'Etichetta' : '',
            config: type === 'gauge' || type === 'tank' ? { min: 0, max: 100 } : {},
        };
        setWidgets(prev => {
            const next = [...prev, w];
            pushHistory(next);
            return next;
        });
        setSelectedIds([w.id]);
    };

    const patchWidget = (wid: string, patch: Partial<SynopticWidget>) => {
        setWidgets(prev => prev.map(w => w.id === wid ? { ...w, ...patch } : w));
    };
    const patchConfig = (wid: string, patch: Record<string, unknown>) => {
        setWidgets(prev => {
            const next = prev.map(w => w.id === wid ? { ...w, config: { ...w.config, ...patch } } : w);
            pushHistory(next);
            return next;
        });
    };
    const removeWidget = (wid: string) => {
        setWidgets(prev => {
            const next = prev.filter(w => w.id !== wid);
            pushHistory(next);
            return next;
        });
        setSelectedIds(prev => prev.filter(i => i !== wid));
    };
    const removeWidgets = (wids: string[]) => {
        const set = new Set(wids);
        setWidgets(prev => {
            const next = prev.filter(w => !set.has(w.id));
            pushHistory(next);
            return next;
        });
        setSelectedIds([]);
    };

    // Tag binding with auto-fill from scaling metadata
    const handleTagBind = useCallback((widgetId: string, rawValue: string) => {
        const tagId = rawValue === 'none' ? null : Number(rawValue);
        const w = widgets.find(x => x.id === widgetId);
        if (!w) return;

        const configPatch: Record<string, unknown> = {};
        if (tagId != null) {
            const tag = tags.find(t => t.id === tagId);
            if (tag?.scaling_enabled) {
                if (w.type === 'value' || w.type === 'gauge') {
                    if (tag.eu_unit) configPatch.unit = tag.eu_unit;
                    if (tag.eu_decimals != null) configPatch.decimals = tag.eu_decimals;
                }
                if (w.type === 'gauge' || w.type === 'tank' || w.type === 'bargraph') {
                    if (tag.scaling_eu_min != null) configPatch.min = tag.scaling_eu_min;
                    if (tag.scaling_eu_max != null) configPatch.max = tag.scaling_eu_max;
                }
            }
        }

        const updated: SynopticWidget = Object.keys(configPatch).length > 0
            ? { ...w, tagId: tagId ?? undefined, config: { ...w.config, ...configPatch } }
            : { ...w, tagId: tagId ?? undefined };

        setWidgets(prev => {
            const next = prev.map(x => x.id === widgetId ? updated : x);
            pushHistory(next);
            return next;
        });
    }, [widgets, tags, pushHistory]);

    // Drag-to-move (pointer events, scale-aware).
    // Capture is set on the widget div (currentTarget) so events follow the pointer
    // even when it moves outside the widget's bounds. Move/Up handlers live on the
    // canvas div to avoid losing them during fast drags.
    const onWidgetPointerDown = (e: React.PointerEvent, w: SynopticWidget) => {
        if (!isEdit) return;
        if (w.locked) {
            e.stopPropagation();
            setSelectedIds([w.id]);
            return;
        }
        e.stopPropagation();

        if (e.shiftKey) {
            // Shift-click: toggle widget in selection (no drag)
            setSelectedIds(prev =>
                prev.includes(w.id) ? prev.filter(i => i !== w.id) : [...prev, w.id]
            );
            return;
        }

        // If clicking a widget not in selection, replace selection
        if (!selectedIds.includes(w.id)) {
            setSelectedIds([w.id]);
        }

        // Start drag for all currently selected (or just the clicked one if it's now selected)
        const currentSelected = selectedIds.includes(w.id) ? selectedIds : [w.id];
        const origPositions = new Map<string, { x: number; y: number }>();
        widgets.forEach(widget => {
            if (currentSelected.includes(widget.id)) {
                origPositions.set(widget.id, { x: widget.x, y: widget.y });
            }
        });
        // Ensure current widget is included
        if (!origPositions.has(w.id)) {
            origPositions.set(w.id, { x: w.x, y: w.y });
        }

        dragRef.current = { startX: e.clientX, startY: e.clientY, origPositions };
        (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    };
    const onCanvasPointerMove = (e: React.PointerEvent) => {
        const r = resizeRef.current;
        if (r) {
            const dx = (e.clientX - r.startX) / scale;
            const dy = (e.clientY - r.startY) / scale;
            let nx = r.origX, ny = r.origY, nw = r.origW, nh = r.origH;
            if (r.handleId.includes('e')) {
                nw = Math.max(SNAP, snap(r.origW + dx));
            }
            if (r.handleId.includes('s')) {
                nh = Math.max(SNAP, snap(r.origH + dy));
            }
            if (r.handleId.includes('w')) {
                const sdx = snap(dx);
                nx = r.origX + sdx;
                nw = Math.max(SNAP, r.origW - sdx);
            }
            if (r.handleId.includes('n')) {
                const sdy = snap(dy);
                ny = r.origY + sdy;
                nh = Math.max(SNAP, r.origH - sdy);
            }
            setWidgets(prev => prev.map(w => w.id === r.widgetId ? { ...w, x: nx, y: ny, w: nw, h: nh } : w));
            return;
        }
        const d = dragRef.current;
        if (!d) return;
        const dx = (e.clientX - d.startX) / scale;
        const dy = (e.clientY - d.startY) / scale;
        setWidgets(prev => prev.map(w => {
            const orig = d.origPositions.get(w.id);
            if (!orig) return w;
            return { ...w, x: snap(orig.x + dx), y: snap(orig.y + dy) };
        }));
    };
    const onCanvasPointerUp = () => {
        if (resizeRef.current || dragRef.current) {
            // Push history after drag/resize ends
            setWidgets(prev => {
                pushHistory(prev);
                return prev;
            });
        }
        resizeRef.current = null;
        dragRef.current = null;
    };

    // Clipboard ref (module-level copy/paste without serialisation overhead).
    const clipboard = useRef<SynopticWidget | null>(null);

    // Keyboard shortcuts: Delete/Backspace remove, Ctrl+D duplicate, Ctrl+C copy,
    // Ctrl+V paste, Arrow nudge (Shift+Arrow = 8px), Escape deselect, Ctrl+A select all,
    // Ctrl+Z undo, Ctrl+Y / Ctrl+Shift+Z redo.
    useEffect(() => {
        if (!isEdit) return;
        const handler = (e: KeyboardEvent) => {
            if ((e.target as HTMLElement).tagName === 'INPUT') return;
            if (e.key === 'Delete' || e.key === 'Backspace') {
                if (selectedIds.length > 0) {
                    removeWidgets(selectedIds);
                }
            } else if (e.key === 'Escape') {
                setSelectedIds([]);
            } else if ((e.ctrlKey || e.metaKey) && e.key === 'a') {
                e.preventDefault();
                setSelectedIds(widgets.map(w => w.id));
            } else if ((e.ctrlKey || e.metaKey) && e.key === 'd' && selectedIds.length === 1) {
                e.preventDefault();
                setWidgets(prev => {
                    const src = prev.find(w => w.id === selectedIds[0]);
                    if (!src) return prev;
                    const copy = { ...src, id: uid(), x: src.x + SNAP * 2, y: src.y + SNAP * 2 };
                    clipboard.current = copy;
                    setSelectedIds([copy.id]);
                    const next = [...prev, copy];
                    pushHistory(next);
                    return next;
                });
            } else if ((e.ctrlKey || e.metaKey) && e.key === 'c' && selectedIds.length === 1) {
                setWidgets(prev => {
                    const src = prev.find(w => w.id === selectedIds[0]);
                    if (src) clipboard.current = src;
                    return prev;
                });
            } else if ((e.ctrlKey || e.metaKey) && e.key === 'v' && clipboard.current) {
                e.preventDefault();
                const copy = { ...clipboard.current, id: uid(), x: clipboard.current.x + SNAP * 2, y: clipboard.current.y + SNAP * 2 };
                clipboard.current = copy;
                setWidgets(prev => {
                    const next = [...prev, copy];
                    pushHistory(next);
                    return next;
                });
                setSelectedIds([copy.id]);
            } else if (e.key.startsWith('Arrow') && selectedIds.length > 0) {
                e.preventDefault();
                const step = e.shiftKey ? SNAP : 1;
                setWidgets(prev => prev.map(w => {
                    if (!selectedIds.includes(w.id)) return w;
                    const dx = e.key === 'ArrowLeft' ? -step : e.key === 'ArrowRight' ? step : 0;
                    const dy = e.key === 'ArrowUp' ? -step : e.key === 'ArrowDown' ? step : 0;
                    return { ...w, x: w.x + dx, y: w.y + dy };
                }));
            } else if ((e.ctrlKey || e.metaKey) && e.key === 'z' && !e.shiftKey) {
                e.preventDefault();
                if (historyIdxRef.current > 0) {
                    historyIdxRef.current--;
                    setWidgets(JSON.parse(JSON.stringify(historyRef.current[historyIdxRef.current])));
                    setSelectedIds([]);
                }
            } else if ((e.ctrlKey || e.metaKey) && (e.key === 'y' || (e.key === 'z' && e.shiftKey))) {
                e.preventDefault();
                if (historyIdxRef.current < historyRef.current.length - 1) {
                    historyIdxRef.current++;
                    setWidgets(JSON.parse(JSON.stringify(historyRef.current[historyIdxRef.current])));
                    setSelectedIds([]);
                }
            }
        };
        window.addEventListener('keydown', handler);
        return () => window.removeEventListener('keydown', handler);
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [isEdit, selectedIds, widgets, pushHistory]);

    const handleSave = async () => {
        if (!synoptic) return;
        setIsSaving(true);
        setSaveStatus('idle');
        try {
            await synopticsApi.update(synoptic.id, {
                name: synoptic.name, description: synoptic.description,
                site_id: synoptic.site_id, area_id: synoptic.area_id,
                background_color: synoptic.background_color,
                canvas_w: synoptic.canvas_w, canvas_h: synoptic.canvas_h,
                layout: widgets,
            });
            setSaveStatus('ok');
            setTimeout(() => setSaveStatus('idle'), 2500);
        } catch (e) {
            console.error('Failed to save synoptic', e);
            setSaveStatus('error');
            setTimeout(() => setSaveStatus('idle'), 4000);
        } finally {
            setIsSaving(false);
        }
    };

    if (isLoading) {
        return <div className="p-8 flex items-center justify-center text-muted-foreground"><Loader2 className="animate-spin mr-2" /> Caricamento...</div>;
    }
    if (loadError || !synoptic) {
        return (
            <div className="p-8 flex flex-col items-center justify-center gap-3 text-muted-foreground">
                <AlertTriangle size={40} className="text-destructive opacity-70" />
                <p>{loadError || 'Sinottico non trovato.'}</p>
                <button onClick={() => navigate('/synoptics')} className="text-primary underline text-sm">Torna ai sinottici</button>
            </div>
        );
    }

    return (
        <div className="space-y-4">
            {/* Toolbar */}
            <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-3 min-w-0">
                    <Button variant="ghost" size="icon" onClick={() => navigate('/synoptics')}><ArrowLeft size={18} /></Button>
                    <div className="min-w-0">
                        <h2 className="text-xl font-bold tracking-tight truncate">{synoptic.name}</h2>
                        <p className="text-xs text-muted-foreground">{isEdit ? 'Designer' : 'Runtime — valori live'}</p>
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    {isEdit ? (
                        <>
                            <Button variant="outline" size="sm" className="gap-1" onClick={() => navigate(`/synoptics/${synoptic.id}`)}>
                                <Monitor size={15} /> Anteprima
                            </Button>
                            {saveStatus === 'ok' && (
                                <span className="flex items-center gap-1 text-xs text-emerald-500"><Check size={14} /> Salvato</span>
                            )}
                            {saveStatus === 'error' && (
                                <span className="flex items-center gap-1 text-xs text-destructive"><AlertTriangle size={14} /> Errore</span>
                            )}
                            <Button size="sm" className="gap-1" onClick={handleSave} disabled={isSaving}>
                                {isSaving ? <Loader2 size={15} className="animate-spin" /> : <Save size={15} />} Salva
                            </Button>
                        </>
                    ) : (
                        <>
                            <span className={cn(
                                'flex items-center gap-1.5 text-xs font-medium px-2 py-1 rounded-full border',
                                wsConnected
                                    ? 'text-emerald-600 border-emerald-500/30 bg-emerald-500/10'
                                    : 'text-slate-500 border-slate-500/30 bg-slate-500/10'
                            )}>
                                <span className={cn('w-1.5 h-1.5 rounded-full', wsConnected ? 'bg-emerald-500 animate-pulse' : 'bg-slate-400')} />
                                {wsConnected ? 'LIVE' : 'OFFLINE'}
                            </span>
                            <Button variant="outline" size="icon" className="h-8 w-8"
                                title="Schermo intero"
                                onClick={() => {
                                    if (!document.fullscreenElement) {
                                        document.documentElement.requestFullscreen().catch(() => {});
                                    } else {
                                        document.exitFullscreen().catch(() => {});
                                    }
                                }}>
                                <Maximize2 size={14} />
                            </Button>
                            {isAdmin() && (
                                <Button variant="outline" size="sm" className="gap-1" onClick={() => navigate(`/synoptics/${synoptic.id}/edit`)}>
                                    <Pencil size={15} /> Modifica
                                </Button>
                            )}
                        </>
                    )}
                </div>
            </div>

            <div className={cn('grid gap-4', isEdit ? 'grid-cols-[160px_1fr_280px]' : 'grid-cols-1')}>
                {/* Palette (edit only) */}
                {isEdit && (
                    <div className="space-y-2">
                        <p className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Componenti</p>
                        <div className="grid grid-cols-2 gap-2">
                            {WIDGET_CATALOG.map(c => (
                                <button key={c.type} onClick={() => addWidget(c.type)}
                                    className="flex flex-col items-center justify-center gap-1 p-2 rounded-md border bg-card hover:bg-muted/50 hover:border-primary/40 transition-colors h-16">
                                    <div className="w-7 h-7 flex items-center justify-center">
                                        <SynopticWidgetView widget={{ id: 'preview', type: c.type, x: 0, y: 0, w: 28, h: 28, config: { min: 0, max: 100 } }} />
                                    </div>
                                    <span className="text-[10px]">{c.label}</span>
                                </button>
                            ))}
                        </div>
                    </div>
                )}

                {/* Canvas */}
                <div ref={canvasWrapRef} className="overflow-auto rounded-md border bg-muted/20 p-0.5">
                    <div
                        className="relative mx-auto"
                        style={{
                            width: synoptic.canvas_w * scale,
                            height: synoptic.canvas_h * scale,
                        }}
                        onClick={() => isEdit && setSelectedIds([])}
                    >
                        <div
                            className="absolute top-0 left-0 origin-top-left"
                            style={{
                                width: synoptic.canvas_w, height: synoptic.canvas_h, transform: `scale(${scale})`,
                                background: synoptic.background_color,
                                backgroundImage: isEdit ? `radial-gradient(circle, rgba(148,163,184,0.25) 1px, transparent 1px)` : undefined,
                                backgroundSize: isEdit ? `${SNAP * 2}px ${SNAP * 2}px` : undefined,
                            }}
                            onPointerMove={onCanvasPointerMove}
                            onPointerUp={onCanvasPointerUp}
                        >
                            {widgets.map(w => (
                                <div
                                    key={w.id}
                                    onPointerDown={(e) => onWidgetPointerDown(e, w)}
                                    onClick={(e) => { if (isEdit) { e.stopPropagation(); } }}
                                    className={cn('absolute select-none',
                                        isEdit && (w.locked ? 'cursor-default' : 'cursor-move'),
                                        isEdit && selectedIds.includes(w.id) && 'outline outline-2 outline-primary outline-offset-2',
                                        isEdit && w.locked && 'opacity-80')}
                                    style={{ left: w.x, top: w.y, width: w.w, height: w.h, transform: w.rotation ? `rotate(${w.rotation}deg)` : undefined }}
                                >
                                    <SynopticWidgetView widget={w} live={tagValue(w.tagId)}
                                        liveSecondary={tagValue(
                                            (w.config?.tagSecondary as number | null | undefined) ??
                                            (w.config?.tagFlow as number | null | undefined) ??
                                            (w.config?.tagPosition as number | null | undefined) ??
                                            (w.config?.tagBinding as number | null | undefined)
                                        )}
                                        inAlarm={alarmTagIds.has(w.tagId ?? -1)}
                                        onWrite={w.tagId != null && !w.config?.navigateSynopticId ? (v) => onWrite(w.tagId!, v) : undefined}
                                        onNavigate={!isEdit && w.config?.navigateSynopticId != null
                                            ? () => navigate(`/synoptics/${w.config!.navigateSynopticId}`)
                                            : undefined} />
                                    {!isEdit && alarmTagIds.has(w.tagId ?? -1) && (
                                        <button
                                            onClick={e => { e.stopPropagation(); navigate('/alarms'); }}
                                            style={{ position: 'absolute', top: 2, right: 2, zIndex: 10, cursor: 'pointer', border: 'none', padding: '1px 4px', borderRadius: 3, fontSize: 8, fontWeight: 700, lineHeight: 1.4, background: 'rgba(239,68,68,0.9)', color: 'white' }}
                                            title="Tag in allarme — clicca per vedere gli allarmi"
                                            className="animate-pulse"
                                        >⚠ ALM</button>
                                    )}
                                </div>
                            ))}
                            {isEdit && selected && !selected.locked && selectedIds.length === 1 && RESIZE_HANDLES.map(h => {
                                const hs = 8 / scale; // handle size in canvas-px so it's always 8 screen-px
                                const hoff = 4 / scale;
                                return (
                                    <div
                                        key={h.id}
                                        style={{
                                            position: 'absolute',
                                            left: selected.x + h.xF * selected.w - hoff,
                                            top: selected.y + h.yF * selected.h - hoff,
                                            width: hs,
                                            height: hs,
                                            cursor: h.cursor,
                                            background: 'white',
                                            border: '1.5px solid #3b82f6',
                                            borderRadius: 2,
                                            zIndex: 50,
                                            pointerEvents: isEdit ? 'auto' : 'none',
                                        }}
                                        onPointerDown={(e) => {
                                            e.stopPropagation();
                                            (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
                                            resizeRef.current = {
                                                handleId: h.id,
                                                startX: e.clientX,
                                                startY: e.clientY,
                                                origX: selected.x,
                                                origY: selected.y,
                                                origW: selected.w,
                                                origH: selected.h,
                                                widgetId: selected.id,
                                            };
                                        }}
                                    />
                                );
                            })}
                        </div>
                    </div>
                </div>

                {/* Properties panel (edit only) */}
                {isEdit && (
                    <div className="space-y-3">
                        <p className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Proprietà</p>
                        {selectedIds.length > 1 ? (
                            <div className="space-y-3">
                                <p className="text-xs text-muted-foreground">{selectedIds.length} widget selezionati</p>
                                {/* Align tools */}
                                <div className="space-y-1">
                                    <p className="text-[10px] uppercase text-muted-foreground font-semibold tracking-wider">Allinea</p>
                                    <div className="grid grid-cols-3 gap-1">
                                        {[
                                            { icon: <AlignLeft size={13}/>, title: 'Sinistra', align: (ws: SynopticWidget[]) => { const min = Math.min(...ws.map(w => w.x)); return ws.map(w => ({ ...w, x: min })); } },
                                            { icon: <AlignCenterHorizontal size={13}/>, title: 'Centro H', align: (ws: SynopticWidget[]) => { const cx = (Math.min(...ws.map(w => w.x)) + Math.max(...ws.map(w => w.x + w.w))) / 2; return ws.map(w => ({ ...w, x: Math.round(cx - w.w / 2) })); } },
                                            { icon: <AlignRight size={13}/>, title: 'Destra', align: (ws: SynopticWidget[]) => { const max = Math.max(...ws.map(w => w.x + w.w)); return ws.map(w => ({ ...w, x: max - w.w })); } },
                                            { icon: <AlignStartVertical size={13}/>, title: 'Alto', align: (ws: SynopticWidget[]) => { const min = Math.min(...ws.map(w => w.y)); return ws.map(w => ({ ...w, y: min })); } },
                                            { icon: <AlignCenterVertical size={13}/>, title: 'Centro V', align: (ws: SynopticWidget[]) => { const cy = (Math.min(...ws.map(w => w.y)) + Math.max(...ws.map(w => w.y + w.h))) / 2; return ws.map(w => ({ ...w, y: Math.round(cy - w.h / 2) })); } },
                                            { icon: <AlignEndVertical size={13}/>, title: 'Basso', align: (ws: SynopticWidget[]) => { const max = Math.max(...ws.map(w => w.y + w.h)); return ws.map(w => ({ ...w, y: max - w.h })); } },
                                        ].map(({ icon, title, align }) => (
                                            <Button key={title} variant="outline" size="icon" className="h-7 w-full" title={title}
                                                onClick={() => {
                                                    const sel = widgets.filter(w => selectedIds.includes(w.id));
                                                    const aligned = align(sel);
                                                    const alignedMap = new Map(aligned.map(w => [w.id, w]));
                                                    setWidgets(prev => {
                                                        const next = prev.map(w => alignedMap.get(w.id) ?? w);
                                                        pushHistory(next);
                                                        return next;
                                                    });
                                                }}>
                                                {icon}
                                            </Button>
                                        ))}
                                    </div>
                                    <div className="grid grid-cols-2 gap-1 mt-1">
                                        {[
                                            { icon: <AlignHorizontalDistributeCenter size={13}/>, title: 'Dist. H', distribute: (ws: SynopticWidget[]) => {
                                                const sorted = [...ws].sort((a, b) => a.x - b.x);
                                                const minX = sorted[0].x; const maxX = sorted[sorted.length-1].x + sorted[sorted.length-1].w;
                                                const totalW = sorted.reduce((s, w) => s + w.w, 0);
                                                const gap = (maxX - minX - totalW) / (sorted.length - 1);
                                                let cur = minX;
                                                return sorted.map(w => { const r = { ...w, x: Math.round(cur) }; cur += w.w + gap; return r; });
                                            }},
                                            { icon: <AlignVerticalDistributeCenter size={13}/>, title: 'Dist. V', distribute: (ws: SynopticWidget[]) => {
                                                const sorted = [...ws].sort((a, b) => a.y - b.y);
                                                const minY = sorted[0].y; const maxY = sorted[sorted.length-1].y + sorted[sorted.length-1].h;
                                                const totalH = sorted.reduce((s, w) => s + w.h, 0);
                                                const gap = (maxY - minY - totalH) / (sorted.length - 1);
                                                let cur = minY;
                                                return sorted.map(w => { const r = { ...w, y: Math.round(cur) }; cur += w.h + gap; return r; });
                                            }},
                                        ].map(({ icon, title, distribute }) => (
                                            <Button key={title} variant="outline" size="sm" className="h-7 text-xs gap-1 w-full" title={title}
                                                disabled={selectedIds.length < 3}
                                                onClick={() => {
                                                    const sel = widgets.filter(w => selectedIds.includes(w.id));
                                                    if (sel.length < 3) return;
                                                    const distributed = distribute(sel);
                                                    const distMap = new Map(distributed.map(w => [w.id, w]));
                                                    setWidgets(prev => {
                                                        const next = prev.map(w => distMap.get(w.id) ?? w);
                                                        pushHistory(next);
                                                        return next;
                                                    });
                                                }}>
                                                {icon} {title}
                                            </Button>
                                        ))}
                                    </div>
                                </div>
                                <Button variant="destructive" size="sm" className="w-full gap-1"
                                    onClick={() => removeWidgets(selectedIds)}>
                                    <Trash2 size={13} /> Elimina selezionati
                                </Button>
                            </div>
                        ) : !selected ? (
                            <div className="space-y-3">
                                <p className="text-xs text-muted-foreground">Seleziona un componente, oppure configura la pagina:</p>
                                <div className="grid gap-2">
                                    <Label className="text-xs">Nome pagina</Label>
                                    <Input className="h-8 text-xs" value={synoptic.name}
                                        onChange={e => setSynoptic({ ...synoptic, name: e.target.value })} />
                                </div>
                                <div className="grid gap-2">
                                    <Label className="text-xs">Sfondo</Label>
                                    <input type="color" value={synoptic.background_color}
                                        onChange={e => setSynoptic({ ...synoptic, background_color: e.target.value })}
                                        className="h-8 w-full rounded border bg-transparent" />
                                </div>
                                <div className="grid grid-cols-2 gap-2">
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Larghezza</Label>
                                        <Input type="number" value={synoptic.canvas_w} onChange={e => setSynoptic({ ...synoptic, canvas_w: parseInt(e.target.value) || 1280 })} />
                                    </div>
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Altezza</Label>
                                        <Input type="number" value={synoptic.canvas_h} onChange={e => setSynoptic({ ...synoptic, canvas_h: parseInt(e.target.value) || 720 })} />
                                    </div>
                                </div>
                            </div>
                        ) : (
                            <div className="space-y-3">
                                <div className="flex items-center justify-between">
                                    <span className="text-sm font-medium capitalize">{selected.type}</span>
                                    <div className="flex gap-0.5">
                                        <Button variant="ghost" size="icon" className="h-7 w-7" title="Duplica (Ctrl+D)"
                                            onClick={() => {
                                                const copy = { ...selected, id: uid(), x: selected.x + SNAP * 2, y: selected.y + SNAP * 2 };
                                                setWidgets(prev => {
                                                    const next = [...prev, copy];
                                                    pushHistory(next);
                                                    return next;
                                                });
                                                setSelectedIds([copy.id]);
                                            }}><Copy size={13} /></Button>
                                        <Button variant="ghost" size="icon" className="h-7 w-7"
                                            title={selected.locked ? 'Sblocca widget' : 'Blocca widget'}
                                            onClick={() => {
                                                setWidgets(prev => {
                                                    const next = prev.map(w => w.id === selected.id ? { ...w, locked: !w.locked } : w);
                                                    pushHistory(next);
                                                    return next;
                                                });
                                            }}>
                                            {selected.locked ? <Unlock size={13} /> : <Lock size={13} />}
                                        </Button>
                                        <Button variant="ghost" size="icon" className="h-7 w-7 text-destructive" onClick={() => removeWidget(selected.id)}><Trash2 size={15} /></Button>
                                    </div>
                                </div>

                                {WIDGET_CATALOG.find(c => c.type === selected.type)?.needsTag && (
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Tag</Label>
                                        <TagCombobox
                                            tags={tags}
                                            value={selected.tagId != null ? String(selected.tagId) : 'none'}
                                            onChange={v => handleTagBind(selected.id, v)}
                                        />
                                    </div>
                                )}

                                <div className="grid gap-1">
                                    <Label className="text-xs">Etichetta</Label>
                                    <Input className="h-8 text-xs" value={selected.label || ''} onChange={e => patchWidget(selected.id, { label: e.target.value })} />
                                </div>

                                {(selected.type === 'value') && (
                                    <div className="grid grid-cols-2 gap-2">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Unità</Label>
                                            <Input className="h-8 text-xs" value={String(selected.config?.unit ?? '')} onChange={e => patchConfig(selected.id, { unit: e.target.value })} />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Decimali</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.decimals ?? 1} onChange={e => patchConfig(selected.id, { decimals: parseInt(e.target.value) || 0 })} />
                                        </div>
                                    </div>
                                )}

                                {selected.type === 'value' && (
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Dim. testo (px)</Label>
                                        <Input className="h-8 text-xs" type="number" value={selected.config?.fontSize ?? 22}
                                            onChange={e => patchConfig(selected.id, { fontSize: parseInt(e.target.value) || 22 })} />
                                    </div>
                                )}

                                {(selected.type === 'gauge' || selected.type === 'tank' || selected.type === 'bargraph') && (
                                    <div className="grid grid-cols-2 gap-2">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Min</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.min ?? 0} onChange={e => patchConfig(selected.id, { min: parseFloat(e.target.value) || 0 })} />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Max</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.max ?? 100} onChange={e => patchConfig(selected.id, { max: parseFloat(e.target.value) || 100 })} />
                                        </div>
                                    </div>
                                )}

                                {selected.type === 'bargraph' && (
                                    <div className="flex items-center gap-2">
                                        <input type="checkbox" id="bar-vert" checked={!!selected.config?.vertical}
                                            onChange={e => patchConfig(selected.id, { vertical: e.target.checked })} />
                                        <Label htmlFor="bar-vert" className="text-xs cursor-pointer">Verticale</Label>
                                    </div>
                                )}

                                {selected.type === 'pipe' && (
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Forma</Label>
                                        <Select value={String(selected.config?.pipeShape ?? 'straight')}
                                            onValueChange={v => patchConfig(selected.id, { pipeShape: v })}>
                                            <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="straight">Dritto</SelectItem>
                                                <SelectItem value="corner">Curva (L)</SelectItem>
                                                <SelectItem value="tee">Derivazione (T)</SelectItem>
                                                <SelectItem value="cross">Croce (+)</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                )}

                                {selected.type === 'button' && (
                                    <div className="grid grid-cols-2 gap-2">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Valore ON</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.writeValue ?? 1} onChange={e => patchConfig(selected.id, { writeValue: parseFloat(e.target.value) })} />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Valore OFF</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.writeOffValue ?? 0} onChange={e => patchConfig(selected.id, { writeOffValue: parseFloat(e.target.value) })} />
                                        </div>
                                    </div>
                                )}

                                {selected.type === 'button' && (
                                    <div className="flex items-center gap-2 col-span-2">
                                        <input type="checkbox" id="btn-momentary" checked={!!selected.config?.momentary}
                                            onChange={e => patchConfig(selected.id, { momentary: e.target.checked })} />
                                        <Label htmlFor="btn-momentary" className="text-xs cursor-pointer">Momentaneo (premi/rilascia)</Label>
                                    </div>
                                )}

                                {selected.type === 'button' && (
                                    <div className="flex items-center gap-2 col-span-2">
                                        <input type="checkbox" id="btn-confirm" checked={!!selected.config?.requireConfirm}
                                            onChange={e => patchConfig(selected.id, { requireConfirm: e.target.checked })} />
                                        <Label htmlFor="btn-confirm" className="text-xs cursor-pointer">Richiedi conferma</Label>
                                    </div>
                                )}

                                {(selected.type === 'value' || selected.type === 'indicator' || selected.type === 'gauge') && (
                                    <div className="grid grid-cols-2 gap-2">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Warn &gt;</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.warnAbove ?? ''} onChange={e => patchConfig(selected.id, { warnAbove: e.target.value === '' ? undefined : parseFloat(e.target.value) })} />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Crit &gt;</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.critAbove ?? ''} onChange={e => patchConfig(selected.id, { critAbove: e.target.value === '' ? undefined : parseFloat(e.target.value) })} />
                                        </div>
                                    </div>
                                )}

                                {(selected.type === 'indicator' || selected.type === 'pump' || selected.type === 'valve' || selected.type === 'motor' || selected.type === 'button') && (
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Valore "attivo" (≥)</Label>
                                        <Input className="h-8 text-xs" type="number" value={selected.config?.onValue ?? 1} onChange={e => patchConfig(selected.id, { onValue: parseFloat(e.target.value) || 0 })} />
                                    </div>
                                )}

                                {(selected.type === 'label' || selected.type === 'pipe' || selected.type === 'indicator' || selected.type === 'pump' || selected.type === 'valve' || selected.type === 'motor' || selected.type === 'button' || selected.type === 'bargraph') && (
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Colore</Label>
                                        <input type="color" value={String(selected.config?.color ?? '#10b981')} onChange={e => patchConfig(selected.id, { color: e.target.value })} className="h-8 w-full rounded border bg-transparent" />
                                    </div>
                                )}

                                {selected.type === 'label' && (
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Dimensione testo</Label>
                                        <Input className="h-8 text-xs" type="number" value={selected.config?.fontSize ?? 16} onChange={e => patchConfig(selected.id, { fontSize: parseInt(e.target.value) || 16 })} />
                                    </div>
                                )}

                                {/* ── colorOn / colorOff for binary widgets ── */}
                                {(selected.type === 'indicator' || selected.type === 'pump' || selected.type === 'valve' || selected.type === 'motor' || selected.type === 'button') && (
                                    <div className="grid grid-cols-2 gap-2 pt-1 border-t">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Colore ON</Label>
                                            <input type="color" value={String(selected.config?.colorOn ?? '#10b981')} onChange={e => patchConfig(selected.id, { colorOn: e.target.value })} className="h-8 w-full rounded border bg-transparent" />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Colore OFF</Label>
                                            <input type="color" value={String(selected.config?.colorOff ?? '#475569')} onChange={e => patchConfig(selected.id, { colorOff: e.target.value })} className="h-8 w-full rounded border bg-transparent" />
                                        </div>
                                    </div>
                                )}

                                {/* ── colorBands for numeric widgets ── */}
                                {(selected.type === 'value' || selected.type === 'gauge' || selected.type === 'bargraph' || selected.type === 'tank') && (
                                    <div className="space-y-1 pt-1 border-t">
                                        <div className="flex items-center justify-between">
                                            <Label className="text-xs">Bande colore</Label>
                                            <Button variant="ghost" size="sm" className="h-6 px-1 text-xs"
                                                onClick={() => {
                                                    const bands = [...((selected.config?.colorBands as Array<{above:number;color:string}>) ?? []), { above: 0, color: '#10b981' }];
                                                    patchConfig(selected.id, { colorBands: bands });
                                                }}>+ Aggiungi</Button>
                                        </div>
                                        <p className="text-[10px] text-muted-foreground">Rosso riservato agli allarmi</p>
                                        {((selected.config?.colorBands as Array<{above:number;color:string}>) ?? []).map((band, i) => (
                                            <div key={i} className="flex items-center gap-1">
                                                <input type="color" value={band.color} className="h-6 w-8 rounded cursor-pointer border"
                                                    onChange={e => {
                                                        const bands = [...((selected.config?.colorBands as Array<{above:number;color:string}>) ?? [])];
                                                        bands[i] = { ...bands[i], color: e.target.value };
                                                        patchConfig(selected.id, { colorBands: bands });
                                                    }} />
                                                <span className="text-[10px] text-muted-foreground">≥</span>
                                                <Input type="number" className="h-6 text-xs flex-1" value={band.above}
                                                    onChange={e => {
                                                        const bands = [...((selected.config?.colorBands as Array<{above:number;color:string}>) ?? [])];
                                                        bands[i] = { ...bands[i], above: parseFloat(e.target.value) || 0 };
                                                        patchConfig(selected.id, { colorBands: bands });
                                                    }} />
                                                <Button variant="ghost" size="icon" className="h-6 w-6 text-destructive"
                                                    onClick={() => {
                                                        const bands = ((selected.config?.colorBands as Array<{above:number;color:string}>) ?? []).filter((_, j) => j !== i);
                                                        patchConfig(selected.id, { colorBands: bands });
                                                    }}>×</Button>
                                            </div>
                                        ))}
                                    </div>
                                )}

                                {/* ── Value extras ── */}
                                {selected.type === 'value' && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="grid grid-cols-2 gap-2">
                                            <div className="grid gap-1">
                                                <Label className="text-xs">Prefisso</Label>
                                                <Input className="h-8 text-xs" value={String(selected.config?.prefix ?? '')} onChange={e => patchConfig(selected.id, { prefix: e.target.value || undefined })} />
                                            </div>
                                            <div className="grid gap-1">
                                                <Label className="text-xs">Testo no-dato</Label>
                                                <Input className="h-8 text-xs" value={String(selected.config?.noDataText ?? '')} placeholder="—" onChange={e => patchConfig(selected.id, { noDataText: e.target.value || undefined })} />
                                            </div>
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Sfondo</Label>
                                            <input type="color" value={String(selected.config?.bgColor ?? '#0f172a')} onChange={e => patchConfig(selected.id, { bgColor: e.target.value })} className="h-8 w-full rounded border bg-transparent" />
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="show-ts" checked={!!selected.config?.showTimestamp} onChange={e => patchConfig(selected.id, { showTimestamp: e.target.checked })} />
                                            <Label htmlFor="show-ts" className="text-xs cursor-pointer">Mostra timestamp</Label>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="blink-alarm" checked={!!selected.config?.blinkOnAlarm} onChange={e => patchConfig(selected.id, { blinkOnAlarm: e.target.checked })} />
                                            <Label htmlFor="blink-alarm" className="text-xs cursor-pointer">Lampeggia in allarme</Label>
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Tag secondario (SP)</Label>
                                            <TagCombobox tags={tags} value={selected.config?.tagSecondary != null ? String(selected.config.tagSecondary) : 'none'} onChange={v => patchConfig(selected.id, { tagSecondary: v === 'none' ? undefined : Number(v) })} />
                                        </div>
                                    </div>
                                )}

                                {/* ── Gauge extras ── */}
                                {selected.type === 'gauge' && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Spessore arco</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.arcWidth ?? 9} onChange={e => patchConfig(selected.id, { arcWidth: parseInt(e.target.value) || 9 })} />
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="show-ticks" checked={!!selected.config?.showTicks} onChange={e => patchConfig(selected.id, { showTicks: e.target.checked })} />
                                            <Label htmlFor="show-ticks" className="text-xs cursor-pointer">Tacche graduazione</Label>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="show-minmax" checked={!!selected.config?.showMinMax} onChange={e => patchConfig(selected.id, { showMinMax: e.target.checked })} />
                                            <Label htmlFor="show-minmax" className="text-xs cursor-pointer">Mostra min/max</Label>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="show-unit-g" checked={!!selected.config?.showUnit} onChange={e => patchConfig(selected.id, { showUnit: e.target.checked })} />
                                            <Label htmlFor="show-unit-g" className="text-xs cursor-pointer">Mostra unità</Label>
                                        </div>
                                    </div>
                                )}

                                {/* ── Tank extras ── */}
                                {selected.type === 'tank' && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Orientamento</Label>
                                            <Select value={String(selected.config?.tankOrientation ?? 'vertical')} onValueChange={v => patchConfig(selected.id, { tankOrientation: v })}>
                                                <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="vertical">Verticale</SelectItem>
                                                    <SelectItem value="horizontal">Orizzontale</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="show-pct" checked={!!selected.config?.showPercentage} onChange={e => patchConfig(selected.id, { showPercentage: e.target.checked })} />
                                            <Label htmlFor="show-pct" className="text-xs cursor-pointer">Mostra percentuale</Label>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="show-val-t" checked={!!selected.config?.showValue} onChange={e => patchConfig(selected.id, { showValue: e.target.checked })} />
                                            <Label htmlFor="show-val-t" className="text-xs cursor-pointer">Mostra valore EU</Label>
                                        </div>
                                    </div>
                                )}

                                {/* ── Pump/Motor extras ── */}
                                {(selected.type === 'pump' || selected.type === 'motor') && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="show-status" checked={!!selected.config?.showStatus} onChange={e => patchConfig(selected.id, { showStatus: e.target.checked })} />
                                            <Label htmlFor="show-status" className="text-xs cursor-pointer">Mostra RUN/STOP</Label>
                                        </div>
                                        {selected.type === 'pump' && (
                                            <div className="grid gap-1">
                                                <Label className="text-xs">Velocità animazione</Label>
                                                <Select value={String(selected.config?.spinSpeed ?? 'normal')} onValueChange={v => patchConfig(selected.id, { spinSpeed: v })}>
                                                    <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                                    <SelectContent>
                                                        <SelectItem value="slow">Lenta</SelectItem>
                                                        <SelectItem value="normal">Normale</SelectItem>
                                                        <SelectItem value="fast">Veloce</SelectItem>
                                                    </SelectContent>
                                                </Select>
                                            </div>
                                        )}
                                    </div>
                                )}

                                {/* ── Valve extras ── */}
                                {selected.type === 'valve' && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Tipo valvola</Label>
                                            <Select value={String(selected.config?.valveType ?? 'butterfly')} onValueChange={v => patchConfig(selected.id, { valveType: v })}>
                                                <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="butterfly">Farfalla</SelectItem>
                                                    <SelectItem value="gate">Saracinesca</SelectItem>
                                                    <SelectItem value="ball">A sfera</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Tag posizione (0-100%)</Label>
                                            <TagCombobox tags={tags} value={selected.config?.tagPosition != null ? String(selected.config.tagPosition) : 'none'} onChange={v => patchConfig(selected.id, { tagPosition: v === 'none' ? undefined : Number(v) })} />
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="show-pos" checked={!!selected.config?.showPosition} onChange={e => patchConfig(selected.id, { showPosition: e.target.checked })} />
                                            <Label htmlFor="show-pos" className="text-xs cursor-pointer">Mostra % apertura</Label>
                                        </div>
                                    </div>
                                )}

                                {/* ── Bargraph extras ── */}
                                {selected.type === 'bargraph' && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="show-bar-val" checked={!!selected.config?.showBarValue} onChange={e => patchConfig(selected.id, { showBarValue: e.target.checked })} />
                                            <Label htmlFor="show-bar-val" className="text-xs cursor-pointer">Mostra valore sulla barra</Label>
                                        </div>
                                    </div>
                                )}

                                {/* ── Button extras ── */}
                                {selected.type === 'button' && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Forma</Label>
                                            <Select value={String(selected.config?.buttonShape ?? 'rounded')} onValueChange={v => patchConfig(selected.id, { buttonShape: v })}>
                                                <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="rounded">Arrotondato</SelectItem>
                                                    <SelectItem value="rect">Rettangolare</SelectItem>
                                                    <SelectItem value="circle">Circolare</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Icona</Label>
                                            <Select value={String(selected.config?.buttonIcon ?? '')} onValueChange={v => patchConfig(selected.id, { buttonIcon: v || undefined })}>
                                                <SelectTrigger className="h-8 text-xs"><SelectValue placeholder="Nessuna" /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="">Nessuna</SelectItem>
                                                    <SelectItem value="play">▶ Play</SelectItem>
                                                    <SelectItem value="stop">■ Stop</SelectItem>
                                                    <SelectItem value="power">⏻ Power</SelectItem>
                                                    <SelectItem value="reset">↺ Reset</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Testo conferma</Label>
                                            <Input className="h-8 text-xs" value={String(selected.config?.confirmText ?? '')} placeholder="Confermare?" onChange={e => patchConfig(selected.id, { confirmText: e.target.value || undefined })} />
                                        </div>
                                        <div className="grid gap-1 pt-1 border-t">
                                            <Label className="text-xs">Naviga a sinottico</Label>
                                            <Select value={String(selected.config?.navigateSynopticId ?? 'none')}
                                                onValueChange={v => patchConfig(selected.id, { navigateSynopticId: v === 'none' ? undefined : Number(v) })}>
                                                <SelectTrigger className="h-8 text-xs"><SelectValue placeholder="Nessuno (scrivi tag)" /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="none">Nessuno (scrivi tag)</SelectItem>
                                                    {synopticList.filter(s => s.id !== Number(id)).map(s => (
                                                        <SelectItem key={s.id} value={String(s.id)}>{s.name}</SelectItem>
                                                    ))}
                                                </SelectContent>
                                            </Select>
                                        </div>
                                    </div>
                                )}

                                {/* ── Label extras ── */}
                                {selected.type === 'label' && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Allineamento</Label>
                                            <Select value={String(selected.config?.textAlign ?? 'center')} onValueChange={v => patchConfig(selected.id, { textAlign: v })}>
                                                <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="left">Sinistra</SelectItem>
                                                    <SelectItem value="center">Centro</SelectItem>
                                                    <SelectItem value="right">Destra</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>
                                        <div className="grid grid-cols-2 gap-2">
                                            <div className="flex items-center gap-2">
                                                <input type="checkbox" id="label-bold" checked={!!selected.config?.bold} onChange={e => patchConfig(selected.id, { bold: e.target.checked })} />
                                                <Label htmlFor="label-bold" className="text-xs cursor-pointer">Grassetto</Label>
                                            </div>
                                            <div className="flex items-center gap-2">
                                                <input type="checkbox" id="label-italic" checked={!!selected.config?.italic} onChange={e => patchConfig(selected.id, { italic: e.target.checked })} />
                                                <Label htmlFor="label-italic" className="text-xs cursor-pointer">Corsivo</Label>
                                            </div>
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Sfondo</Label>
                                            <input type="color" value={String(selected.config?.labelBgColor ?? '#00000000')} onChange={e => patchConfig(selected.id, { labelBgColor: e.target.value })} className="h-8 w-full rounded border bg-transparent" />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Tag per {'{{value}}'}</Label>
                                            <TagCombobox tags={tags} value={selected.config?.tagBinding != null ? String(selected.config.tagBinding) : 'none'} onChange={v => patchConfig(selected.id, { tagBinding: v === 'none' ? undefined : Number(v) })} />
                                        </div>
                                    </div>
                                )}

                                {/* ── Pipe extras ── */}
                                {selected.type === 'pipe' && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="flow-enabled" checked={!!selected.config?.flowEnabled} onChange={e => patchConfig(selected.id, { flowEnabled: e.target.checked })} />
                                            <Label htmlFor="flow-enabled" className="text-xs cursor-pointer">Animazione flusso</Label>
                                        </div>
                                        {selected.config?.flowEnabled && (
                                            <>
                                                <div className="grid gap-1">
                                                    <Label className="text-xs">Tag flusso (ON quando ≥ 1)</Label>
                                                    <TagCombobox tags={tags} value={selected.config?.tagFlow != null ? String(selected.config.tagFlow) : 'none'} onChange={v => patchConfig(selected.id, { tagFlow: v === 'none' ? undefined : Number(v) })} />
                                                </div>
                                                <div className="grid gap-1">
                                                    <Label className="text-xs">Direzione</Label>
                                                    <Select value={String(selected.config?.flowDirection ?? 'right')} onValueChange={v => patchConfig(selected.id, { flowDirection: v })}>
                                                        <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                                        <SelectContent>
                                                            <SelectItem value="right">→ Destra</SelectItem>
                                                            <SelectItem value="left">← Sinistra</SelectItem>
                                                            <SelectItem value="down">↓ Giù</SelectItem>
                                                            <SelectItem value="up">↑ Su</SelectItem>
                                                        </SelectContent>
                                                    </Select>
                                                </div>
                                                <div className="grid gap-1">
                                                    <Label className="text-xs">Colore flusso</Label>
                                                    <input type="color" value={String(selected.config?.flowColor ?? '#ffffff')} onChange={e => patchConfig(selected.id, { flowColor: e.target.value })} className="h-8 w-full rounded border bg-transparent" />
                                                </div>
                                            </>
                                        )}
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Spessore tubo (px)</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.strokeWidth ?? 20} onChange={e => patchConfig(selected.id, { strokeWidth: parseInt(e.target.value) || 20 })} />
                                        </div>
                                    </div>
                                )}

                                {/* ── Image extras ── */}
                                {selected.type === 'image' && (
                                    <div className="space-y-2 pt-1 border-t">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Immagine</Label>
                                            <input type="file" accept="image/*" className="text-xs text-muted-foreground"
                                                onChange={(e) => {
                                                    const file = e.target.files?.[0];
                                                    if (!file) return;
                                                    const reader = new FileReader();
                                                    reader.onload = (ev) => { patchConfig(selected.id, { imageUrl: ev.target?.result as string }); };
                                                    reader.readAsDataURL(file);
                                                }} />
                                            {selected.config?.imageUrl && <span className="text-[10px] text-emerald-400">✓ Caricata</span>}
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">URL immagine</Label>
                                            <Input className="h-8 text-xs" placeholder="https://..." value={String(selected.config?.imageUrl ?? '')} onChange={e => patchConfig(selected.id, { imageUrl: e.target.value || undefined })} />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Adattamento</Label>
                                            <Select value={String(selected.config?.imageObjectFit ?? 'fill')} onValueChange={v => patchConfig(selected.id, { imageObjectFit: v })}>
                                                <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="fill">Riempi</SelectItem>
                                                    <SelectItem value="contain">Contieni</SelectItem>
                                                    <SelectItem value="cover">Copri</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Opacità (%)</Label>
                                            <Input className="h-8 text-xs" type="number" min={0} max={100} value={selected.config?.opacity ?? 100} onChange={e => patchConfig(selected.id, { opacity: parseInt(e.target.value) || 100 })} />
                                        </div>
                                    </div>
                                )}

                                {/* ── Clock config ── */}
                                {selected.type === 'clock' && (
                                    <div className="grid gap-2">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Dim. testo (px)</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.fontSize ?? 22} onChange={e => patchConfig(selected.id, { fontSize: parseInt(e.target.value) || 22 })} />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Formato ora</Label>
                                            <Select value={selected.config?.clockFormat ?? '24h'} onValueChange={v => patchConfig(selected.id, { clockFormat: v })}>
                                                <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="24h">24 ore</SelectItem>
                                                    <SelectItem value="12h">12 ore (AM/PM)</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="clock-show-date" checked={selected.config?.showDate !== false} onChange={e => patchConfig(selected.id, { showDate: e.target.checked })} />
                                            <Label htmlFor="clock-show-date" className="text-xs cursor-pointer">Mostra data</Label>
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Colore testo</Label>
                                            <div className="flex gap-2 items-center">
                                                <input type="color" value={selected.config?.color ?? '#e2e8f0'} onChange={e => patchConfig(selected.id, { color: e.target.value })} className="h-8 w-10 rounded border border-input cursor-pointer" />
                                                <Input className="h-8 text-xs flex-1" value={selected.config?.color ?? '#e2e8f0'} onChange={e => patchConfig(selected.id, { color: e.target.value })} />
                                            </div>
                                        </div>
                                    </div>
                                )}

                                {/* ── Setpoint config ── */}
                                {selected.type === 'setpoint' && (
                                    <div className="grid gap-2">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Dim. testo (px)</Label>
                                            <Input className="h-8 text-xs" type="number" value={selected.config?.fontSize ?? 22} onChange={e => patchConfig(selected.id, { fontSize: parseInt(e.target.value) || 22 })} />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Unità di misura</Label>
                                            <Input className="h-8 text-xs" placeholder="es. °C, bar, %" value={selected.config?.unit ?? ''} onChange={e => patchConfig(selected.id, { unit: e.target.value })} />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Decimali</Label>
                                            <Input className="h-8 text-xs" type="number" min={0} max={6} value={selected.config?.decimals ?? 2} onChange={e => patchConfig(selected.id, { decimals: parseInt(e.target.value) ?? 2 })} />
                                        </div>
                                        <div className="grid grid-cols-2 gap-2">
                                            <div className="grid gap-1">
                                                <Label className="text-xs">Min</Label>
                                                <Input className="h-8 text-xs" type="number" placeholder="—" value={selected.config?.spMin ?? ''} onChange={e => patchConfig(selected.id, { spMin: e.target.value === '' ? undefined : parseFloat(e.target.value) })} />
                                            </div>
                                            <div className="grid gap-1">
                                                <Label className="text-xs">Max</Label>
                                                <Input className="h-8 text-xs" type="number" placeholder="—" value={selected.config?.spMax ?? ''} onChange={e => patchConfig(selected.id, { spMax: e.target.value === '' ? undefined : parseFloat(e.target.value) })} />
                                            </div>
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Step (incremento)</Label>
                                            <Input className="h-8 text-xs" type="number" min={0} step="any" placeholder="1" value={selected.config?.spStep ?? ''} onChange={e => patchConfig(selected.id, { spStep: e.target.value === '' ? undefined : parseFloat(e.target.value) })} />
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input type="checkbox" id="sp-confirm-write" checked={!!selected.config?.confirmWrite} onChange={e => patchConfig(selected.id, { confirmWrite: e.target.checked })} />
                                            <Label htmlFor="sp-confirm-write" className="text-xs cursor-pointer">Conferma prima di scrivere</Label>
                                        </div>
                                    </div>
                                )}

                                {/* ── Alarm blink for all tag-bound widgets ── */}
                                {WIDGET_CATALOG.find(c => c.type === selected.type)?.needsTag && (
                                    <div className="flex items-center gap-2 pt-1 border-t">
                                        <input type="checkbox" id="blink-alarm-all" checked={!!selected.config?.blinkOnAlarm} onChange={e => patchConfig(selected.id, { blinkOnAlarm: e.target.checked })} />
                                        <Label htmlFor="blink-alarm-all" className="text-xs cursor-pointer">Lampeggia in allarme</Label>
                                    </div>
                                )}

                                {/* Z-order */}
                                <div className="flex gap-1 pt-1 border-t">
                                    <Button variant="outline" size="sm" className="flex-1 h-7 text-xs"
                                        onClick={() => setWidgets(prev => {
                                            const idx = prev.findIndex(w => w.id === selected.id);
                                            if (idx <= 0) return prev;
                                            const next = [...prev];
                                            [next[idx - 1], next[idx]] = [next[idx], next[idx - 1]];
                                            return next;
                                        })}>↓ Indietro</Button>
                                    <Button variant="outline" size="sm" className="flex-1 h-7 text-xs"
                                        onClick={() => setWidgets(prev => {
                                            const idx = prev.findIndex(w => w.id === selected.id);
                                            if (idx >= prev.length - 1) return prev;
                                            const next = [...prev];
                                            [next[idx], next[idx + 1]] = [next[idx + 1], next[idx]];
                                            return next;
                                        })}>↑ Avanti</Button>
                                </div>

                                <div className="grid grid-cols-2 gap-2">
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Larghezza</Label>
                                        <Input className="h-8 text-xs" type="number" value={selected.w} onChange={e => patchWidget(selected.id, { w: parseInt(e.target.value) || 20 })} />
                                    </div>
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Altezza</Label>
                                        <Input className="h-8 text-xs" type="number" value={selected.h} onChange={e => patchWidget(selected.id, { h: parseInt(e.target.value) || 20 })} />
                                    </div>
                                    <div className="grid gap-1">
                                        <Label className="text-xs">X</Label>
                                        <Input className="h-8 text-xs" type="number" value={selected.x} onChange={e => patchWidget(selected.id, { x: parseInt(e.target.value) || 0 })} />
                                    </div>
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Y</Label>
                                        <Input className="h-8 text-xs" type="number" value={selected.y} onChange={e => patchWidget(selected.id, { y: parseInt(e.target.value) || 0 })} />
                                    </div>
                                    <div className="grid gap-1 col-span-2">
                                        <Label className="text-xs">Rotazione (°)</Label>
                                        <Input className="h-8 text-xs" type="number" value={selected.rotation ?? 0} onChange={e => patchWidget(selected.id, { rotation: parseInt(e.target.value) || 0 })} />
                                    </div>
                                </div>
                            </div>
                        )}
                    </div>
                )}
            </div>
        </div>
    );
};

export default SynopticEditorPage;
