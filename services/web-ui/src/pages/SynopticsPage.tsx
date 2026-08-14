import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
    Plus, Trash2, Pencil, Monitor, LayoutTemplate, Factory, MapPin,
    ChevronRight, ChevronDown, FolderOpen, Folder,
} from 'lucide-react';
import { synopticsApi, Synoptic } from '@/api/synoptics';
import { sitesApi } from '@/api/sites';
import { areasApi } from '@/api/areas';
import { Site, Area } from '@/types';
import { useAuthStore } from '@/stores/useAuthStore';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent } from '@/components/ui/card';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import { cn } from '@/lib/utils';
import { SynopticWidgetView } from '@/components/synoptics/SynopticWidget';

// Scope of the currently selected tree node — drives the right-hand panel and
// pre-fills the "new synoptic" dialog so a page is filed under the right line.
type NodeScope =
    | { kind: 'all' }
    | { kind: 'site'; siteId: number }
    | { kind: 'area'; siteId: number; areaId: number }
    | { kind: 'general' }; // synoptics with no site/area

function TreeRow({ label, depth, active, count, icon, expandable, isExpanded, onToggle, onClick }: {
    label: string; depth: number; active: boolean; count: number;
    icon: React.ReactNode; expandable?: boolean; isExpanded?: boolean;
    onToggle?: () => void; onClick: () => void;
}) {
    return (
        <div
            onClick={onClick}
            className={cn('flex items-center gap-1 py-1.5 pr-2 rounded-md cursor-pointer text-sm hover:bg-muted/60 transition-colors',
                active && 'bg-primary/10 text-primary font-medium')}
            style={{ paddingLeft: 8 + depth * 14 }}
        >
            {expandable ? (
                <button onClick={(e) => { e.stopPropagation(); onToggle?.(); }} className="p-0.5 -ml-1 text-muted-foreground hover:text-foreground">
                    {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                </button>
            ) : <span className="w-3.5" />}
            {icon}
            <span className="truncate flex-1">{label}</span>
            {count > 0 && <span className="text-[10px] text-muted-foreground tabular-nums">{count}</span>}
        </div>
    );
}

function SynopticThumb({ s, h = 130 }: { s: Synoptic; h?: number }) {
    const w = h * (s.canvas_w / s.canvas_h);
    const scale = Math.min(w / s.canvas_w, h / s.canvas_h);
    return (
        <div className="relative overflow-hidden rounded-md border" style={{ width: w, height: h, background: s.background_color }}>
            <div style={{ width: s.canvas_w, height: s.canvas_h, transform: `scale(${scale})`, transformOrigin: 'top left' }}>
                {(s.layout || []).map(wdg => (
                    <div key={wdg.id} className="absolute" style={{ left: wdg.x, top: wdg.y, width: wdg.w, height: wdg.h, transform: wdg.rotation ? `rotate(${wdg.rotation}deg)` : undefined }}>
                        <SynopticWidgetView widget={wdg} />
                    </div>
                ))}
            </div>
        </div>
    );
}

const SynopticsPage = () => {
    const navigate = useNavigate();
    const { isAdmin } = useAuthStore();
    const { selectedOrgId } = useNavigationStore();

    const [sites, setSites] = useState<Site[]>([]);
    const [areas, setAreas] = useState<Area[]>([]);
    const [items, setItems] = useState<Synoptic[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [expanded, setExpanded] = useState<Set<string>>(new Set());
    const [scope, setScope] = useState<NodeScope>({ kind: 'all' });

    // Create dialog
    const [isOpen, setIsOpen] = useState(false);
    const [name, setName] = useState('');
    const [description, setDescription] = useState('');
    const [formSiteId, setFormSiteId] = useState<string>('none');
    const [formAreaId, setFormAreaId] = useState<string>('none');

    const load = async () => {
        try {
            const [siteList, areaList, synRes] = await Promise.all([
                selectedOrgId ? sitesApi.getAll(selectedOrgId) : Promise.resolve([]),
                areasApi.getAll().catch(() => []),
                synopticsApi.list(),
            ]);
            setSites(siteList);
            setAreas(areaList);
            setItems(synRes.items);
        } catch (e) {
            console.error('Failed to load synoptics tree', e);
        } finally {
            setIsLoading(false);
        }
    };

    useEffect(() => { load(); }, [selectedOrgId]);

    const areasBySite = useMemo(() => {
        const m = new Map<number, Area[]>();
        areas.forEach(a => {
            if (!m.has(a.site_id)) m.set(a.site_id, []);
            m.get(a.site_id)!.push(a);
        });
        return m;
    }, [areas]);

    const countFor = (scope: NodeScope): number =>
        items.filter(s => matchesScope(s, scope)).length;

    const visibleItems = useMemo(
        () => items.filter(s => matchesScope(s, scope)),
        [items, scope],
    );

    const toggle = (key: string) =>
        setExpanded(prev => {
            const next = new Set(prev);
            if (next.has(key)) { next.delete(key); } else { next.add(key); }
            return next;
        });

    const openCreate = () => {
        // Pre-fill site/area from the currently selected node.
        if (scope.kind === 'area') { setFormSiteId(String(scope.siteId)); setFormAreaId(String(scope.areaId)); }
        else if (scope.kind === 'site') { setFormSiteId(String(scope.siteId)); setFormAreaId('none'); }
        else { setFormSiteId('none'); setFormAreaId('none'); }
        setName('');
        setDescription('');
        setIsOpen(true);
    };

    const handleCreate = async () => {
        if (!name.trim()) return;
        try {
            const { id } = await synopticsApi.create({
                name: name.trim(), description,
                site_id: formSiteId === 'none' ? null : Number(formSiteId),
                area_id: formAreaId === 'none' ? null : Number(formAreaId),
                background_color: '#0f172a', canvas_w: 1280, canvas_h: 720, layout: [],
            });
            setIsOpen(false);
            navigate(`/synoptics/${id}/edit`);
        } catch (e) {
            console.error('Failed to create synoptic', e);
        }
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (!confirm('Eliminare questo sinottico?')) return;
        await synopticsApi.remove(id);
        load();
    };

    if (isLoading) {
        return <div className="p-8 text-center text-muted-foreground">Caricamento sinottici...</div>;
    }

    const scopeTitle = (): string => {
        if (scope.kind === 'all') return 'Tutti i sinottici';
        if (scope.kind === 'general') return 'Generali (senza linea)';
        if (scope.kind === 'site') return sites.find(s => s.id === scope.siteId)?.name || 'Sito';
        return areas.find(a => a.id === scope.areaId)?.name || 'Area';
    };

    return (
        <div className="space-y-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Sinottici</h2>
                    <p className="text-muted-foreground">Mimici SCADA organizzati per impianto, sito e linea.</p>
                </div>
                {isAdmin() && (
                    <Button className="gap-2" onClick={openCreate}><Plus size={16} /> Nuovo sinottico</Button>
                )}
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-[260px_1fr] gap-4">
                {/* Work tree */}
                <Card className="h-fit">
                    <CardContent className="p-2">
                        <TreeRow label="Tutti i sinottici" depth={0} active={scope.kind === 'all'}
                            count={items.length} icon={<LayoutTemplate size={15} className="text-muted-foreground" />}
                            onClick={() => setScope({ kind: 'all' })} />

                        {sites.map(site => {
                            const siteAreas = areasBySite.get(site.id) || [];
                            const key = `site-${site.id}`;
                            const isExp = expanded.has(key);
                            return (
                                <div key={site.id}>
                                    <TreeRow label={site.name} depth={0}
                                        active={scope.kind === 'site' && scope.siteId === site.id}
                                        count={countFor({ kind: 'site', siteId: site.id })}
                                        icon={<Factory size={15} className="text-muted-foreground" />}
                                        expandable={siteAreas.length > 0} isExpanded={isExp}
                                        onToggle={() => toggle(key)}
                                        onClick={() => setScope({ kind: 'site', siteId: site.id })} />
                                    {isExp && siteAreas.map(area => (
                                        <TreeRow key={area.id} label={area.name} depth={1}
                                            active={scope.kind === 'area' && scope.areaId === area.id}
                                            count={countFor({ kind: 'area', siteId: site.id, areaId: area.id })}
                                            icon={<MapPin size={14} className="text-muted-foreground" />}
                                            onClick={() => setScope({ kind: 'area', siteId: site.id, areaId: area.id })} />
                                    ))}
                                </div>
                            );
                        })}

                        <TreeRow label="Generali" depth={0} active={scope.kind === 'general'}
                            count={countFor({ kind: 'general' })}
                            icon={<Folder size={15} className="text-muted-foreground" />}
                            onClick={() => setScope({ kind: 'general' })} />
                    </CardContent>
                </Card>

                {/* Content: synoptic pages under the selected node */}
                <div className="space-y-3">
                    <div className="flex items-center gap-2 text-sm text-muted-foreground px-1">
                        <FolderOpen size={16} />
                        <span className="font-medium text-foreground">{scopeTitle()}</span>
                        <span>· {visibleItems.length} pagine</span>
                        {isAdmin() && (
                            <Button variant="ghost" size="sm" className="ml-auto h-9 sm:h-7 gap-1 text-xs" onClick={openCreate}>
                                <Plus size={14} /> Aggiungi qui
                            </Button>
                        )}
                    </div>

                    {visibleItems.length === 0 ? (
                        <Card>
                            <CardContent className="py-16 flex flex-col items-center text-center gap-3 text-muted-foreground">
                                <LayoutTemplate size={40} className="opacity-40" />
                                <p>Nessuna pagina sinottico in questo nodo.{isAdmin() && ' Aggiungine una con "Aggiungi qui".'}</p>
                            </CardContent>
                        </Card>
                    ) : (
                        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
                            {visibleItems.map(s => (
                                <Card key={s.id} className="overflow-hidden hover:shadow-md transition-shadow cursor-pointer" onClick={() => navigate(`/synoptics/${s.id}`)}>
                                    <div className="flex items-center justify-center bg-muted/30 p-2">
                                        <SynopticThumb s={s} />
                                    </div>
                                    <CardContent className="p-3 flex items-center justify-between gap-2">
                                        <div className="min-w-0">
                                            <p className="font-semibold truncate">{s.name}</p>
                                            <p className="text-xs text-muted-foreground truncate">{s.description || `${(s.layout || []).length} widget`}</p>
                                        </div>
                                        <div className="flex items-center gap-1 shrink-0">
                                            <Button variant="ghost" size="icon" className="h-10 sm:h-8 w-10 sm:w-8" title="Visualizza" onClick={(e) => { e.stopPropagation(); navigate(`/synoptics/${s.id}`); }}>
                                                <Monitor size={16} />
                                            </Button>
                                            {isAdmin() && (
                                                <>
                                                    <Button variant="ghost" size="icon" className="h-10 sm:h-8 w-10 sm:w-8" title="Modifica" onClick={(e) => { e.stopPropagation(); navigate(`/synoptics/${s.id}/edit`); }}>
                                                        <Pencil size={16} />
                                                    </Button>
                                                    <Button variant="ghost" size="icon" className="h-10 sm:h-8 w-10 sm:w-8 text-destructive hover:text-destructive hover:bg-destructive/10" title="Elimina" onClick={(e) => handleDelete(e, s.id)}>
                                                        <Trash2 size={16} />
                                                    </Button>
                                                </>
                                            )}
                                        </div>
                                    </CardContent>
                                </Card>
                            ))}
                        </div>
                    )}
                </div>
            </div>

            {/* Create dialog */}
            <Dialog open={isOpen} onOpenChange={setIsOpen}>
                <DialogContent>
                    <DialogHeader><DialogTitle>Nuovo sinottico</DialogTitle></DialogHeader>
                    <div className="grid gap-4 py-2">
                        <div className="grid gap-2">
                            <Label htmlFor="syn-name">Nome</Label>
                            <Input id="syn-name" value={name} onChange={e => setName(e.target.value)} placeholder="es. Overview" />
                        </div>
                        <div className="grid gap-2">
                            <Label htmlFor="syn-desc">Descrizione</Label>
                            <Input id="syn-desc" value={description} onChange={e => setDescription(e.target.value)} placeholder="opzionale" />
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                            <div className="grid gap-2">
                                <Label className="text-xs">Sito</Label>
                                <Select value={formSiteId} onValueChange={(v) => { setFormSiteId(v); setFormAreaId('none'); }}>
                                    <SelectTrigger><SelectValue placeholder="Sito" /></SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="none">Nessuno</SelectItem>
                                        {sites.map(s => <SelectItem key={s.id} value={String(s.id)}>{s.name}</SelectItem>)}
                                    </SelectContent>
                                </Select>
                            </div>
                            <div className="grid gap-2">
                                <Label className="text-xs">Linea / Area</Label>
                                <Select value={formAreaId} onValueChange={setFormAreaId} disabled={formSiteId === 'none'}>
                                    <SelectTrigger><SelectValue placeholder="Area" /></SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="none">Nessuna</SelectItem>
                                        {(areasBySite.get(Number(formSiteId)) || []).map(a => <SelectItem key={a.id} value={String(a.id)}>{a.name}</SelectItem>)}
                                    </SelectContent>
                                </Select>
                            </div>
                        </div>
                    </div>
                    <DialogFooter><Button onClick={handleCreate}>Crea e apri designer</Button></DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
};

// Whether a synoptic belongs to the given tree node scope.
function matchesScope(s: Synoptic, scope: NodeScope): boolean {
    switch (scope.kind) {
        case 'all': return true;
        case 'general': return s.site_id == null && s.area_id == null;
        case 'site': return s.site_id === scope.siteId;
        case 'area': return s.area_id === scope.areaId;
    }
}

export default SynopticsPage;
