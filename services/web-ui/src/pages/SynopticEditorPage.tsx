import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { ArrowLeft, Save, Trash2, Loader2, Pencil, Monitor, Check, AlertTriangle } from 'lucide-react';
import { synopticsApi, Synoptic, SynopticWidget } from '@/api/synoptics';
import { tagsApi } from '@/api/tags';
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
import { cn } from '@/lib/utils';
import { SynopticWidgetView, WIDGET_CATALOG, LiveValue } from '@/components/synoptics/SynopticWidget';

const uid = () => Math.random().toString(36).slice(2, 10);
const SNAP = 8; // grid snap size in canvas-pixels

const snap = (v: number) => Math.round(v / SNAP) * SNAP;

interface DragState {
    id: string;
    startX: number;
    startY: number;
    origX: number;
    origY: number;
}

const SynopticEditorPage = ({ mode }: { mode: 'view' | 'edit' }) => {
    const { id } = useParams<{ id: string }>();
    const navigate = useNavigate();
    const { isAdmin } = useAuthStore();
    const { selectedOrgId } = useNavigationStore();
    const { values: liveValues, connected: wsConnected } = useRealtime(selectedOrgId ?? undefined);

    const [synoptic, setSynoptic] = useState<Synoptic | null>(null);
    const [widgets, setWidgets] = useState<SynopticWidget[]>([]);
    const [tags, setTags] = useState<TagWithHierarchy[]>([]);
    const [selectedId, setSelectedId] = useState<string | null>(null);
    const [isLoading, setIsLoading] = useState(true);
    const [isSaving, setIsSaving] = useState(false);
    const [saveStatus, setSaveStatus] = useState<'idle' | 'ok' | 'error'>('idle');
    const [loadError, setLoadError] = useState<string | null>(null);
    const [scale, setScale] = useState(1);

    const isEdit = mode === 'edit';
    const canvasWrapRef = useRef<HTMLDivElement>(null);
    const dragRef = useRef<DragState | null>(null);

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
                setWidgets(s.layout || []);
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

    const selected = useMemo(() => widgets.find(w => w.id === selectedId) || null, [widgets, selectedId]);

    const tagValue = useCallback((tagId?: number | null): LiveValue | undefined => {
        if (tagId == null) return undefined; // unbound widget → always preview
        if (mode !== 'view') return undefined; // edit mode → preview
        const v = liveValues.get(tagId);
        // Tag is bound but no live data yet (edge offline / WS not connected) →
        // return BAD-quality null so widgets show "—" / grey / off state.
        return v ? { value: v.value, quality: v.quality } : { value: null, quality: 2 };
    }, [liveValues, mode]);

    // ── Editing actions ────────────────────────────────────────────────────
    const addWidget = (type: SynopticWidget['type']) => {
        const meta = WIDGET_CATALOG.find(w => w.type === type)!;
        const w: SynopticWidget = {
            id: uid(), type, x: 40, y: 40, w: meta.defaultW, h: meta.defaultH,
            label: type === 'label' ? 'Etichetta' : '',
            config: type === 'gauge' || type === 'tank' ? { min: 0, max: 100 } : {},
        };
        setWidgets(prev => [...prev, w]);
        setSelectedId(w.id);
    };

    const patchWidget = (wid: string, patch: Partial<SynopticWidget>) => {
        setWidgets(prev => prev.map(w => w.id === wid ? { ...w, ...patch } : w));
    };
    const patchConfig = (wid: string, patch: Record<string, unknown>) => {
        setWidgets(prev => prev.map(w => w.id === wid ? { ...w, config: { ...w.config, ...patch } } : w));
    };
    const removeWidget = (wid: string) => {
        setWidgets(prev => prev.filter(w => w.id !== wid));
        setSelectedId(null);
    };

    // Drag-to-move (pointer events, scale-aware).
    // Capture is set on the widget div (currentTarget) so events follow the pointer
    // even when it moves outside the widget's bounds. Move/Up handlers live on the
    // canvas div to avoid losing them during fast drags.
    const onWidgetPointerDown = (e: React.PointerEvent, w: SynopticWidget) => {
        if (!isEdit) return;
        e.stopPropagation();
        setSelectedId(w.id);
        dragRef.current = { id: w.id, startX: e.clientX, startY: e.clientY, origX: w.x, origY: w.y };
        (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    };
    const onCanvasPointerMove = (e: React.PointerEvent) => {
        const d = dragRef.current;
        if (!d) return;
        const dx = (e.clientX - d.startX) / scale;
        const dy = (e.clientY - d.startY) / scale;
        patchWidget(d.id, { x: snap(d.origX + dx), y: snap(d.origY + dy) });
    };
    const onCanvasPointerUp = () => {
        dragRef.current = null;
    };

    // Keyboard shortcuts: Delete/Backspace to remove, Arrow to nudge, Escape to deselect.
    useEffect(() => {
        if (!isEdit) return;
        const handler = (e: KeyboardEvent) => {
            if ((e.target as HTMLElement).tagName === 'INPUT') return;
            if ((e.key === 'Delete' || e.key === 'Backspace') && selectedId) {
                removeWidget(selectedId);
            } else if (e.key === 'Escape') {
                setSelectedId(null);
            } else if (e.key.startsWith('Arrow') && selectedId) {
                e.preventDefault();
                const step = e.shiftKey ? SNAP : 1;
                setWidgets(prev => prev.map(w => {
                    if (w.id !== selectedId) return w;
                    const dx = e.key === 'ArrowLeft' ? -step : e.key === 'ArrowRight' ? step : 0;
                    const dy = e.key === 'ArrowUp' ? -step : e.key === 'ArrowDown' ? step : 0;
                    return { ...w, x: w.x + dx, y: w.y + dy };
                }));
            }
        };
        window.addEventListener('keydown', handler);
        return () => window.removeEventListener('keydown', handler);
    }, [isEdit, selectedId]);

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

    const tagLabel = (tagId?: number | null) => {
        if (tagId == null) return 'Nessun tag';
        const t = tags.find(t => t.id === tagId);
        return t ? (t.alias || t.code) : `Tag #${tagId}`;
    };

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
                        onClick={() => isEdit && setSelectedId(null)}
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
                                    onClick={(e) => { if (isEdit) { e.stopPropagation(); setSelectedId(w.id); } }}
                                    className={cn('absolute select-none', isEdit && 'cursor-move',
                                        isEdit && selectedId === w.id && 'outline outline-2 outline-primary outline-offset-2')}
                                    style={{ left: w.x, top: w.y, width: w.w, height: w.h, transform: w.rotation ? `rotate(${w.rotation}deg)` : undefined }}
                                >
                                    <SynopticWidgetView widget={w} live={tagValue(w.tagId)} />
                                </div>
                            ))}
                        </div>
                    </div>
                </div>

                {/* Properties panel (edit only) */}
                {isEdit && (
                    <div className="space-y-3">
                        <p className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Proprietà</p>
                        {!selected ? (
                            <div className="space-y-3">
                                <p className="text-xs text-muted-foreground">Seleziona un componente, oppure configura la pagina:</p>
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
                                    <Button variant="ghost" size="icon" className="h-7 w-7 text-destructive" onClick={() => removeWidget(selected.id)}><Trash2 size={15} /></Button>
                                </div>

                                {WIDGET_CATALOG.find(c => c.type === selected.type)?.needsTag && (
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Tag</Label>
                                        <Select value={selected.tagId != null ? String(selected.tagId) : 'none'}
                                            onValueChange={v => patchWidget(selected.id, { tagId: v === 'none' ? null : Number(v) })}>
                                            <SelectTrigger className="h-8 text-xs"><SelectValue>{tagLabel(selected.tagId)}</SelectValue></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="none">Nessun tag</SelectItem>
                                                {tags.map(t => <SelectItem key={t.id} value={String(t.id)}>{t.alias || t.code}</SelectItem>)}
                                            </SelectContent>
                                        </Select>
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

                                {(selected.type === 'gauge' || selected.type === 'tank') && (
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

                                {(selected.type === 'indicator' || selected.type === 'pump' || selected.type === 'valve' || selected.type === 'motor') && (
                                    <div className="grid gap-1">
                                        <Label className="text-xs">Valore "attivo" (≥)</Label>
                                        <Input className="h-8 text-xs" type="number" value={selected.config?.onValue ?? 1} onChange={e => patchConfig(selected.id, { onValue: parseFloat(e.target.value) || 0 })} />
                                    </div>
                                )}

                                {(selected.type === 'label' || selected.type === 'pipe' || selected.type === 'indicator' || selected.type === 'pump' || selected.type === 'valve' || selected.type === 'motor') && (
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

                                <div className="grid grid-cols-2 gap-2 pt-1 border-t">
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
