import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Trash2, Pencil, Monitor, LayoutTemplate } from 'lucide-react';
import { synopticsApi, Synoptic } from '@/api/synoptics';
import { useAuthStore } from '@/stores/useAuthStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent } from '@/components/ui/card';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogTrigger,
} from '@/components/ui/dialog';
import { SynopticWidgetView } from '@/components/synoptics/SynopticWidget';

// Mini non-interactive preview of a synoptic, scaled into a fixed thumbnail.
function SynopticThumb({ s }: { s: Synoptic }) {
    const scale = Math.min(320 / s.canvas_w, 180 / s.canvas_h);
    return (
        <div className="relative overflow-hidden rounded-md border" style={{ width: 320, height: 180, background: s.background_color }}>
            <div style={{ width: s.canvas_w, height: s.canvas_h, transform: `scale(${scale})`, transformOrigin: 'top left' }}>
                {(s.layout || []).map(w => (
                    <div key={w.id} className="absolute" style={{ left: w.x, top: w.y, width: w.w, height: w.h, transform: w.rotation ? `rotate(${w.rotation}deg)` : undefined }}>
                        <SynopticWidgetView widget={w} />
                    </div>
                ))}
            </div>
        </div>
    );
}

const SynopticsPage = () => {
    const navigate = useNavigate();
    const { isAdmin } = useAuthStore();
    const [items, setItems] = useState<Synoptic[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isOpen, setIsOpen] = useState(false);
    const [name, setName] = useState('');
    const [description, setDescription] = useState('');

    const load = async () => {
        try {
            const res = await synopticsApi.list();
            setItems(res.items);
        } catch (e) {
            console.error('Failed to load synoptics', e);
        } finally {
            setIsLoading(false);
        }
    };

    useEffect(() => { load(); }, []);

    const handleCreate = async () => {
        if (!name.trim()) return;
        try {
            const { id } = await synopticsApi.create({
                name: name.trim(), description, background_color: '#0f172a',
                canvas_w: 1280, canvas_h: 720, layout: [],
            });
            setIsOpen(false);
            setName('');
            setDescription('');
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

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Sinottici</h2>
                    <p className="text-muted-foreground">Mimici SCADA: pagine grafiche con simboli industriali legati ai tag in tempo reale.</p>
                </div>
                {isAdmin() && (
                    <Dialog open={isOpen} onOpenChange={setIsOpen}>
                        <DialogTrigger asChild>
                            <Button className="gap-2"><Plus size={16} /> Nuovo sinottico</Button>
                        </DialogTrigger>
                        <DialogContent>
                            <DialogHeader><DialogTitle>Nuovo sinottico</DialogTitle></DialogHeader>
                            <div className="grid gap-4 py-2">
                                <div className="grid gap-2">
                                    <Label htmlFor="syn-name">Nome</Label>
                                    <Input id="syn-name" value={name} onChange={e => setName(e.target.value)} placeholder="es. Linea 1 — Overview" />
                                </div>
                                <div className="grid gap-2">
                                    <Label htmlFor="syn-desc">Descrizione</Label>
                                    <Input id="syn-desc" value={description} onChange={e => setDescription(e.target.value)} placeholder="opzionale" />
                                </div>
                            </div>
                            <DialogFooter><Button onClick={handleCreate}>Crea e apri designer</Button></DialogFooter>
                        </DialogContent>
                    </Dialog>
                )}
            </div>

            {items.length === 0 ? (
                <Card>
                    <CardContent className="py-16 flex flex-col items-center text-center gap-3 text-muted-foreground">
                        <LayoutTemplate size={40} className="opacity-40" />
                        <p>Nessun sinottico ancora. {isAdmin() ? 'Creane uno per disegnare la tua prima pagina SCADA.' : 'Chiedi a un amministratore di crearne uno.'}</p>
                    </CardContent>
                </Card>
            ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
                    {items.map(s => (
                        <Card key={s.id} className="overflow-hidden hover:shadow-md transition-shadow cursor-pointer" onClick={() => navigate(`/synoptics/${s.id}`)}>
                            <SynopticThumb s={s} />
                            <CardContent className="p-3 flex items-center justify-between gap-2">
                                <div className="min-w-0">
                                    <p className="font-semibold truncate">{s.name}</p>
                                    <p className="text-xs text-muted-foreground truncate">{s.description || `${(s.layout || []).length} widget`}</p>
                                </div>
                                <div className="flex items-center gap-1 shrink-0">
                                    <Button variant="ghost" size="icon" className="h-8 w-8" title="Visualizza" onClick={(e) => { e.stopPropagation(); navigate(`/synoptics/${s.id}`); }}>
                                        <Monitor size={16} />
                                    </Button>
                                    {isAdmin() && (
                                        <>
                                            <Button variant="ghost" size="icon" className="h-8 w-8" title="Modifica" onClick={(e) => { e.stopPropagation(); navigate(`/synoptics/${s.id}/edit`); }}>
                                                <Pencil size={16} />
                                            </Button>
                                            <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10" title="Elimina" onClick={(e) => handleDelete(e, s.id)}>
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
    );
};

export default SynopticsPage;
