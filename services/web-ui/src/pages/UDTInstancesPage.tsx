import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Trash2, Layers, AlertTriangle, Tag as TagIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';

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
    const { t: tr } = useTranslation();
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
            showApiSuccess(tr('udtInstancesPage.created', { count: res.tags_created }));
            setCreateOpen(false);
            setName('');
            setBaseAddress('');
            queryClient.invalidateQueries({ queryKey: ['udt-instances'] });
            queryClient.invalidateQueries({ queryKey: ['udt-types'] });
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
        onError: (e) => showApiError(e, tr('udtInstancesPage.create_failed')),
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => udtApi.deleteInstance(id),
        onSuccess: (res) => {
            showApiSuccess(tr('udtInstancesPage.deleted', { count: res.tags_deleted }));
            setToDelete(null);
            queryClient.invalidateQueries({ queryKey: ['udt-instances'] });
            queryClient.invalidateQueries({ queryKey: ['udt-types'] });
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
        onError: (e) => showApiError(e, tr('udtInstancesPage.delete_failed')),
    });

    return (
        <div className="p-6 space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-semibold flex items-center gap-2">
                        <Layers size={22} /> {tr('udtInstancesPage.title')}
                    </h1>
                    <p className="text-sm text-muted-foreground mt-1 max-w-2xl">
                        {tr('udtInstancesPage.subtitle')}
                    </p>
                </div>
                {isAdmin() && (
                    <Button className="gap-1 shrink-0" disabled={types.length === 0}
                        onClick={() => setCreateOpen(true)}>
                        <Plus size={16} /> {tr('udtInstancesPage.add')}
                    </Button>
                )}
            </div>

            <Table>
                <TableHeader>
                    <TableRow>
                        <TableHead>{tr('common.name')}</TableHead>
                        <TableHead>{tr('udtInstancesPage.type')}</TableHead>
                        <TableHead>{tr('udtInstancesPage.base_address')}</TableHead>
                        <TableHead className="text-right">{tr('udtInstancesPage.tags')}</TableHead>
                        <TableHead className="w-16" />
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {isLoading && (
                        <TableRow>
                            <TableCell colSpan={5} className="text-muted-foreground">{tr('common.loading')}</TableCell>
                        </TableRow>
                    )}
                    {!isLoading && instances.length === 0 && (
                        <TableRow>
                            <TableCell colSpan={5} className="text-muted-foreground py-8 text-center">
                                {types.length === 0
                                    ? tr('udtInstancesPage.empty_no_types')
                                    : tr('udtInstancesPage.empty')}
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
                                        <Button variant="ghost" size="icon" aria-label={tr('common.delete')} onClick={() => setToDelete(in_)}>
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
                        <DialogTitle>{tr('udtInstancesPage.add')}</DialogTitle>
                        <DialogDescription>
                            {tr('udtInstancesPage.create_desc')}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-3 py-2">
                        <div>
                            <Label>{tr('udtInstancesPage.type')}</Label>
                            <Select value={typeId} onValueChange={setTypeId}>
                                <SelectTrigger><SelectValue placeholder={tr('udtInstancesPage.pick_type')} /></SelectTrigger>
                                <SelectContent>
                                    {types.map((t) => (
                                        <SelectItem key={t.id} value={String(t.id)}>{t.name}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                        <div>
                            <Label>{tr('common.gateway')}</Label>
                            <Select value={gatewayId} onValueChange={setGatewayId}>
                                <SelectTrigger><SelectValue placeholder={tr('udtInstancesPage.pick_gateway')} /></SelectTrigger>
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
                            <Label>{tr('common.name')}</Label>
                            <Input value={name} placeholder={tr('udtInstancesPage.name_placeholder')}
                                onChange={(e) => setName(e.target.value)} />
                            <p className="text-xs text-muted-foreground mt-1">
                                {tr('udtInstancesPage.name_help_prefix')} <code>{tr('udtInstancesPage.name_placeholder')}_Speed</code>. {tr('udtInstancesPage.name_help_suffix')}
                            </p>
                        </div>
                        <div>
                            <Label>{tr('udtInstancesPage.base_address')}</Label>
                            <Input value={baseAddress} placeholder="40001"
                                onChange={(e) => setBaseAddress(e.target.value)} />
                            <p className="text-xs text-muted-foreground mt-1">
                                {tr('udtInstancesPage.base_address_help')}{' '}
                                <code>40001</code> + <code>+2</code> → <code>40001+2</code>.
                            </p>
                        </div>
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setCreateOpen(false)}>{tr('common.cancel')}</Button>
                        <Button disabled={!typeId || !gatewayId || !name || createMutation.isPending}
                            onClick={() => createMutation.mutate()}>
                            {tr('udtInstancesPage.create_submit')}
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
                            {tr('udtInstancesPage.delete_title', { name: toDelete?.name })}
                        </DialogTitle>
                        <DialogDescription>
                            {tr('udtInstancesPage.delete_desc', { count: toDelete?.tag_count ?? 0 })}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setToDelete(null)}>{tr('common.cancel')}</Button>
                        <Button variant="destructive" disabled={deleteMutation.isPending}
                            onClick={() => toDelete && deleteMutation.mutate(toDelete.id)}>
                            {tr('common.delete')}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
};

export default UDTInstancesPage;
