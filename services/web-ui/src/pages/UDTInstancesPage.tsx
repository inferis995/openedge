import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Trash2, Layers, AlertTriangle, Tag as TagIcon } from 'lucide-react';

import { udtApi, UDTInstance } from '@/api/udt';
import { gatewaysApi } from '@/api/gateways';
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
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog';

/**
 * Instances: a type bound to one gateway at one base address.
 *
 * Creating one generates its tags immediately, and the dialog says how many, so
 * a wrong base address shows up on the first instance rather than after ten.
 */
const UDTInstancesPage = () => {
    const queryClient = useQueryClient();
    const { isAdmin } = useAuthStore();

    const { data: typesData } = useQuery({ queryKey: ['udt-types'], queryFn: udtApi.listTypes });
    const types = typesData?.items ?? [];

    const { data: instData, isLoading } = useQuery({
        queryKey: ['udt-instances'],
        queryFn: () => udtApi.listInstances(),
    });
    const instances: UDTInstance[] = instData?.items ?? [];

    const { data: gateways = [] } = useQuery({
        queryKey: ['gateways'],
        queryFn: () => gatewaysApi.getAll(),
        staleTime: 60_000,
    });

    const [createOpen, setCreateOpen] = useState(false);
    const [typeId, setTypeId] = useState<string>('');
    const [gatewayId, setGatewayId] = useState<string>('');
    const [name, setName] = useState('');
    const [baseAddress, setBaseAddress] = useState('');
    const [toDelete, setToDelete] = useState<UDTInstance | null>(null);

    const createMutation = useMutation({
        mutationFn: () =>
            udtApi.createInstance({
                type_id: Number(typeId),
                gateway_id: Number(gatewayId),
                name,
                base_address: baseAddress,
            }),
        onSuccess: (res) => {
            showApiSuccess(`Istanza creata — ${res.tags_created} tag generati`);
            setCreateOpen(false);
            setName('');
            setBaseAddress('');
            queryClient.invalidateQueries({ queryKey: ['udt-instances'] });
            queryClient.invalidateQueries({ queryKey: ['udt-types'] });
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
        onError: (e) => showApiError(e, 'Creazione dell\'istanza fallita'),
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => udtApi.deleteInstance(id),
        onSuccess: (res) => {
            showApiSuccess(`Istanza eliminata — ${res.tags_deleted} tag rimossi`);
            setToDelete(null);
            queryClient.invalidateQueries({ queryKey: ['udt-instances'] });
            queryClient.invalidateQueries({ queryKey: ['udt-types'] });
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
        onError: (e) => showApiError(e, 'Eliminazione fallita'),
    });

    return (
        <div className="p-6 space-y-6">
            <div className="flex items-start justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-semibold flex items-center gap-2">
                        <Layers size={22} /> Istanze
                    </h1>
                    <p className="text-sm text-muted-foreground mt-1 max-w-2xl">
                        Un'istanza è un tipo applicato a un gateway a un indirizzo base.
                        I tag vengono generati subito: indirizzo base + suffisso del membro.
                    </p>
                </div>
                {isAdmin() && (
                    <Button className="gap-1 shrink-0" disabled={types.length === 0}
                        onClick={() => setCreateOpen(true)}>
                        <Plus size={16} /> Nuova istanza
                    </Button>
                )}
            </div>

            <Table>
                <TableHeader>
                    <TableRow>
                        <TableHead>Nome</TableHead>
                        <TableHead>Tipo</TableHead>
                        <TableHead>Indirizzo base</TableHead>
                        <TableHead className="text-right">Tag</TableHead>
                        <TableHead className="w-16" />
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {isLoading && (
                        <TableRow>
                            <TableCell colSpan={5} className="text-muted-foreground">Caricamento…</TableCell>
                        </TableRow>
                    )}
                    {!isLoading && instances.length === 0 && (
                        <TableRow>
                            <TableCell colSpan={5} className="text-muted-foreground py-8 text-center">
                                {types.length === 0
                                    ? 'Definisci prima un tipo, poi potrai istanziarlo.'
                                    : 'Nessuna istanza. Creane una per generare i tag di un\'apparecchiatura.'}
                            </TableCell>
                        </TableRow>
                    )}
                    {instances.map((in_) => (
                        <TableRow key={in_.id}>
                            <TableCell className="font-medium">{in_.name}</TableCell>
                            <TableCell className="text-muted-foreground">{in_.type_name}</TableCell>
                            <TableCell><code className="text-xs">{in_.base_address || '—'}</code></TableCell>
                            <TableCell className="text-right">
                                <Badge variant="outline" className="gap-1">
                                    <TagIcon size={12} /> {in_.tag_count ?? 0}
                                </Badge>
                            </TableCell>
                            <TableCell>
                                {isAdmin() && (
                                    <div className="flex justify-end">
                                        <Button variant="ghost" size="icon" onClick={() => setToDelete(in_)}>
                                            <Trash2 size={15} className="text-destructive" />
                                        </Button>
                                    </div>
                                )}
                            </TableCell>
                        </TableRow>
                    ))}
                </TableBody>
            </Table>

            {/* Create */}
            <Dialog open={createOpen} onOpenChange={setCreateOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Nuova istanza</DialogTitle>
                        <DialogDescription>
                            I tag vengono generati subito. Verifica il primo sul PLC prima
                            di crearne altri: un indirizzo base sbagliato si moltiplica in
                            silenzio.
                        </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-3 py-2">
                        <div>
                            <Label>Tipo</Label>
                            <Select value={typeId} onValueChange={setTypeId}>
                                <SelectTrigger><SelectValue placeholder="Scegli un tipo" /></SelectTrigger>
                                <SelectContent>
                                    {types.map((t) => (
                                        <SelectItem key={t.id} value={String(t.id)}>{t.name}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                        <div>
                            <Label>Gateway</Label>
                            <Select value={gatewayId} onValueChange={setGatewayId}>
                                <SelectTrigger><SelectValue placeholder="Scegli un gateway" /></SelectTrigger>
                                <SelectContent>
                                    {gateways.map((g) => (
                                        <SelectItem key={g.id} value={String(g.id)}>
                                            {g.name} ({g.driver_type})
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                        <div>
                            <Label>Nome</Label>
                            <Input value={name} placeholder="Pompa01"
                                onChange={(e) => setName(e.target.value)} />
                            <p className="text-xs text-muted-foreground mt-1">
                                Prefissa ogni tag generato: <code>Pompa01_Speed</code>. Così un
                                allarme dice da quale macchina arriva.
                            </p>
                        </div>
                        <div>
                            <Label>Indirizzo base</Label>
                            <Input value={baseAddress} placeholder="40001"
                                onChange={(e) => setBaseAddress(e.target.value)} />
                            <p className="text-xs text-muted-foreground mt-1">
                                I suffissi dei membri vengono accodati a questo:{' '}
                                <code>40001</code> + <code>+2</code> → <code>40001+2</code>.
                            </p>
                        </div>
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setCreateOpen(false)}>Annulla</Button>
                        <Button disabled={!typeId || !gatewayId || !name || createMutation.isPending}
                            onClick={() => createMutation.mutate()}>
                            Crea e genera i tag
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
                            Vengono eliminati {toDelete?.tag_count ?? 0} tag e tutto quello che
                            lo storico ha registrato per loro. Non è recuperabile se non da un
                            backup.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setToDelete(null)}>Annulla</Button>
                        <Button variant="destructive" disabled={deleteMutation.isPending}
                            onClick={() => toDelete && deleteMutation.mutate(toDelete.id)}>
                            Elimina
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
};

export default UDTInstancesPage;
