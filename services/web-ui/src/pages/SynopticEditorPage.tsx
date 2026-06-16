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
    const [selectedIds, setSelectedIds] = useState<string[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isSaving, setIsSaving] = useState(false);
    const [saveStatus, setSaveStatus] = useState<'idle' | 'ok' | 'error'>('idle');
    const [loadError, setLoadError] = useState<string | null>(null);
    const [scale, setScale] = useState(1);

    const isEdit = mode === 'edit';
    const canvasWrapRef = useRef<HTMLDivElement>(null);
    const dragRef = useRef<DragState | null>(null);
    const resizeRef = useRef<ResizeState | null>(null);

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
        }
        return ids;
    }, [widgets]);

    const { values: liveValues, connected: wsConnected } = useRealtime(selectedOrgId ?? undefined, canvasTagIds);

    // Load synoptic + tags.
    useEffect(() => {
        if (!id) return;
        (async () => {
            try {
                const [s, tagList] = await Promise.all([
                    synopticsApi.get(Number(id)),
                    tagsApi.getAllWithHierarchy().catch(() => []),
                ]);
                setSynoptic(s);
                const initial = s.layout || [];
                setWidgets(initial);
                historyRef.current = [JSON.parse(JSON.stringify(initial))];
                historyIdxRef.current = 0;
                setTags(tagList);
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
        if (tag?.scaling_enabled && !tag.invert) {
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
        setSelectedIds(prev => prev.filter(id => id !== wid));
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
                prev.includes(w.id) ? prev.filter(id => id !== w.id) : [...prev, w.id]
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
                                        onWrite={w.tagId != null ? (v) => onWrite(w.tagId!, v) : undefined} />
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

                                {selected.type === 'image' && (
                                    <div className="space-y-2">
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Immagine</Label>
                                            <input type="file" accept="image/*"
                                                className="text-xs"
                                                onChange={(e) => {
                                                    const file = e.target.files?.[0];
                                                    if (!file) return;
                                                    const reader = new FileReader();
                                                    reader.onload = (ev) => {
                                                        patchConfig(selected.id, { imageUrl: ev.target?.result as string });
                                                    };
                                                    reader.readAsDataURL(file);
                                                }} />
                                        </div>
                                        <div className="grid gap-1">
                                            <Label className="text-xs">Adattamento</Label>
                                            <Select value={String(selected.config?.imageObjectFit ?? 'fill')}
                                                onValueChange={v => patchConfig(selected.id, { imageObjectFit: v })}>
                                                <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="fill">Fill</SelectItem>
                                                    <SelectItem value="contain">Contain</SelectItem>
                                                    <SelectItem value="cover">Cover</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>
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
