import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
    Building2, CheckCircle2, Cpu, Factory, FileSpreadsheet, Loader2, PartyPopper, Tags, XCircle,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import TagImportDialog from '@/components/tags/TagImportDialog';
import { organizationsApi } from '@/api/organizations';
import { sitesApi } from '@/api/sites';
import { areasApi } from '@/api/areas';
import { gatewaysApi } from '@/api/gateways';
import { tagsApi } from '@/api/tags';
import { useAuthStore } from '@/stores/useAuthStore';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { showApiError } from '@/lib/api-error-handler';
import { cn } from '@/lib/utils';
import type { CreateGatewayDto } from '@/types';

type Driver = 'S7' | 'MODBUS_TCP' | 'OPC_UA';
const STEPS = ['plant', 'plc', 'tags', 'done'] as const;
type Step = (typeof STEPS)[number];

/**
 * First steps: from an empty installation to live values from a PLC.
 *
 * The pieces all existed — organizations, sites, areas, gateways, tags — on
 * five different pages, in an order a new user had to guess (a gateway needs
 * an area, an area a site, a site an organization), with nothing saying so.
 * This walks through them in that order, on one page, and ends on a PLC
 * whose connection has been tried.
 */
export default function SetupWizardPage() {
    const { t } = useTranslation();
    const navigate = useNavigate();
    const qc = useQueryClient();
    const { isGlobalAdmin, user } = useAuthStore();
    const { selectedOrgId, setSelectedOrgId, setSelectedSiteId, setSelectedAreaId } = useNavigationStore();

    const [step, setStep] = useState<Step>('plant');
    const [busy, setBusy] = useState(false);

    // Step 1 — plant
    const orgScoped = !!user?.org_id;
    const [orgId, setOrgId] = useState<number | null>(user?.org_id ?? selectedOrgId ?? null);
    const [newOrgName, setNewOrgName] = useState('');
    const [siteName, setSiteName] = useState('');
    const [areaName, setAreaName] = useState('');
    const [areaId, setAreaId] = useState<number | null>(null);

    const { data: orgs = [] } = useQuery({
        queryKey: ['organizations'],
        queryFn: organizationsApi.getAll,
        enabled: isGlobalAdmin(),
    });
    useEffect(() => {
        if (!orgId && orgs.length === 1) setOrgId(orgs[0].id);
    }, [orgs, orgId]);

    // Step 2 — PLC
    const [gwName, setGwName] = useState('');
    const [driver, setDriver] = useState<Driver>('S7');
    const [ip, setIp] = useState('');
    const [rack, setRack] = useState(0);
    const [slot, setSlot] = useState(1);
    const [port, setPort] = useState(502);
    const [slaveId, setSlaveId] = useState(1);
    const [endpoint, setEndpoint] = useState('');
    const [gatewayId, setGatewayId] = useState<number | null>(null);
    const [test, setTest] = useState<{ success: boolean; message: string } | null>(null);

    // Step 3 — tags
    const [importOpen, setImportOpen] = useState(false);
    const { data: tagCount = 0 } = useQuery({
        queryKey: ['tags', gatewayId ?? 'none', 'count'],
        queryFn: async () => (await tagsApi.getAll(gatewayId!)).length,
        enabled: !!gatewayId && step === 'tags',
        refetchInterval: step === 'tags' ? 3000 : false,
    });

    const savePlant = async () => {
        setBusy(true);
        try {
            let org = orgId;
            if (!org) {
                const created = await organizationsApi.create({ name: newOrgName.trim() });
                org = created.id;
                setOrgId(org);
                await qc.invalidateQueries({ queryKey: ['organizations'] });
            }
            setSelectedOrgId(org);
            const site = await sitesApi.create({ org_id: org, name: siteName.trim() });
            setSelectedSiteId(site.id);
            const area = await areasApi.create({ site_id: site.id, org_id: org, name: areaName.trim() });
            setSelectedAreaId(area.id);
            setAreaId(area.id);
            await qc.invalidateQueries({ queryKey: ['sites'] });
            await qc.invalidateQueries({ queryKey: ['areas'] });
            setStep('plc');
        } catch (e) {
            showApiError(e);
        } finally {
            setBusy(false);
        }
    };

    const connectionConfig = (): Record<string, unknown> => {
        switch (driver) {
            case 'MODBUS_TCP':
                return { transport: 'tcp', ip_address: ip.trim(), port, slave_id: slaveId };
            case 'OPC_UA':
                return { endpoint: endpoint.trim(), auth_mode: 'Anonymous' };
            default:
                return { ip_address: ip.trim(), rack, slot };
        }
    };

    const saveAndTestPLC = async () => {
        if (!areaId) return;
        setBusy(true);
        setTest(null);
        try {
            const dto = {
                area_id: areaId,
                name: gwName.trim(),
                driver_type: driver,
                ip_address: driver === 'OPC_UA' ? '' : ip.trim(),
                scan_rate_ms: 1000,
                enabled: true,
                org_id: orgId ?? undefined,
                connection_config: connectionConfig(),
            } as CreateGatewayDto & { connection_config: Record<string, unknown> };
            let id = gatewayId;
            if (id) {
                await gatewaysApi.update(id, dto);
            } else {
                id = (await gatewaysApi.create(dto)).id;
                setGatewayId(id);
            }
            await qc.invalidateQueries({ queryKey: ['gateways'] });
            setTest(await gatewaysApi.testConnection(id));
        } catch (e) {
            showApiError(e);
        } finally {
            setBusy(false);
        }
    };

    const plcReady = gwName.trim() && (driver === 'OPC_UA' ? endpoint.trim() : ip.trim());
    const plantReady = siteName.trim() && areaName.trim() && (orgId || newOrgName.trim());
    const idx = STEPS.indexOf(step);

    return (
        <div className="max-w-3xl mx-auto space-y-6">
            <div>
                <h2 className="text-2xl font-bold tracking-tight">{t('setup.title')}</h2>
                <p className="text-muted-foreground">{t('setup.subtitle')}</p>
            </div>

            {/* Stepper */}
            <ol className="grid grid-cols-4 gap-2">
                {STEPS.map((s, i) => (
                    <li key={s} className={cn(
                        'rounded-md border px-3 py-2 text-xs sm:text-sm flex items-center gap-2',
                        i < idx && 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
                        i === idx && 'border-primary bg-primary/5 font-semibold',
                        i > idx && 'text-muted-foreground',
                    )}>
                        {i < idx ? <CheckCircle2 size={14} /> : <span className="tabular-nums">{i + 1}</span>}
                        <span className="truncate">{t(`setup.step_${s}`)}</span>
                    </li>
                ))}
            </ol>

            {step === 'plant' && (
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2"><Factory size={18} /> {t('setup.plant_title')}</CardTitle>
                        <CardDescription>{t('setup.plant_desc')}</CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        {!orgScoped && (
                            <div className="space-y-2">
                                <Label className="flex items-center gap-1.5"><Building2 size={14} /> {t('common.organization')}</Label>
                                {orgs.length > 0 && (
                                    <Select value={orgId ? String(orgId) : 'new'} onValueChange={(v) => setOrgId(v === 'new' ? null : Number(v))}>
                                        <SelectTrigger><SelectValue /></SelectTrigger>
                                        <SelectContent>
                                            {orgs.map((o) => <SelectItem key={o.id} value={String(o.id)}>{o.name}</SelectItem>)}
                                            <SelectItem value="new">{t('setup.new_org')}</SelectItem>
                                        </SelectContent>
                                    </Select>
                                )}
                                {!orgId && (
                                    <Input value={newOrgName} onChange={(e) => setNewOrgName(e.target.value)} placeholder={t('setup.org_ph')} />
                                )}
                                <p className="text-xs text-muted-foreground">{t('setup.org_hint')}</p>
                            </div>
                        )}
                        <div className="grid sm:grid-cols-2 gap-4">
                            <div className="space-y-2">
                                <Label htmlFor="setup-site">{t('setup.site')}</Label>
                                <Input id="setup-site" value={siteName} onChange={(e) => setSiteName(e.target.value)} placeholder={t('sites.name_placeholder')} />
                            </div>
                            <div className="space-y-2">
                                <Label htmlFor="setup-area">{t('setup.area')}</Label>
                                <Input id="setup-area" value={areaName} onChange={(e) => setAreaName(e.target.value)} placeholder={t('areas.name_placeholder')} />
                            </div>
                        </div>
                        <div className="flex justify-end">
                            <Button onClick={() => void savePlant()} disabled={!plantReady || busy} className="gap-2">
                                {busy && <Loader2 size={14} className="animate-spin" />} {t('common.next')}
                            </Button>
                        </div>
                    </CardContent>
                </Card>
            )}

            {step === 'plc' && (
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2"><Cpu size={18} /> {t('setup.plc_title')}</CardTitle>
                        <CardDescription>{t('setup.plc_desc')}</CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <div className="grid sm:grid-cols-2 gap-4">
                            <div className="space-y-2">
                                <Label htmlFor="setup-gw">{t('setup.plc_name')}</Label>
                                <Input id="setup-gw" value={gwName} onChange={(e) => setGwName(e.target.value)} placeholder={t('setup.plc_name_ph')} />
                            </div>
                            <div className="space-y-2">
                                <Label>{t('setup.plc_type')}</Label>
                                <Select value={driver} onValueChange={(v) => { setDriver(v as Driver); setTest(null); }}>
                                    <SelectTrigger><SelectValue /></SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="S7">Siemens S7 (S7-300/400/1200/1500)</SelectItem>
                                        <SelectItem value="MODBUS_TCP">Modbus TCP</SelectItem>
                                        <SelectItem value="OPC_UA">OPC UA</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>
                        </div>

                        {driver === 'OPC_UA' ? (
                            <div className="space-y-2">
                                <Label htmlFor="setup-ep">{t('setup.endpoint')}</Label>
                                <Input id="setup-ep" value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="opc.tcp://192.168.1.10:4840" className="font-mono" />
                            </div>
                        ) : (
                            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
                                <div className="space-y-2 col-span-2">
                                    <Label htmlFor="setup-ip">{t('setup.ip')}</Label>
                                    <Input id="setup-ip" value={ip} onChange={(e) => setIp(e.target.value)} placeholder="192.168.0.10" className="font-mono" />
                                </div>
                                {driver === 'S7' ? (
                                    <>
                                        <div className="space-y-2">
                                            <Label>Rack</Label>
                                            <Input type="number" value={rack} onChange={(e) => setRack(Number(e.target.value) || 0)} />
                                        </div>
                                        <div className="space-y-2">
                                            <Label>Slot</Label>
                                            <Input type="number" value={slot} onChange={(e) => setSlot(Number(e.target.value) || 0)} />
                                        </div>
                                    </>
                                ) : (
                                    <>
                                        <div className="space-y-2">
                                            <Label>{t('setup.port')}</Label>
                                            <Input type="number" value={port} onChange={(e) => setPort(Number(e.target.value) || 502)} />
                                        </div>
                                        <div className="space-y-2">
                                            <Label>Slave ID</Label>
                                            <Input type="number" value={slaveId} onChange={(e) => setSlaveId(Number(e.target.value) || 1)} />
                                        </div>
                                    </>
                                )}
                            </div>
                        )}
                        {driver === 'S7' && <p className="text-xs text-muted-foreground">{t('setup.s7_hint')}</p>}

                        {test && (
                            <div className={cn('rounded-md border p-3 text-sm flex gap-2',
                                test.success ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                    : 'border-amber-300 bg-amber-50 text-amber-900 dark:bg-amber-950/30 dark:text-amber-200')}>
                                {test.success ? <CheckCircle2 size={16} className="mt-0.5 shrink-0" /> : <XCircle size={16} className="mt-0.5 shrink-0" />}
                                <div>
                                    <p className="font-medium">{test.success ? t('setup.test_ok') : t('setup.test_fail')}</p>
                                    {test.message && <p className="text-xs opacity-80 mt-0.5">{test.message}</p>}
                                    {!test.success && <p className="text-xs mt-1">{t('setup.test_fail_hint')}</p>}
                                </div>
                            </div>
                        )}

                        <div className="flex flex-wrap justify-between gap-2">
                            <Button variant="outline" onClick={() => void saveAndTestPLC()} disabled={!plcReady || busy} className="gap-2">
                                {busy && <Loader2 size={14} className="animate-spin" />}
                                {gatewayId ? t('setup.retest') : t('setup.connect')}
                            </Button>
                            <Button onClick={() => setStep('tags')} disabled={!gatewayId || busy}>
                                {test?.success ? t('common.next') : t('setup.continue_anyway')}
                            </Button>
                        </div>
                    </CardContent>
                </Card>
            )}

            {step === 'tags' && gatewayId && (
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2"><Tags size={18} /> {t('setup.tags_title')}</CardTitle>
                        <CardDescription>{t('setup.tags_desc')}</CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <div className="grid sm:grid-cols-2 gap-3">
                            <button type="button" onClick={() => setImportOpen(true)}
                                className="rounded-lg border p-4 text-left hover:bg-muted/50 transition-colors">
                                <FileSpreadsheet className="mb-2 text-primary" />
                                <p className="font-medium">{t('setup.tags_import')}</p>
                                <p className="text-xs text-muted-foreground">{t('setup.tags_import_desc')}</p>
                            </button>
                            <Link to={`/tags?gateway_id=${gatewayId}`}
                                className="rounded-lg border p-4 text-left hover:bg-muted/50 transition-colors">
                                <Tags className="mb-2 text-primary" />
                                <p className="font-medium">{t('setup.tags_manual')}</p>
                                <p className="text-xs text-muted-foreground">
                                    {driver === 'OPC_UA' ? t('setup.tags_browse_desc') : t('setup.tags_manual_desc')}
                                </p>
                            </Link>
                        </div>
                        <p className="text-sm">
                            {t('setup.tags_count', { count: tagCount })}
                        </p>
                        <div className="flex justify-end">
                            <Button onClick={() => setStep('done')} disabled={tagCount === 0}>{t('common.next')}</Button>
                        </div>
                        <TagImportDialog open={importOpen} onOpenChange={setImportOpen} gatewayId={gatewayId} driverType={driver} />
                    </CardContent>
                </Card>
            )}

            {step === 'done' && (
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2"><PartyPopper size={18} /> {t('setup.done_title')}</CardTitle>
                        <CardDescription>{t('setup.done_desc')}</CardDescription>
                    </CardHeader>
                    <CardContent className="grid sm:grid-cols-3 gap-3">
                        {([
                            ['/trend', 'setup.next_trend'],
                            ['/synoptics', 'setup.next_synoptic'],
                            [`/tags?gateway_id=${gatewayId}`, 'setup.next_alarms'],
                        ] as const).map(([to, key]) => (
                            <Button key={key} variant="outline" className="h-auto py-3 whitespace-normal" onClick={() => navigate(to)}>
                                {t(key)}
                            </Button>
                        ))}
                    </CardContent>
                </Card>
            )}
        </div>
    );
}
