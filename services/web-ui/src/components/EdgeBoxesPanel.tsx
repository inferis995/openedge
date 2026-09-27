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
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';
import { useTranslation } from 'react-i18next';

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
    const { t } = useTranslation();
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
            showApiSuccess(t('edgeBoxes.updated'));
        },
        onError: (e) => showApiError(e, t('edgeBoxes.update_failed')),
    });

    const remove = useMutation({
        mutationFn: (id: number) => edgeAgentsApi.remove(orgId, id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: key });
            showApiSuccess(t('edgeBoxes.removed'), t('edgeBoxes.removed_desc'));
        },
        onError: (e) => showApiError(e, t('edgeBoxes.remove_failed')),
    });

    if (isLoading || !data) {
        return <p className="text-xs text-muted-foreground">{t('edgeBoxes.loading')}</p>;
    }

    const online = (a: EdgeAgent) =>
        !!a.last_seen_at && Date.now() - new Date(a.last_seen_at).getTime() < OFFLINE_AFTER_MS;

    return (
        <div className="space-y-3">
            {/* Il server: interroga i gateway che nessuna scatola ha. */}
            <div className="flex items-start gap-3 rounded-lg border p-3">
                <Server size={16} className="mt-0.5 text-muted-foreground" />
                <div className="text-sm">
                    <p className="font-medium">{t('edgeBoxes.server')}</p>
                    {data.server_polls ? (
                        <p className="text-xs text-muted-foreground">
                            {t('edgeBoxes.server_polls', { count: data.server_gateways })}
                        </p>
                    ) : (
                        <p className="text-xs text-muted-foreground">
                            {t('edgeBoxes.server_no_poll')}
                        </p>
                    )}
                </div>
            </div>

            {data.unpolled_gateways > 0 && (
                <div className="flex items-start gap-2 rounded-lg border border-amber-500/50 bg-amber-500/10 p-3 text-xs">
                    <AlertTriangle size={14} className="mt-0.5 text-amber-600" />
                    <span>
                        <strong>{t('edgeBoxes.unpolled', { count: data.unpolled_gateways })}</strong>{' '}
                        {t('edgeBoxes.unpolled_hint')}
                    </span>
                </div>
            )}

            {data.agents.length === 0 ? (
                <p className="text-xs text-muted-foreground">
                    {t('edgeBoxes.empty')}
                </p>
            ) : (
                <div className="rounded-lg border overflow-x-auto">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>{t('edgeBoxes.col_box')}</TableHead>
                                <TableHead>{t('edgeBoxes.col_status')}</TableHead>
                                <TableHead>{t('edgeBoxes.col_polls')}</TableHead>
                                <TableHead className="text-right">{t('common.gateway')}</TableHead>
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
                                                    aria-label={t('edgeBoxes.name_aria')}
                                                />
                                                <Button size="icon" variant="ghost" aria-label={t('edgeBoxes.save_name')}
                                                    onClick={() => update.mutate({ id: a.id, name: draftName })}>
                                                    <Check size={14} />
                                                </Button>
                                                <Button size="icon" variant="ghost" aria-label={t('common.cancel')}
                                                    onClick={() => setEditing(null)}>
                                                    <X size={14} />
                                                </Button>
                                            </div>
                                        ) : (
                                            <div className="flex items-center gap-1">
                                                <span className="font-medium">{a.name}</span>
                                                <Button size="icon" variant="ghost" aria-label={t('edgeBoxes.rename')}
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
                                            <Badge variant="outline" className="border-green-500 text-green-600">{t('edgeBoxes.online')}</Badge>
                                        ) : (
                                            <Badge variant="outline" className="text-muted-foreground">
                                                {a.last_seen_at ? t('edgeBoxes.offline') : t('edgeBoxes.never_seen')}
                                            </Badge>
                                        )}
                                    </TableCell>
                                    <TableCell className="min-w-[13rem]">
                                        <Select
                                            value={a.scope}
                                            onValueChange={(v) => update.mutate({ id: a.id, scope: v as EdgeAgentScope })}
                                        >
                                            <SelectTrigger className="h-8" aria-label={t('edgeBoxes.scope_aria')}>
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="assigned">{t('edgeBoxes.scope_assigned')}</SelectItem>
                                                <SelectItem value="all">{t('edgeBoxes.scope_all')}</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </TableCell>
                                    <TableCell className="text-right tabular-nums">{a.gateways}</TableCell>
                                    <TableCell className="text-right">
                                        <Button size="icon" variant="ghost" aria-label={t('edgeBoxes.remove_aria')}
                                            onClick={async () => {
                                                if (await confirmAction({ title: i18n.t('ask.remove_box', { name: a.name }), description: i18n.t('ask.remove_box_desc', { count: a.gateways }), confirmLabel: i18n.t('ask.remove'), destructive: true })) {
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
                {t('edgeBoxes.scope_all_hint')}
            </p>
        </div>
    );
}
