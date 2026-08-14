import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Clock, Pencil, Plus, Trash2, UserPlus, Users } from 'lucide-react';

import {
    shiftsApi, Shift, ShiftAssignment, CreateShiftDto, WEEKDAY_LABELS,
} from '@/api/shifts';
import { usersApi } from '@/api/users';
import { showApiError, showApiSuccess } from '@/lib/api-error-handler';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog';

// Mostra "Lun-Ven" se i weekdays sono 1..5, "Sab-Dom" se 0+6, altrimenti
// la lista esplicita. Più leggibile dei badge singoli quando i pattern
// sono quelli classici.
const formatWeekdays = (weekdays: number[]): string => {
    if (weekdays.length === 0) return '—';
    const sorted = [...weekdays].sort((a, b) => a - b);
    const isWeekdays = sorted.length === 5 && sorted.every((d, i) => d === i + 1);
    if (isWeekdays) return 'Lun-Ven';
    const isWeekend = sorted.length === 2 && sorted[0] === 0 && sorted[1] === 6;
    if (isWeekend) return 'Sab-Dom';
    return sorted.map((d) => WEEKDAY_LABELS[d]).join(' ');
};

const ShiftsPage = () => {
    const qc = useQueryClient();

    const { data: shifts = [], isLoading } = useQuery({
        queryKey: ['shifts'],
        queryFn: shiftsApi.list,
    });

    // Editor turno (create + edit con lo stesso form).
    const [editorOpen, setEditorOpen] = useState(false);
    const [editingId, setEditingId] = useState<number | null>(null);
    const [name, setName] = useState('');
    const [startTime, setStartTime] = useState('06:00');
    const [endTime, setEndTime] = useState('14:00');
    const [weekdays, setWeekdays] = useState<number[]>([1, 2, 3, 4, 5]);
    const [active, setActive] = useState(true);

    const openCreate = () => {
        setEditingId(null);
        setName('');
        setStartTime('06:00');
        setEndTime('14:00');
        setWeekdays([1, 2, 3, 4, 5]);
        setActive(true);
        setEditorOpen(true);
    };
    const openEdit = (s: Shift) => {
        setEditingId(s.id);
        setName(s.name);
        setStartTime(s.start_time);
        setEndTime(s.end_time);
        setWeekdays(s.weekdays);
        setActive(s.active);
        setEditorOpen(true);
    };
    const toggleWeekday = (d: number) => {
        setWeekdays((cur) => cur.includes(d) ? cur.filter((x) => x !== d) : [...cur, d].sort((a, b) => a - b));
    };

    const saveMutation = useMutation({
        mutationFn: async () => {
            const data: CreateShiftDto = { name, start_time: startTime, end_time: endTime, weekdays, active };
            if (editingId !== null) await shiftsApi.update(editingId, data);
            else await shiftsApi.create(data);
        },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['shifts'] });
            showApiSuccess('Turno salvato');
            setEditorOpen(false);
        },
        onError: (e) => showApiError(e, 'Salvataggio fallito'),
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => shiftsApi.delete(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['shifts'] });
            showApiSuccess('Turno eliminato');
        },
        onError: (e) => showApiError(e, 'Eliminazione fallita'),
    });

    const handleDelete = (s: Shift) => {
        if (confirm(`Eliminare il turno "${s.name}"? Le assegnazioni operatori verranno rimosse insieme.`)) {
            deleteMutation.mutate(s.id);
        }
    };

    // Assegnamenti operatori — apre un secondo dialog dedicato.
    const [assignShift, setAssignShift] = useState<Shift | null>(null);

    return (
        <div className="space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight flex items-center gap-2">
                        <Clock size={22} /> Turni & Operatori
                    </h2>
                    <p className="text-muted-foreground">
                        Definisci gli orari di lavoro e assegna gli operatori. Il sistema riconosce
                        automaticamente il turno corrente in base all'ora del server.
                    </p>
                </div>
                <Button onClick={openCreate} className="gap-2">
                    <Plus size={16} /> Nuovo turno
                </Button>
            </div>

            <div className="rounded-md border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[60px]">ID</TableHead>
                            <TableHead>Nome</TableHead>
                            <TableHead>Orario</TableHead>
                            <TableHead>Giorni</TableHead>
                            <TableHead>Stato</TableHead>
                            <TableHead className="text-right">Azioni</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {isLoading ? (
                            <TableRow><TableCell colSpan={6} className="h-20 text-center">Caricamento…</TableCell></TableRow>
                        ) : shifts.length === 0 ? (
                            <TableRow><TableCell colSpan={6} className="h-20 text-center">Nessun turno definito.</TableCell></TableRow>
                        ) : shifts.map((s) => (
                            <TableRow key={s.id}>
                                <TableCell className="font-medium">{s.id}</TableCell>
                                <TableCell className="font-semibold">{s.name}</TableCell>
                                <TableCell className="font-mono text-xs">
                                    {s.start_time} → {s.end_time}
                                    {s.wraps && <Badge className="ml-2 bg-purple-500/10 text-purple-500 border-none text-xs">notte</Badge>}
                                </TableCell>
                                <TableCell className="text-xs">{formatWeekdays(s.weekdays)}</TableCell>
                                <TableCell>
                                    {s.active
                                        ? <Badge className="bg-emerald-500/10 text-emerald-500 border-none">Attivo</Badge>
                                        : <Badge className="bg-slate-500/10 text-slate-400 border-none">Disattivato</Badge>}
                                </TableCell>
                                <TableCell className="text-right">
                                    <div className="flex items-center justify-end gap-2">
                                        <Button variant="ghost" size="icon" title="Operatori"
                                            className="h-10 sm:h-8 w-10 sm:w-8 text-cyan-500 hover:bg-cyan-500/10"
                                            onClick={() => setAssignShift(s)}>
                                            <Users size={16} />
                                        </Button>
                                        <Button variant="ghost" size="icon" title="Modifica"
                                            className="h-10 sm:h-8 w-10 sm:w-8 text-blue-500 hover:bg-blue-500/10"
                                            onClick={() => openEdit(s)}>
                                            <Pencil size={16} />
                                        </Button>
                                        <Button variant="ghost" size="icon" title="Elimina"
                                            className="h-10 sm:h-8 w-10 sm:w-8 text-red-500 hover:bg-red-500/10"
                                            onClick={() => handleDelete(s)}>
                                            <Trash2 size={16} />
                                        </Button>
                                    </div>
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>

            {/* Editor turno */}
            <Dialog open={editorOpen} onOpenChange={setEditorOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>{editingId !== null ? 'Modifica turno' : 'Nuovo turno'}</DialogTitle>
                        <DialogDescription>
                            Inserisci nome, orario di inizio/fine, giorni della settimana. Per il
                            turno notte usa un'ora fine minore di quella di inizio (es. 22:00 → 06:00).
                        </DialogDescription>
                    </DialogHeader>
                    <div className="grid gap-4 py-2">
                        <div className="grid gap-1">
                            <Label htmlFor="sh-name">Nome</Label>
                            <Input id="sh-name" value={name} onChange={(e) => setName(e.target.value)}
                                placeholder="es. Mattina" />
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                            <div className="grid gap-1">
                                <Label htmlFor="sh-start">Inizio</Label>
                                <Input id="sh-start" type="time" value={startTime} onChange={(e) => setStartTime(e.target.value)} />
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="sh-end">Fine</Label>
                                <Input id="sh-end" type="time" value={endTime} onChange={(e) => setEndTime(e.target.value)} />
                            </div>
                        </div>
                        <div className="grid gap-2">
                            <Label>Giorni della settimana</Label>
                            <div className="flex gap-2 flex-wrap">
                                {WEEKDAY_LABELS.map((label, idx) => {
                                    const sel = weekdays.includes(idx);
                                    return (
                                        <button key={idx} type="button" onClick={() => toggleWeekday(idx)}
                                            className={`px-3 py-1.5 rounded-md text-xs font-medium border transition-colors ${
                                                sel
                                                    ? 'bg-primary text-primary-foreground border-primary'
                                                    : 'bg-transparent border-input hover:bg-muted'
                                            }`}>
                                            {label}
                                        </button>
                                    );
                                })}
                            </div>
                        </div>
                        <div className="flex items-center gap-3">
                            <Switch checked={active} onCheckedChange={setActive} />
                            <Label>Turno attivo</Label>
                        </div>
                    </div>
                    <DialogFooter>
                        <Button onClick={() => saveMutation.mutate()} disabled={!name.trim() || saveMutation.isPending}>
                            {saveMutation.isPending ? 'Salvataggio…' : (editingId !== null ? 'Salva' : 'Crea')}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <AssignmentsDialog
                shift={assignShift}
                onClose={() => setAssignShift(null)}
            />
        </div>
    );
};

// ─────────────────────────────────────────────────────────────────────────────
// Dialog separato per gestione operatori assegnati a un turno.
// ─────────────────────────────────────────────────────────────────────────────
const AssignmentsDialog = ({ shift, onClose }: { shift: Shift | null; onClose: () => void }) => {
    const qc = useQueryClient();
    const { data: assignments = [], refetch } = useQuery({
        queryKey: ['shift-assignments', shift?.id],
        queryFn: () => shift ? shiftsApi.listAssignments(shift.id) : Promise.resolve([]),
        enabled: !!shift,
    });
    const { data: users = [] } = useQuery({
        queryKey: ['users'],
        queryFn: usersApi.list,
    });

    const [selectedUserID, setSelectedUserID] = useState<string>('');
    const [validFrom, setValidFrom] = useState<string>(new Date().toISOString().slice(0, 10));
    const [validTo, setValidTo] = useState<string>('');

    const addMutation = useMutation({
        mutationFn: async () => {
            if (!shift) return;
            await shiftsApi.createAssignment(shift.id, {
                user_id: parseInt(selectedUserID, 10),
                valid_from: validFrom,
                valid_to: validTo || null,
            });
        },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['shift-assignments', shift?.id] });
            refetch();
            setSelectedUserID('');
            setValidTo('');
            showApiSuccess('Operatore assegnato');
        },
        onError: (e) => showApiError(e, 'Assegnazione fallita'),
    });

    const removeMutation = useMutation({
        mutationFn: (aid: number) => shiftsApi.deleteAssignment(aid),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['shift-assignments', shift?.id] });
            refetch();
            showApiSuccess('Assegnazione rimossa');
        },
        onError: (e) => showApiError(e, 'Rimozione fallita'),
    });

    if (!shift) return null;

    return (
        <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
            <DialogContent className="max-w-2xl">
                <DialogHeader>
                    <DialogTitle>Operatori — {shift.name}</DialogTitle>
                    <DialogDescription>
                        Lista degli operatori designati per questo turno. La dashboard mostra in
                        tempo reale chi è in servizio.
                    </DialogDescription>
                </DialogHeader>

                {/* Form nuovo assegnamento */}
                <div className="rounded-md border bg-muted/30 p-3 space-y-3">
                    <div className="flex items-center gap-2 text-sm font-semibold">
                        <UserPlus size={14} /> Aggiungi operatore
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2 items-end">
                        <div className="grid gap-1">
                            <Label htmlFor="asg-user" className="text-xs">Utente</Label>
                            <select
                                id="asg-user"
                                value={selectedUserID}
                                onChange={(e) => setSelectedUserID(e.target.value)}
                                className="flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm"
                            >
                                <option value="">— seleziona —</option>
                                {users.map((u) => (
                                    <option key={u.id} value={u.id}>{u.username} {u.full_name ? `(${u.full_name})` : ''}</option>
                                ))}
                            </select>
                        </div>
                        <div className="grid gap-1">
                            <Label htmlFor="asg-from" className="text-xs">Dal</Label>
                            <Input id="asg-from" type="date" value={validFrom} onChange={(e) => setValidFrom(e.target.value)} className="h-9" />
                        </div>
                        <div className="grid gap-1">
                            <Label htmlFor="asg-to" className="text-xs">Al (opzionale)</Label>
                            <Input id="asg-to" type="date" value={validTo} onChange={(e) => setValidTo(e.target.value)} className="h-9" />
                        </div>
                    </div>
                    <Button
                        size="sm"
                        onClick={() => addMutation.mutate()}
                        disabled={!selectedUserID || addMutation.isPending}>
                        Assegna
                    </Button>
                </div>

                {/* Lista assegnamenti */}
                <div className="rounded-md border max-h-72 overflow-auto">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Operatore</TableHead>
                                <TableHead>Dal</TableHead>
                                <TableHead>Al</TableHead>
                                <TableHead className="text-right">Azioni</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {assignments.length === 0 ? (
                                <TableRow><TableCell colSpan={4} className="h-16 text-center text-muted-foreground">Nessun operatore assegnato.</TableCell></TableRow>
                            ) : assignments.map((a: ShiftAssignment) => (
                                <TableRow key={a.id}>
                                    <TableCell className="text-sm">
                                        <div className="font-medium">{a.username}</div>
                                        {a.full_name && <div className="text-xs text-muted-foreground">{a.full_name}</div>}
                                    </TableCell>
                                    <TableCell className="font-mono text-xs">{a.valid_from?.slice(0, 10)}</TableCell>
                                    <TableCell className="font-mono text-xs">{a.valid_to ? a.valid_to.slice(0, 10) : '—'}</TableCell>
                                    <TableCell className="text-right">
                                        <Button variant="ghost" size="icon"
                                            className="h-10 sm:h-8 w-10 sm:w-8 text-red-500"
                                            onClick={() => removeMutation.mutate(a.id)}>
                                            <Trash2 size={14} />
                                        </Button>
                                    </TableCell>
                                </TableRow>
                            ))}
                        </TableBody>
                    </Table>
                </div>
            </DialogContent>
        </Dialog>
    );
};

export default ShiftsPage;
