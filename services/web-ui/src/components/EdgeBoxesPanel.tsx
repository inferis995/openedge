import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import { AlertTriangle, Check, Pencil, Server, Trash2, X } from 'lucide-react';
import { edgeAgentsApi, EdgeAgent, EdgeAgentScope } from '@/api/edgeAgents';
import { showApiSuccess, showApiError } from '@/lib/api-error-handler';

// Una scatola che non si fa sentire da più di tanto è considerata offline.
// Il suo heartbeat parte ogni 30 secondi.
const OFFLINE_AFTER_MS = 2 * 60 * 1000;

interface Props {
    orgId: number;
}

// Chi interroga quale PLC, e le scatole di questa organizzazione.
//
// La regola è una sola: ogni gateway lo interroga la scatola a cui è
// assegnato, oppure il server. Questo pannello la rende visibile — quanti
// gateway sono del server, quanti di ciascuna scatola, e soprattutto se ce ne
// sono che non interroga nessuno.
export default function EdgeBoxesPanel({ orgId }: Props) {
    const qc = useQueryClient();
    const key = ['edge-agents', orgId];

    const { data, isLoading } = useQuery({
        queryKey: key,
        queryFn: () => edgeAgentsApi.list(orgId),
        refetchInterval: 30_000,
    });

    const [editing, setEditing] = useState<number | null>(null);
    const [draftName, setDraftName] = useState('');

    const update = useMutation({
        mutationFn: (v: { id: number; name?: string; scope?: EdgeAgentScope }) =>
            edgeAgentsApi.update(orgId, v.id, { name: v.name, scope: v.scope }),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: key });
            setEditing(null);
            showApiSuccess('Scatola aggiornata');
        },
        onError: (e) => showApiError(e, 'Aggiornamento non riuscito'),
    });

    const remove = useMutation({
        mutationFn: (id: number) => edgeAgentsApi.remove(orgId, id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: key });
            showApiSuccess('Scatola rimossa', 'La sua chiave è revocata e i suoi gateway tornano al server.');
        },
        onError: (e) => showApiError(e, 'Rimozione non riuscita'),
    });

    if (isLoading || !data) {
        return <p className="text-xs text-muted-foreground">Caricamento scatole…</p>;
    }

    const online = (a: EdgeAgent) =>
        !!a.last_seen_at && Date.now() - new Date(a.last_seen_at).getTime() < OFFLINE_AFTER_MS;

    return (
        <div className="space-y-3">
            {/* Il server: interroga i gateway che nessuna scatola ha. */}
            <div className="flex items-start gap-3 rounded-lg border p-3">
                <Server size={16} className="mt-0.5 text-muted-foreground" />
                <div className="text-sm">
                    <p className="font-medium">Server</p>
                    {data.server_polls ? (
                        <p className="text-xs text-muted-foreground">
                            Interroga direttamente {data.server_gateways} gateway — quelli che non hai
                            assegnato a nessuna scatola.
                        </p>
                    ) : (
                        <p className="text-xs text-muted-foreground">
                            Non interroga PLC (nessun driver-manager attivo sul server, tipico di un
                            server in cloud).
                        </p>
                    )}
                </div>
            </div>

            {data.unpolled_gateways > 0 && (
                <div className="flex items-start gap-2 rounded-lg border border-amber-500/50 bg-amber-500/10 p-3 text-xs">
                    <AlertTriangle size={14} className="mt-0.5 text-amber-600" />
                    <span>
                        <strong>{data.unpolled_gateways} gateway non li interroga nessuno.</strong>{' '}
                        Assegnali a una scatola dalla pagina Gateway, oppure imposta una scatola su
                        "tutti i non assegnati".
                    </span>
                </div>
            )}

            {data.agents.length === 0 ? (
                <p className="text-xs text-muted-foreground">
                    Nessuna scatola installata. Non serve, se il server raggiunge tutti i PLC: una
                    scatola va messa solo vicino ai PLC che il server non raggiunge.
                </p>
            ) : (
                <div className="rounded-lg border overflow-x-auto">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Scatola</TableHead>
                                <TableHead>Stato</TableHead>
                                <TableHead>Interroga</TableHead>
                                <TableHead className="text-right">Gateway</TableHead>
                                <TableHead />
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {data.agents.map((a) => (
                                <TableRow key={a.id}>
                                    <TableCell className="min-w-[10rem]">
                                        {editing === a.id ? (
                                            <div className="flex items-center gap-1">
                                                <Input
                                                    value={draftName}
                                                    onChange={(e) => setDraftName(e.target.value)}
                                                    className="h-8"
                                                    aria-label="Nome della scatola"
                                                />
                                                <Button size="icon" variant="ghost" aria-label="Salva nome"
                                                    onClick={() => update.mutate({ id: a.id, name: draftName })}>
                                                    <Check size={14} />
                                                </Button>
                                                <Button size="icon" variant="ghost" aria-label="Annulla"
                                                    onClick={() => setEditing(null)}>
                                                    <X size={14} />
                                                </Button>
                                            </div>
                                        ) : (
                                            <div className="flex items-center gap-1">
                                                <span className="font-medium">{a.name}</span>
                                                <Button size="icon" variant="ghost" aria-label="Rinomina"
                                                    onClick={() => { setEditing(a.id); setDraftName(a.name); }}>
                                                    <Pencil size={12} />
                                                </Button>
                                            </div>
                                        )}
                                        {a.agent_version && (
                                            <span className="text-[11px] text-muted-foreground">v{a.agent_version}</span>
                                        )}
                                    </TableCell>
                                    <TableCell>
                                        {online(a) ? (
                                            <Badge variant="outline" className="border-green-500 text-green-600">Online</Badge>
                                        ) : (
                                            <Badge variant="outline" className="text-muted-foreground">
                                                {a.last_seen_at ? 'Offline' : 'Mai collegata'}
                                            </Badge>
                                        )}
                                    </TableCell>
                                    <TableCell className="min-w-[13rem]">
                                        <Select
                                            value={a.scope}
                                            onValueChange={(v) => update.mutate({ id: a.id, scope: v as EdgeAgentScope })}
                                        >
                                            <SelectTrigger className="h-8" aria-label="Cosa interroga">
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="assigned">Solo i gateway assegnati</SelectItem>
                                                <SelectItem value="all">Anche tutti i non assegnati</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </TableCell>
                                    <TableCell className="text-right tabular-nums">{a.gateways}</TableCell>
                                    <TableCell className="text-right">
                                        <Button size="icon" variant="ghost" aria-label="Rimuovi scatola"
                                            onClick={() => {
                                                if (confirm(`Rimuovere "${a.name}"? La sua chiave viene revocata subito e i suoi ${a.gateways} gateway tornano al server.`)) {
                                                    remove.mutate(a.id);
                                                }
                                            }}>
                                            <Trash2 size={14} className="text-destructive" />
                                        </Button>
                                    </TableCell>
                                </TableRow>
                            ))}
                        </TableBody>
                    </Table>
                </div>
            )}
            <p className="text-[11px] text-muted-foreground">
                "Anche tutti i non assegnati" serve quando il server è in cloud e non interroga
                niente: una sola scatola per organizzazione può averlo.
            </p>
        </div>
    );
}
