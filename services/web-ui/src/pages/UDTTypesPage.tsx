import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Pencil, Trash2, Boxes, AlertTriangle, Layers } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

import { udtApi, UDTType } from '@/api/udt';
import { useAuthStore } from '@/stores/useAuthStore';
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

/**
 * The list of user-defined types.
 *
 * Creating a type here only declares a name; the members — which are what make
 * it useful — are edited on the type's own page. Splitting it that way keeps
 * this screen answering one question: which types exist, and how much of the
 * plant depends on each.
 */
const UDTTypesPage = () => {
    const queryClient = useQueryClient();
    const navigate = useNavigate();
    const { isAdmin } = useAuthStore();

    const { data, isLoading } = useQuery({
        queryKey: ['udt-types'],
        queryFn: udtApi.listTypes,
    });
    const types: UDTType[] = data?.items ?? [];

    const [createOpen, setCreateOpen] = useState(false);
    const [name, setName] = useState('');
    const [description, setDescription] = useState('');
    const [toDelete, setToDelete] = useState<UDTType | null>(null);

    const createMutation = useMutation({
        mutationFn: () =>
            udtApi.createType({
                name,
                description,
                // A type needs at least one member, so start it with a plausible
                // one rather than sending an empty shape the API will refuse.
                members: [
                    {
                        name: 'Value',
                        address_suffix: '',
                        data_type: 'REAL',
                        historize: true,
                        historize_deadband: 0,
                        scaling_enabled: false,
                        scaling_raw_min: 0,
                        scaling_raw_max: 27648,
                        scaling_eu_min: 0,
                        scaling_eu_max: 100,
                        scaling_clamp: true,
                        eu_unit: '',
                        eu_decimals: 2,
                        invert: false,
                        sort_order: 0,
                        alarms: [],
                    },
                ],
            }),
        onSuccess: (res) => {
            showApiSuccess('Tipo creato');
            setCreateOpen(false);
            setName('');
            setDescription('');
            queryClient.invalidateQueries({ queryKey: ['udt-types'] });
            // Straight into the editor: a type with one placeholder member is
            // not finished, and leaving the user on a list implies it is.
            navigate(`/udt/types/${res.id}`);
        },
        onError: (e) => showApiError(e, 'Creazione del tipo fallita'),
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => udtApi.deleteType(id),
        onSuccess: () => {
            showApiSuccess('Tipo eliminato');
            setToDelete(null);
            queryClient.invalidateQueries({ queryKey: ['udt-types'] });
        },
        onError: (e) => showApiError(e, 'Eliminazione fallita'),
    });

    return (
        <div className="p-6 space-y-6">
            <div className="flex items-start justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-semibold flex items-center gap-2">
                        <Boxes size={22} /> Tipi (UDT)
                    </h1>
                    <p className="text-sm text-muted-foreground mt-1 max-w-2xl">
                        Un tipo descrive un'apparecchiatura una volta sola. Ogni istanza
                        viene generata da lui e resta legata: modifichi il tipo e tutte le
                        istanze seguono.
                    </p>
                </div>
                {isAdmin() && (
                    <Button onClick={() => setCreateOpen(true)} className="gap-1 shrink-0">
                        <Plus size={16} /> Nuovo tipo
                    </Button>
                )}
            </div>

            <Table>
                <TableHeader>
                    <TableRow>
                        <TableHead>Nome</TableHead>
                        <TableHead>Descrizione</TableHead>
                        <TableHead className="text-right">Istanze</TableHead>
                        <TableHead className="w-24" />
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {isLoading && (
                        <TableRow>
                            <TableCell colSpan={4} className="text-muted-foreground">
                                Caricamento…
                            </TableCell>
                        </TableRow>
                    )}
                    {!isLoading && types.length === 0 && (
                        <TableRow>
                            <TableCell colSpan={4} className="text-muted-foreground py-8 text-center">
                                Nessun tipo definito. Creane uno quando hai più
                                apparecchiature dello stesso genere — dieci pompe, venti
                                valvole — invece di creare i tag a mano una per una.
                            </TableCell>
                        </TableRow>
                    )}
                    {types.map((t) => (
                        <TableRow key={t.id} className="cursor-pointer" onClick={() => navigate(`/udt/types/${t.id}`)}>
                            <TableCell className="font-medium">{t.name}</TableCell>
                            <TableCell className="text-muted-foreground">{t.description}</TableCell>
                            <TableCell className="text-right">
                                <Badge variant="outline" className="gap-1">
                                    <Layers size={12} /> {t.instance_count}
                                </Badge>
                            </TableCell>
                            <TableCell onClick={(e) => e.stopPropagation()}>
                                <div className="flex justify-end gap-1">
                                    <Button variant="ghost" size="icon"
                                        onClick={() => navigate(`/udt/types/${t.id}`)}>
                                        <Pencil size={15} />
                                    </Button>
                                    {isAdmin() && (
                                        <Button variant="ghost" size="icon"
                                            onClick={() => setToDelete(t)}>
                                            <Trash2 size={15} className="text-destructive" />
                                        </Button>
                                    )}
                                </div>
                            </TableCell>
                        </TableRow>
                    ))}
                </TableBody>
            </Table>

            {/* Create */}
            <Dialog open={createOpen} onOpenChange={setCreateOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Nuovo tipo</DialogTitle>
                        <DialogDescription>
                            Dai un nome al tipo; i membri si definiscono subito dopo.
                        </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-3 py-2">
                        <div>
                            <Label htmlFor="udt-name">Nome</Label>
                            <Input id="udt-name" value={name} placeholder="Motore"
                                onChange={(e) => setName(e.target.value)} />
                            <p className="text-xs text-muted-foreground mt-1">
                                Lettere, cifre, trattino e underscore. Il nome finisce
                                nell'alias dei tag generati e quindi in un topic MQTT.
                            </p>
                        </div>
                        <div>
                            <Label htmlFor="udt-desc">Descrizione</Label>
                            <Input id="udt-desc" value={description}
                                placeholder="Motore asincrono con inverter"
                                onChange={(e) => setDescription(e.target.value)} />
                        </div>
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setCreateOpen(false)}>Annulla</Button>
                        <Button disabled={!name || createMutation.isPending}
                            onClick={() => createMutation.mutate()}>
                            Crea e definisci i membri
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Delete */}
            <Dialog open={!!toDelete} onOpenChange={(o) => !o && setToDelete(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <AlertTriangle size={18} className="text-destructive" />
                            Eliminare «{toDelete?.name}»?
                        </DialogTitle>
                        <DialogDescription>
                            {toDelete && toDelete.instance_count > 0
                                ? `Questo tipo ha ${toDelete.instance_count} istanze. L'API rifiuterà
                                   l'eliminazione: elimina prima le istanze, che sono apparecchiature reali.`
                                : 'Il tipo non ha istanze, quindi non viene generato né eliminato alcun tag.'}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setToDelete(null)}>Annulla</Button>
                        <Button variant="destructive"
                            disabled={deleteMutation.isPending}
                            onClick={() => toDelete && deleteMutation.mutate(toDelete.id)}>
                            Elimina
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
};

export default UDTTypesPage;
