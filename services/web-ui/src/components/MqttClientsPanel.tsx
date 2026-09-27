import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Copy, KeyRound, Plus, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { EmptyState } from '@/components/ui/empty-state';
import { mqttClientsApi } from '@/api/mqttClients';
import { showApiError } from '@/lib/api-error-handler';
import { confirmAction } from '@/lib/confirm';

/**
 * MQTT logins for the organization's external systems — a SCADA, Node-RED, a
 * MES. Each reads this organization's data and nothing else, and can be
 * revoked on its own. The password is shown once.
 */
export default function MqttClientsPanel({ orgId }: { orgId: number }) {
    const { t, i18n } = useTranslation();
    const qc = useQueryClient();
    const [description, setDescription] = useState('');
    const [fresh, setFresh] = useState<{ username: string; password: string; broker: string } | null>(null);

    const { data: clients = [] } = useQuery({
        queryKey: ['mqtt-clients', orgId],
        queryFn: () => mqttClientsApi.list(orgId),
    });

    const create = useMutation({
        mutationFn: () => mqttClientsApi.create(orgId, description.trim()),
        onSuccess: (c) => {
            setFresh({
                username: c.username,
                password: c.password,
                broker: `${c.broker_tls ? 'mqtts' : 'mqtt'}://${c.broker_host}:${c.broker_port}`,
            });
            setDescription('');
            void qc.invalidateQueries({ queryKey: ['mqtt-clients', orgId] });
        },
        onError: (e) => showApiError(e),
    });

    const remove = useMutation({
        mutationFn: (id: number) => mqttClientsApi.remove(orgId, id),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ['mqtt-clients', orgId] }),
        onError: (e) => showApiError(e),
    });

    const copy = (text: string) => {
        void navigator.clipboard?.writeText(text).then(
            () => toast.success(t('mqttClients.copied')),
            () => toast.error(t('mqttClients.copy_failed')),
        );
    };

    return (
        <div className="space-y-4">
            <p className="text-sm text-muted-foreground">{t('mqttClients.intro')}</p>

            <div className="flex flex-col sm:flex-row gap-2 sm:items-end">
                <div className="flex-1 space-y-1">
                    <Label htmlFor="mqtt-client-desc" className="text-xs">{t('mqttClients.what_for')}</Label>
                    <Input
                        id="mqtt-client-desc"
                        value={description}
                        maxLength={100}
                        onChange={(e) => setDescription(e.target.value)}
                        placeholder={t('mqttClients.what_for_ph')}
                    />
                </div>
                <Button className="gap-2" onClick={() => create.mutate()} disabled={!description.trim() || create.isPending}>
                    <Plus size={14} /> {t('mqttClients.create')}
                </Button>
            </div>

            {fresh && (
                <div className="rounded-md border border-amber-300 bg-amber-50 dark:bg-amber-950/30 dark:border-amber-800 p-3 space-y-2 text-sm">
                    <p className="font-medium">{t('mqttClients.once')}</p>
                    {[
                        [t('mqttClients.broker'), fresh.broker],
                        [t('mqttClients.username'), fresh.username],
                        [t('mqttClients.password'), fresh.password],
                        [t('mqttClients.topics'), 'data/#   ·   spBv1.0/#'],
                    ].map(([label, value]) => (
                        <div key={label} className="flex items-center gap-2">
                            <span className="w-28 shrink-0 text-xs text-muted-foreground">{label}</span>
                            <code className="flex-1 font-mono text-xs break-all">{value}</code>
                            <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" aria-label={t('mqttClients.copy')} onClick={() => copy(value)}>
                                <Copy size={13} />
                            </Button>
                        </div>
                    ))}
                    <Button size="sm" variant="outline" onClick={() => setFresh(null)}>{t('mqttClients.saved_it')}</Button>
                </div>
            )}

            {clients.length === 0 ? (
                <EmptyState icon={KeyRound} title={t('mqttClients.empty_title')} description={t('mqttClients.empty_desc')} className="py-6" />
            ) : (
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>{t('mqttClients.what_for')}</TableHead>
                            <TableHead>{t('mqttClients.username')}</TableHead>
                            <TableHead>{t('common.created_at')}</TableHead>
                            <TableHead className="w-12" />
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {clients.map((c) => (
                            <TableRow key={c.id}>
                                <TableCell className="font-medium">{c.description}</TableCell>
                                <TableCell className="font-mono text-xs">{c.username}</TableCell>
                                <TableCell className="text-xs text-muted-foreground">{new Date(c.created_at).toLocaleDateString(i18n.language)}</TableCell>
                                <TableCell>
                                    <Button
                                        variant="ghost"
                                        size="icon"
                                        className="h-8 w-8 text-destructive hover:text-destructive"
                                        aria-label={t('mqttClients.revoke')}
                                        onClick={async () => {
                                            if (await confirmAction({
                                                title: t('mqttClients.revoke_title', { name: c.description }),
                                                description: t('mqttClients.revoke_desc'),
                                                confirmLabel: t('mqttClients.revoke'),
                                                destructive: true,
                                            })) remove.mutate(c.id);
                                        }}
                                    >
                                        <Trash2 size={14} />
                                    </Button>
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            )}
        </div>
    );
}
