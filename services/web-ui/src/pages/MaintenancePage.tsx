import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Wrench, Plus, Pencil, Trash2, AlertCircle } from 'lucide-react';

import { maintenanceApi, MaintenanceWindow, CreateMaintenanceDto } from '@/api/maintenance';
import { showApiError, showApiSuccess } from '@/lib/api-error-handler';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog';

// Helper LOCAL ↔ RFC3339: gli input datetime-local lavorano in fuso locale;
// l'API vuole UTC RFC3339. Conversione esplicita per evitare drift.
const toLocalInput = (iso: string) => {
    const d = new Date(iso);
    const pad = (n: number) => `${n}`.padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
};
const fromLocalInput = (s: string) => new Date(s).toISOString();

const formatDateTime = (iso: string) =>
    new Date(iso).toLocaleString('it-IT', {
        day: '2-digit', month: '2-digit', year: 'numeric',
        hour: '2-digit', minute: '2-digit',
    });

const MaintenancePage = () => {
    const qc = useQueryClient();
    const { data: windows = [], isLoading } = useQuery({
        queryKey: ['maintenance'],
        queryFn: () => maintenanceApi.list(false),
    });

    const [editorOpen, setEditorOpen] = useState(false);
    const [editingId, setEditingId] = useState<number | null>(null);
    const [title, setTitle] = useState('');
    const [startAt, setStartAt] = useState('');
    const [endAt, setEndAt] = useState('');
    const [reason, setReason] = useState('');

    const openCreate = () => {
        setEditingId(null);
        setTitle('');
        // Default start = adesso, end = +1h. Sono inputs locali; l'API
        // riceve UTC quando salviamo.
        const now = new Date();
        const oneHour = new Date(now.getTime() + 60 * 60_000);
        setStartAt(toLocalInput(now.toISOString()));
        setEndAt(toLocalInput(oneHour.toISOString()));
        setReason('');
        setEditorOpen(true);
    };

    const openEdit = (w: MaintenanceWindow) => {
        setEditingId(w.id);
        setTitle(w.title);
        setStartAt(toLocalInput(w.start_at));
        setEndAt(toLocalInput(w.end_at));
        setReason(w.reason ?? '');
        setEditorOpen(true);
    };

    const saveMutation = useMutation({
        mutationFn: async () => {
            const data: CreateMaintenanceDto = {
                title,
                start_at: fromLocalInput(startAt),
                end_at: fromLocalInput(endAt),
                reason: reason.trim() || undefined,
            };
            if (editingId !== null) await maintenanceApi.update(editingId, data);
            else await maintenanceApi.create(data);
        },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['maintenance'] });
            showApiSuccess('Finestra salvata');
            setEditorOpen(false);
        },
        onError: (e) => showApiError(e, 'Salvataggio fallito'),
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => maintenanceApi.delete(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['maintenance'] });
            showApiSuccess('Finestra eliminata');
        },
        onError: (e) => showApiError(e, 'Eliminazione fallita'),
    });

    const handleDelete = (w: MaintenanceWindow) => {
        if (confirm(`Eliminare la finestra "${w.title}"?`)) {
            deleteMutation.mutate(w.id);
        }
    };

    const activeNow = windows.filter((w) => w.active);

    return (
        <div className="space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight flex items-center gap-2">
                        <Wrench size={22} /> Finestre di Manutenzione
                    </h2>
                    <p className="text-muted-foreground">
                        Durante una finestra attiva le notifiche email/Telegram sono silenziate
                        (gli allarmi restano in DB per audit). Usa per le fermate pianificate.
                    </p>
                </div>
                <Button onClick={openCreate} className="gap-2">
                    <Plus size={16} /> Nuova finestra
                </Button>
            </div>

            {activeNow.length > 0 && (
                <div className="rounded-md border border-amber-500/40 bg-amber-500/5 p-3 flex items-start gap-3">
                    <AlertCircle size={20} className="text-amber-500 mt-0.5" />
                    <div className="text-sm">
                        <p className="font-semibold">Manutenzione in corso adesso</p>
                        <ul className="list-disc list-inside text-xs text-muted-foreground mt-1">
                            {activeNow.map((w) => (
                                <li key={w.id}>
                                    <strong>{w.title}</strong> fino a {formatDateTime(w.end_at)}
                                </li>
                            ))}
                        </ul>
                    </div>
                </div>
            )}

            <div className="rounded-md border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[60px]">ID</TableHead>
                            <TableHead>Titolo</TableHead>
                            <TableHead>Dal</TableHead>
                            <TableHead>Al</TableHead>
                            <TableHead>Stato</TableHead>
                            <TableHead className="text-right">Azioni</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {isLoading ? (
                            <TableRow><TableCell colSpan={6} className="h-20 text-center">Caricamento…</TableCell></TableRow>
                        ) : windows.length === 0 ? (
                            <TableRow><TableCell colSpan={6} className="h-20 text-center">Nessuna finestra di manutenzione.</TableCell></TableRow>
                        ) : windows.map((w) => {
                            const future = new Date(w.start_at) > new Date();
                            return (
                                <TableRow key={w.id}>
                                    <TableCell className="font-medium">{w.id}</TableCell>
                                    <TableCell className="font-semibold">
                                        {w.title}
                                        {w.reason && <div className="text-xs text-muted-foreground font-normal mt-0.5">{w.reason}</div>}
                                    </TableCell>
                                    <TableCell className="text-xs font-mono">{formatDateTime(w.start_at)}</TableCell>
                                    <TableCell className="text-xs font-mono">{formatDateTime(w.end_at)}</TableCell>
                                    <TableCell>
                                        {w.active
                                            ? <Badge className="bg-amber-500/10 text-amber-500 border-none">In corso</Badge>
                                            : future
                                            ? <Badge className="bg-blue-500/10 text-blue-500 border-none">Programmata</Badge>
                                            : <Badge className="bg-slate-500/10 text-slate-400 border-none">Conclusa</Badge>}
                                    </TableCell>
                                    <TableCell className="text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <Button variant="ghost" size="icon" title="Modifica"
                                                className="h-10 sm:h-8 w-10 sm:w-8 text-blue-500 hover:bg-blue-500/10"
                                                onClick={() => openEdit(w)}>
                                                <Pencil size={16} />
                                            </Button>
                                            <Button variant="ghost" size="icon" title="Elimina"
                                                className="h-10 sm:h-8 w-10 sm:w-8 text-red-500 hover:bg-red-500/10"
                                                onClick={() => handleDelete(w)}>
                                                <Trash2 size={16} />
                                            </Button>
                                        </div>
                                    </TableCell>
                                </TableRow>
                            );
                        })}
                    </TableBody>
                </Table>
            </div>

            <Dialog open={editorOpen} onOpenChange={setEditorOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>{editingId !== null ? 'Modifica finestra' : 'Nuova finestra di manutenzione'}</DialogTitle>
                        <DialogDescription>
                            Durante questa finestra le notifiche email/Telegram saranno silenziate.
                            Gli allarmi continuano a scattare e a essere registrati per l'audit.
                        </DialogDescription>
                    </DialogHeader>
                    <div className="grid gap-4 py-2">
                        <div className="grid gap-1">
                            <Label htmlFor="mw-title">Titolo</Label>
                            <Input id="mw-title" value={title} onChange={(e) => setTitle(e.target.value)}
                                placeholder="es. Sostituzione cuscinetto motore linea 2" />
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                            <div className="grid gap-1">
                                <Label htmlFor="mw-start">Dal</Label>
                                <Input id="mw-start" type="datetime-local" value={startAt} onChange={(e) => setStartAt(e.target.value)} />
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="mw-end">Al</Label>
                                <Input id="mw-end" type="datetime-local" value={endAt} onChange={(e) => setEndAt(e.target.value)} />
                            </div>
                        </div>
                        <div className="grid gap-1">
                            <Label htmlFor="mw-reason">Motivazione (opzionale)</Label>
                            <Input id="mw-reason" value={reason} onChange={(e) => setReason(e.target.value)}
                                placeholder="es. Manutenzione programmata trimestrale" />
                        </div>
                    </div>
                    <DialogFooter>
                        <Button onClick={() => saveMutation.mutate()} disabled={!title.trim() || saveMutation.isPending}>
                            {saveMutation.isPending ? 'Salvataggio…' : (editingId !== null ? 'Salva' : 'Crea')}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
};

export default MaintenancePage;
