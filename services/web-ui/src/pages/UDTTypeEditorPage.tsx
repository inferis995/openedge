import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
    Plus, Trash2, Save, ArrowLeft, AlertTriangle, Loader2, Bell, Ruler,
} from 'lucide-react';

import {
    udtApi, UDTMember, UDTAlarm, DataLossRefusal, ReconcileResult, emptyMember,
} from '@/api/udt';
import { useAuthStore } from '@/stores/useAuthStore';
import { showApiError, showApiSuccess } from '@/lib/api-error-handler';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog';

const DATA_TYPES = ['BOOL', 'INT', 'DINT', 'REAL', 'STRING'] as const;

/**
 * The type editor.
 *
 * Members are edited as a whole and saved in one call, because a type is a
 * shape: applying it a member at a time would make the instances pass through
 * states nobody asked for — a motor briefly without its fault bit is a motor
 * whose alarm briefly cannot fire.
 *
 * The dialog at the bottom of this file is the reason the screen exists in this
 * form. Removing a member deletes that tag on every instance, and tag_history
 * cascades from tags, so it also deletes everything ever recorded for them. The
 * API refuses without an explicit confirmation; this surfaces the numbers it
 * quotes rather than turning them into a generic "are you sure".
 */
const UDTTypeEditorPage = () => {
    const { id } = useParams<{ id: string }>();
    const typeId = Number(id);
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { isAdmin } = useAuthStore();

    const { data: type, isLoading } = useQuery({
        queryKey: ['udt-type', typeId],
        queryFn: () => udtApi.getType(typeId),
        enabled: !Number.isNaN(typeId),
    });

    const [name, setName] = useState('');
    const [description, setDescription] = useState('');
    const [members, setMembers] = useState<UDTMember[]>([]);
    const [refusal, setRefusal] = useState<DataLossRefusal | null>(null);
    const [lastResult, setLastResult] = useState<ReconcileResult | null>(null);

    useEffect(() => {
        if (!type) return;
        setName(type.name);
        setDescription(type.description);
        setMembers(type.members.map((m) => ({ ...m, alarms: m.alarms ?? [] })));
    }, [type]);

    const patchMember = (i: number, patch: Partial<UDTMember>) =>
        setMembers((prev) => prev.map((m, k) => (k === i ? { ...m, ...patch } : m)));

    const patchAlarm = (mi: number, ai: number, patch: Partial<UDTAlarm>) =>
        setMembers((prev) =>
            prev.map((m, k) =>
                k === mi
                    ? { ...m, alarms: m.alarms.map((a, j) => (j === ai ? { ...a, ...patch } : a)) }
                    : m,
            ),
        );

    const save = useMutation({
        mutationFn: (confirmDataLoss: boolean) =>
            udtApi.updateType(typeId, {
                name,
                description,
                members: members.map((m, i) => ({ ...m, sort_order: i })),
                confirm_data_loss: confirmDataLoss,
            }),
        onSuccess: (res) => {
            setRefusal(null);
            setLastResult(res);
            showApiSuccess('Tipo salvato');
            queryClient.invalidateQueries({ queryKey: ['udt-type', typeId] });
            queryClient.invalidateQueries({ queryKey: ['udt-types'] });
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
        onError: (e: unknown) => {
            const err = e as { response?: { status?: number; data?: DataLossRefusal } };
            // 409 with an impact is not a failure to report and forget: it is
            // the API asking a question, and the answer belongs to the operator.
            if (err?.response?.status === 409 && err.response.data?.impact) {
                setRefusal(err.response.data);
                return;
            }
            showApiError(e, 'Salvataggio fallito');
        },
    });

    if (isLoading) {
        return (
            <div className="p-8 flex items-center gap-2 text-muted-foreground">
                <Loader2 className="animate-spin" size={18} /> Caricamento…
            </div>
        );
    }
    if (!type) {
        return <div className="p-8 text-muted-foreground">Tipo non trovato.</div>;
    }

    return (
        <div className="p-6 space-y-6 max-w-5xl">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between gap-4">
                <div className="flex items-start gap-3">
                    <Button variant="ghost" size="icon" onClick={() => navigate('/udt/types')}>
                        <ArrowLeft size={18} />
                    </Button>
                    <div>
                        <h1 className="text-2xl font-semibold">{type.name}</h1>
                        <p className="text-sm text-muted-foreground mt-1">
                            {type.instance_count === 0
                                ? 'Nessuna istanza: le modifiche non toccano ancora nulla.'
                                : `${type.instance_count} istanze. Ogni modifica qui le raggiunge tutte.`}
                        </p>
                    </div>
                </div>
                {isAdmin() && (
                    <Button className="gap-1 shrink-0" disabled={save.isPending}
                        onClick={() => save.mutate(false)}>
                        {save.isPending ? <Loader2 size={16} className="animate-spin" /> : <Save size={16} />}
                        Salva e propaga
                    </Button>
                )}
            </div>

            {lastResult && (
                <div className="text-sm rounded border border-emerald-500/30 bg-emerald-500/10 px-3 py-2">
                    Riconciliazione: {lastResult.reconciled.tags_created} tag creati,{' '}
                    {lastResult.reconciled.tags_updated} aggiornati,{' '}
                    {lastResult.reconciled.tags_deleted} eliminati.
                </div>
            )}

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div>
                    <Label>Nome</Label>
                    <Input value={name} onChange={(e) => setName(e.target.value)} />
                </div>
                <div>
                    <Label>Descrizione</Label>
                    <Input value={description} onChange={(e) => setDescription(e.target.value)} />
                </div>
            </div>

            <div className="space-y-3">
                <div className="flex items-center justify-between">
                    <h2 className="text-lg font-medium">Membri</h2>
                    {isAdmin() && (
                        <Button variant="outline" size="sm" className="gap-1"
                            onClick={() => setMembers((p) => [...p, emptyMember()])}>
                            <Plus size={15} /> Aggiungi membro
                        </Button>
                    )}
                </div>
                <p className="text-xs text-muted-foreground -mt-1">
                    Il suffisso viene <strong>accodato</strong> all'indirizzo base
                    dell'istanza: base <code>40001</code> + suffisso <code>+2</code> →{' '}
                    <code>40001+2</code>. Su S7: <code>DB10</code> + <code>.DBX0.1</code>.
                </p>

                {members.map((m, i) => (
                    <div key={i} className="border rounded-lg p-4 space-y-4">
                        <div className="grid grid-cols-12 gap-3 items-end">
                            <div className="col-span-3">
                                <Label>Nome</Label>
                                <Input value={m.name} placeholder="Speed"
                                    onChange={(e) => patchMember(i, { name: e.target.value })} />
                            </div>
                            <div className="col-span-3">
                                <Label>Suffisso indirizzo</Label>
                                <Input value={m.address_suffix} placeholder="+2"
                                    onChange={(e) => patchMember(i, { address_suffix: e.target.value })} />
                            </div>
                            <div className="col-span-2">
                                <Label>Tipo dato</Label>
                                <Select value={m.data_type}
                                    onValueChange={(v) => patchMember(i, { data_type: v as UDTMember['data_type'] })}>
                                    <SelectTrigger><SelectValue /></SelectTrigger>
                                    <SelectContent>
                                        {DATA_TYPES.map((d) => (
                                            <SelectItem key={d} value={d}>{d}</SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>
                            <div className="col-span-2 flex items-center gap-2 pb-2">
                                <Switch checked={m.historize}
                                    onCheckedChange={(v) => patchMember(i, { historize: v })} />
                                <span className="text-sm">Storicizza</span>
                            </div>
                            <div className="col-span-2 flex justify-end pb-1">
                                {isAdmin() && (
                                    <Button variant="ghost" size="icon"
                                        onClick={() => setMembers((p) => p.filter((_, k) => k !== i))}>
                                        <Trash2 size={15} className="text-destructive" />
                                    </Button>
                                )}
                            </div>
                        </div>

                        {/* Scaling */}
                        <div className="border-t pt-3">
                            <div className="flex items-center gap-2 mb-2">
                                <Ruler size={14} className="text-muted-foreground" />
                                <Switch checked={m.scaling_enabled}
                                    onCheckedChange={(v) => patchMember(i, { scaling_enabled: v })} />
                                <span className="text-sm">Scalatura in unità ingegneristiche</span>
                            </div>
                            {m.scaling_enabled && (
                                <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
                                    <div>
                                        <Label className="text-xs">Grezzo min</Label>
                                        <Input type="number" value={m.scaling_raw_min}
                                            onChange={(e) => patchMember(i, { scaling_raw_min: Number(e.target.value) })} />
                                    </div>
                                    <div>
                                        <Label className="text-xs">Grezzo max</Label>
                                        <Input type="number" value={m.scaling_raw_max}
                                            onChange={(e) => patchMember(i, { scaling_raw_max: Number(e.target.value) })} />
                                    </div>
                                    <div>
                                        <Label className="text-xs">EU min</Label>
                                        <Input type="number" value={m.scaling_eu_min}
                                            onChange={(e) => patchMember(i, { scaling_eu_min: Number(e.target.value) })} />
                                    </div>
                                    <div>
                                        <Label className="text-xs">EU max</Label>
                                        <Input type="number" value={m.scaling_eu_max}
                                            onChange={(e) => patchMember(i, { scaling_eu_max: Number(e.target.value) })} />
                                    </div>
                                    <div>
                                        <Label className="text-xs">Unità</Label>
                                        <Input value={m.eu_unit} placeholder="rpm"
                                            onChange={(e) => patchMember(i, { eu_unit: e.target.value })} />
                                    </div>
                                </div>
                            )}
                        </div>

                        {/* Alarms */}
                        <div className="border-t pt-3">
                            <div className="flex items-center justify-between mb-2">
                                <div className="flex items-center gap-2">
                                    <Bell size={14} className="text-muted-foreground" />
                                    <span className="text-sm">Allarmi</span>
                                    {m.alarms.length > 0 && (
                                        <Badge variant="outline">{m.alarms.length}</Badge>
                                    )}
                                </div>
                                {isAdmin() && (
                                    <Button variant="ghost" size="sm" className="gap-1"
                                        onClick={() => patchMember(i, {
                                            alarms: [...m.alarms, {
                                                alarm_type: 'high', threshold: 0, deadband: 0,
                                                delay_seconds: 0, severity: 'warning',
                                                message: '', enabled: true,
                                            }],
                                        })}>
                                        <Plus size={14} /> Aggiungi
                                    </Button>
                                )}
                            </div>
                            {m.alarms.map((a, ai) => (
                                <div key={ai} className="grid grid-cols-12 gap-2 items-end mb-2">
                                    <div className="col-span-2">
                                        <Label className="text-xs">Condizione</Label>
                                        <Select value={a.alarm_type}
                                            onValueChange={(v) => patchAlarm(i, ai, { alarm_type: v as 'high' | 'low' })}>
                                            <SelectTrigger><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="high">Alta</SelectItem>
                                                <SelectItem value="low">Bassa</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div className="col-span-2">
                                        <Label className="text-xs">Soglia</Label>
                                        <Input type="number" value={a.threshold ?? 0}
                                            onChange={(e) => patchAlarm(i, ai, { threshold: Number(e.target.value) })} />
                                    </div>
                                    <div className="col-span-2">
                                        <Label className="text-xs">Gravità</Label>
                                        <Select value={a.severity}
                                            onValueChange={(v) => patchAlarm(i, ai, { severity: v as UDTAlarm['severity'] })}>
                                            <SelectTrigger><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="info">Info</SelectItem>
                                                <SelectItem value="warning">Warning</SelectItem>
                                                <SelectItem value="critical">Critical</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div className="col-span-5">
                                        <Label className="text-xs">Messaggio</Label>
                                        <Input value={a.message} placeholder="sovravelocità"
                                            onChange={(e) => patchAlarm(i, ai, { message: e.target.value })} />
                                    </div>
                                    <div className="col-span-1 flex justify-end pb-1">
                                        <Button variant="ghost" size="icon"
                                            onClick={() => patchMember(i, {
                                                alarms: m.alarms.filter((_, j) => j !== ai),
                                            })}>
                                            <Trash2 size={14} className="text-destructive" />
                                        </Button>
                                    </div>
                                </div>
                            ))}
                        </div>
                    </div>
                ))}
            </div>

            {/*
              The data-loss confirmation.

              Deliberately not a generic "are you sure": it repeats the numbers
              the API computed — which members, how many tags, how many recorded
              rows — because "sei sicuro?" is a question everybody answers yes to,
              and "questo cancella 1.243.902 righe" is not.
            */}
            <Dialog open={!!refusal} onOpenChange={(o) => !o && setRefusal(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <AlertTriangle size={18} className="text-destructive" />
                            Questa modifica cancella dati storici
                        </DialogTitle>
                        <DialogDescription asChild>
                            <div className="space-y-3 pt-2">
                                <p>
                                    Stai rimuovendo{' '}
                                    <strong>{refusal?.impact.members.join(', ')}</strong> dal tipo.
                                </p>
                                <div className="rounded border border-destructive/30 bg-destructive/10 p-3 text-sm space-y-1">
                                    <div>
                                        <strong>{refusal?.impact.tags}</strong> tag verranno eliminati
                                        dalle istanze di questo tipo.
                                    </div>
                                    <div>
                                        <strong>
                                            {refusal && refusal.impact.history_rows >= 0
                                                ? refusal.impact.history_rows.toLocaleString('it-IT')
                                                : 'un numero imprecisato di'}
                                        </strong>{' '}
                                        righe registrate nello storico andranno perse con loro.
                                    </div>
                                </div>
                                <p className="text-xs">
                                    Non è recuperabile se non da un backup. Se ti serve solo
                                    smettere di leggere quel valore, disattiva la
                                    storicizzazione del membro invece di rimuoverlo.
                                </p>
                            </div>
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setRefusal(null)}>
                            Annulla
                        </Button>
                        <Button variant="destructive" disabled={save.isPending}
                            onClick={() => save.mutate(true)}>
                            Elimina comunque
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
};

export default UDTTypeEditorPage;
