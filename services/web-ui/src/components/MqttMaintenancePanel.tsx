import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { KeyRound } from 'lucide-react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { EmptyState } from '@/components/ui/empty-state';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { mqttMaintenanceApi } from '@/api/mqttClients';
import { Button } from '@/components/ui/button';
import { Eye } from 'lucide-react';
import { organizationsApi } from '@/api/organizations';

/**
 * Global-admin maintenance over every organization's external MQTT logins,
 * plus the read-only reconciliation of what the broker actually knows.
 * Creation stays in the organization's own infrastructure dialog (org context);
 * this panel governs: what exists, whether it is enabled, whether the broker
 * still knows it.
 */
export default function MqttMaintenancePanel() {
    const { t } = useTranslation();
    const qc = useQueryClient();
    const [orgFilter, setOrgFilter] = useState<string>('all');
    const [showBroker, setShowBroker] = useState(false);
    // Revealed secret display: cleared as soon as the admin acknowledges it.
    const [revealed, setRevealed] = useState<{ username: string; password: string } | null>(null);
    const [revealing, setRevealing] = useState(false);

    const { data: orgs = [] } = useQuery({
        queryKey: ['organizations'],
        queryFn: () => organizationsApi.getAll(),
    });

    const regKey = ['mqtt-maintenance', orgFilter];
    const { data: registry, isLoading: regLoading } = useQuery({
        queryKey: regKey,
        queryFn: () =>
            mqttMaintenanceApi.listAll(orgFilter === 'all' ? undefined : Number(orgFilter)),
    });

    const { data: broker, isLoading: brokerLoading } = useQuery({
        queryKey: ['mqtt-broker-clients'],
        queryFn: () => mqttMaintenanceApi.brokerClients(),
        enabled: showBroker,
        staleTime: 30_000,
    });

    const refresh = () => {
        void qc.invalidateQueries({ queryKey: ['mqtt-maintenance'] });
        void qc.invalidateQueries({ queryKey: ['mqtt-broker-clients'] });
    };

    const regUsernames = new Set((registry?.items ?? []).map((c) => c.username));
    const orphaned = (broker?.items ?? []).filter((b) => !regUsernames.has(b.username));

    return (
        <div className="space-y-6">
            <Card className="border-border shadow-xs bg-card">
                <CardHeader className="pb-4 border-b border-border">
                    <div className="flex flex-wrap items-center gap-3 justify-between">
                        <div>
                            <CardTitle className="text-base text-foreground">{t('mqttMaintenance.registry_title')}</CardTitle>
                            <CardDescription className="text-xs mt-0.5">
                                {t('mqttMaintenance.registry_desc')}
                            </CardDescription>
                        </div>
                        <div className="flex items-center gap-2">
                            <Select value={orgFilter} onValueChange={setOrgFilter}>
                                <SelectTrigger className="w-48">
                                    <SelectValue placeholder={t('mqttMaintenance.all_orgs')} />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="all">{t('mqttMaintenance.all_orgs')}</SelectItem>
                                    {orgs.map((o) => (
                                        <SelectItem key={o.id} value={String(o.id)}>{o.name}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <button
                                type="button"
                                className="text-xs underline underline-offset-2 text-muted-foreground"
                                onClick={refresh}
                            >
                                {t('common.refresh')}
                            </button>
                        </div>
                    </div>
                </CardHeader>
                {revealed && (
                    <div className="mx-6 mt-4 rounded-md border border-amber-300 bg-amber-50 dark:bg-amber-950/30 dark:border-amber-800 p-3 text-sm flex items-center gap-2">
                        <code className="font-mono text-xs break-all flex-1">{revealed.username}: {revealed.password}</code>
                        <Button size="sm" variant="outline" onClick={() => setRevealed(null)}>{t('mqttClients.saved_it')}</Button>
                    </div>
                )}
                <CardContent className="pt-4">
                    {regLoading ? (
                        <p className="text-sm text-muted-foreground py-4 text-center">{t('common.loading')}</p>
                    ) : !registry || registry.total === 0 ? (
                        <EmptyState icon={KeyRound} title={t('mqttMaintenance.empty_title')} description={t('mqttMaintenance.empty_desc')} className="py-6" />
                    ) : (
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>{t('mqttMaintenance.org')}</TableHead>
                                    <TableHead>{t('mqttClients.what_for')}</TableHead>
                                    <TableHead>{t('mqttClients.username')}</TableHead>
                                    <TableHead>{t('mqttClients.status')}</TableHead>
                                    <TableHead className="w-12" />
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {registry.items.map((c) => (
                                    <TableRow key={c.id}>
                                        <TableCell className="font-medium">{c.org_name}</TableCell>
                                        <TableCell>{c.description}</TableCell>
                                        <TableCell className="font-mono text-xs">{c.username}</TableCell>
                                        <TableCell>
                                            <span className={`inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-medium ${!c.on_broker ? 'bg-red-500/10 text-red-600' : c.enabled ? 'bg-green-500/10 text-green-600' : 'bg-slate-500/10 text-slate-500'}`}>
                                                {!c.on_broker ? t('mqttClients.stale') : c.enabled ? t('mqttClients.active') : t('mqttClients.disabled')}
                                            </span>
                                        </TableCell>
                                        <TableCell>
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-8 w-8"
                                                aria-label={t('mqttClients.show')}
                                                title={t('mqttClients.show_title')}
                                                disabled={revealing}
                                                onClick={async () => {
                                                    setRevealing(true);
                                                    try {
                                                        const r = await mqttMaintenanceApi.revealPassword(c.id);
                                                        setRevealed({ username: r.username, password: r.password });
                                                    } catch {
                                                        /* api-error toast handled globally */
                                                    } finally {
                                                        setRevealing(false);
                                                    }
                                                }}
                                            >
                                                <Eye size={14} />
                                            </Button>
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    )}
                    <p className="text-xs text-muted-foreground mt-3">{t('mqttMaintenance.manage_hint')}</p>
                </CardContent>
            </Card>

            <Card className="border-border shadow-xs bg-card">
                <CardHeader className="pb-4 border-b border-border">
                    <div className="flex flex-wrap items-center gap-3 justify-between">
                        <div>
                            <CardTitle className="text-base text-foreground">{t('mqttMaintenance.broker_title')}</CardTitle>
                            <CardDescription className="text-xs mt-0.5">
                                {t('mqttMaintenance.broker_desc')}
                            </CardDescription>
                        </div>
                        <button
                            type="button"
                            className="text-xs underline underline-offset-2 text-muted-foreground"
                            onClick={() => { setShowBroker((v) => !v); }}
                        >
                            {showBroker ? t('common.hide') : t('common.show')}
                        </button>
                    </div>
                </CardHeader>
                {showBroker && (
                    <CardContent className="pt-4">
                        {brokerLoading ? (
                            <p className="text-sm text-muted-foreground py-4 text-center">{t('common.loading')}</p>
                        ) : (
                            <>
                                {orphaned.length > 0 && (
                                    <div className="rounded-md border border-amber-300 bg-amber-50 dark:bg-amber-950/30 dark:border-amber-800 p-3 mb-3 text-sm">
                                        <p className="font-medium">{t('mqttMaintenance.orphans_title', { count: orphaned.length })}</p>
                                        <p className="text-xs text-muted-foreground mt-1">{t('mqttMaintenance.orphans_desc')}</p>
                                        <div className="mt-2 font-mono text-xs space-y-0.5">
                                            {orphaned.map((b) => (
                                                <div key={b.username}>{b.username} <span className="text-muted-foreground">({b.roles.join(', ')})</span></div>
                                            ))}
                                        </div>
                                    </div>
                                )}
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead>{t('mqttClients.username')}</TableHead>
                                            <TableHead>{t('mqttMaintenance.roles')}</TableHead>
                                            <TableHead>{t('mqttClients.status')}</TableHead>
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {(broker?.items ?? []).map((b) => (
                                            <TableRow key={b.username}>
                                                <TableCell className="font-mono text-xs">{b.username}</TableCell>
                                                <TableCell className="text-xs text-muted-foreground">{b.roles.join(', ') || '—'}</TableCell>
                                                <TableCell>
                                                    <span className={`inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-medium ${b.disabled ? 'bg-slate-500/10 text-slate-500' : 'bg-green-500/10 text-green-600'}`}>
                                                        {b.disabled ? t('mqttClients.disabled') : t('mqttClients.active')}
                                                    </span>
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </>
                        )}
                    </CardContent>
                )}
            </Card>
        </div>
    );
}
