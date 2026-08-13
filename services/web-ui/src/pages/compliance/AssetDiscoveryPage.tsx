import { useState, useEffect, useRef } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
    Shield, Plus, Search, Scan, CheckCircle, XCircle, ChevronDown, ChevronRight, Trash2, RefreshCw
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter
} from '@/components/ui/dialog';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue
} from '@/components/ui/select';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow
} from '@/components/ui/table';
import { complianceApi, OTAsset, CVEMatch } from '@/api/compliance';

const DEVICE_TYPES = ['PLC', 'HMI', 'Switch', 'Router', 'Sensor', 'Unknown'] as const;
const PROTOCOLS = ['Modbus TCP', 'S7', 'OPC UA', 'MQTT', 'DNP3', 'BACnet', 'EtherNet/IP', 'PROFINET', 'Unknown'];

function RiskBadge({ score }: { score: number }) {
    const color =
        score >= 7 ? 'bg-red-500 text-white' :
        score >= 4 ? 'bg-yellow-500 text-white' :
        'bg-green-500 text-white';
    return (
        <span className={`inline-flex items-center justify-center rounded-none clip-chamfer-sm px-2 py-0.5 text-xs font-bold ${color}`}>
            {score.toFixed(1)}
        </span>
    );
}

function SeverityBadge({ severity }: { severity: CVEMatch['severity'] }) {
    const map: Record<string, string> = {
        CRITICAL: 'bg-red-600 text-white',
        HIGH: 'bg-orange-500 text-white',
        MEDIUM: 'bg-yellow-500 text-white',
        LOW: 'bg-blue-500 text-white',
    };
    return (
        <span className={`inline-flex items-center justify-center rounded-none clip-chamfer-sm px-2 py-0.5 text-xs font-bold ${map[severity] ?? 'bg-muted text-muted-foreground'}`}>
            {severity}
        </span>
    );
}

export default function AssetDiscoveryPage() {
    const qc = useQueryClient();
    const [search, setSearch] = useState('');
    const [expandedId, setExpandedId] = useState<number | null>(null);

    // Scan dialog
    const [scanOpen, setScanOpen] = useState(false);
    const [subnet, setSubnet] = useState('192.168.1.0/24');
    const [scanJobId, setScanJobId] = useState<number | null>(null);
    const [scanStatus, setScanStatus] = useState<string | null>(null);
    const scanPollRef = useRef<ReturnType<typeof setInterval> | null>(null);

    // Add asset dialog
    const [assetOpen, setAssetOpen] = useState(false);
    const [assetForm, setAssetForm] = useState<Partial<OTAsset>>({
        ip_address: '', hostname: '', vendor: '', model: '',
        device_type: 'PLC', protocol: 'Modbus TCP', is_authorized: false, notes: ''
    });

    // Add CVE dialog
    const [cveOpen, setCveOpen] = useState(false);
    const [cveAssetId, setCveAssetId] = useState<number | null>(null);
    const [cveForm, setCveForm] = useState<Partial<CVEMatch>>({
        cve_id: '', severity: 'MEDIUM', cvss_score: undefined, description: ''
    });

    const { data: assets = [], isLoading } = useQuery({
        queryKey: ['ot-assets'],
        queryFn: () => complianceApi.listAssets(),
    });

    const createAsset = useMutation({
        mutationFn: (data: Partial<OTAsset>) => complianceApi.createAsset(data),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['ot-assets'] });
            setAssetOpen(false);
            toast.success('Asset aggiunto con successo');
        },
        onError: () => toast.error('Errore durante l\'aggiunta dell\'asset'),
    });

    const deleteAsset = useMutation({
        mutationFn: (id: number) => complianceApi.deleteAsset(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['ot-assets'] });
            toast.success('Asset eliminato');
        },
        onError: () => toast.error('Errore durante l\'eliminazione'),
    });

    const addCVE = useMutation({
        mutationFn: ({ assetId, cve }: { assetId: number; cve: Partial<CVEMatch> }) =>
            complianceApi.addCVE(assetId, cve),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['ot-assets'] });
            setCveOpen(false);
            toast.success('CVE aggiunta');
        },
        onError: () => toast.error('Errore durante l\'aggiunta della CVE'),
    });

    const removeCVE = useMutation({
        mutationFn: ({ assetId, cveId }: { assetId: number; cveId: string }) =>
            complianceApi.removeCVE(assetId, cveId),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['ot-assets'] });
            toast.success('CVE rimossa');
        },
    });

    const triggerScan = useMutation({
        mutationFn: (s: string) => complianceApi.triggerScan(s),
        onSuccess: (job) => {
            setScanJobId(job.id);
            setScanStatus('running');
            toast.info(`Scansione avviata su ${subnet}`);
        },
        onError: () => toast.error('Errore avvio scansione'),
    });

    const syncAssets = useMutation({
        mutationFn: () => complianceApi.syncAssetsFromGateways(),
        onSuccess: (r) => {
            qc.invalidateQueries({ queryKey: ['ot-assets'] });
            toast.success(`Creati ${r.created} asset, aggiornati ${r.updated} da gateway configurati`);
        },
        onError: () => toast.error('Errore durante la sincronizzazione dai gateway'),
    });

    // Poll scan status
    useEffect(() => {
        if (scanJobId && scanStatus === 'running') {
            let count = 0;
            scanPollRef.current = setInterval(async () => {
                count++;
                try {
                    const job = await complianceApi.getScanStatus(scanJobId);
                    setScanStatus(job.status);
                    if (job.status === 'completed') {
                        clearInterval(scanPollRef.current!);
                        toast.success(`Scansione completata: ${job.found_count} asset trovati (${job.new_count} nuovi)`);
                        qc.invalidateQueries({ queryKey: ['ot-assets'] });
                        setScanOpen(false);
                        setScanJobId(null);
                    } else if (job.status === 'failed') {
                        clearInterval(scanPollRef.current!);
                        toast.error(`Scansione fallita: ${job.error}`);
                        setScanJobId(null);
                    }
                } catch {
                    // ignore poll errors
                }
                if (count >= 5) {
                    clearInterval(scanPollRef.current!);
                    setScanStatus('timeout');
                    setScanJobId(null);
                }
            }, 2000);
        }
        return () => { if (scanPollRef.current) clearInterval(scanPollRef.current); };
    }, [scanJobId, scanStatus, qc]);

    const filtered = assets.filter(a =>
        !search ||
        a.ip_address.includes(search) ||
        a.hostname.toLowerCase().includes(search.toLowerCase()) ||
        a.vendor.toLowerCase().includes(search.toLowerCase()) ||
        a.model.toLowerCase().includes(search.toLowerCase())
    );

    const totalAssets = assets.length;
    const criticalAssets = assets.filter(a => a.risk_score >= 7).length;
    const unauthorized = assets.filter(a => !a.is_authorized).length;
    const criticalCves = assets.reduce((acc, a) => acc + (a.cves?.filter(c => c.severity === 'CRITICAL').length ?? 0), 0);

    const formatDate = (s: string) => {
        if (!s) return '—';
        try { return new Date(s).toLocaleDateString('it-IT'); } catch { return s; }
    };

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between gap-4 flex-wrap">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Inventario Asset OT</h2>
                    <p className="text-muted-foreground">Scopri e gestisci tutti i dispositivi industriali</p>
                    <p className="text-xs text-muted-foreground mt-1">
                        I gateway configurati nella tua org vengono rilevati automaticamente ogni ora.
                    </p>
                </div>
                <div className="flex gap-2 flex-wrap">
                    <Button
                        variant="outline"
                        className="gap-2"
                        onClick={() => syncAssets.mutate()}
                        disabled={syncAssets.isPending}
                    >
                        <RefreshCw size={16} className={syncAssets.isPending ? 'animate-spin' : ''} />
                        Sincronizza da Gateway
                    </Button>
                    <Button variant="outline" className="gap-2" onClick={() => setScanOpen(true)}>
                        <Scan size={16} /> Avvia Scansione
                    </Button>
                    <Button className="gap-2" onClick={() => setAssetOpen(true)}>
                        <Plus size={16} /> Aggiungi Asset
                    </Button>
                </div>
            </div>

            {/* Stat Cards */}
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Totale Asset</p>
                    <p className="text-3xl font-bold mt-1">{totalAssets}</p>
                </div>
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Asset Critici</p>
                    <p className="text-3xl font-bold mt-1 text-red-500">{criticalAssets}</p>
                    <p className="text-xs text-muted-foreground">risk ≥ 7</p>
                </div>
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Non Autorizzati</p>
                    <p className="text-3xl font-bold mt-1 text-yellow-500">{unauthorized}</p>
                </div>
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">CVE Critiche</p>
                    <p className="text-3xl font-bold mt-1 text-red-600">{criticalCves}</p>
                </div>
            </div>

            {/* Search */}
            <div className="relative w-full sm:w-72">
                <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
                <Input
                    className="pl-8 h-8 text-sm"
                    placeholder="Cerca IP, hostname, vendor..."
                    value={search}
                    onChange={e => setSearch(e.target.value)}
                />
            </div>

            {/* Table */}
            <div className="clip-chamfer border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-8" />
                            <TableHead>IP / Hostname</TableHead>
                            <TableHead>Vendor / Modello</TableHead>
                            <TableHead>Tipo</TableHead>
                            <TableHead>Protocollo</TableHead>
                            <TableHead>Risk Score</TableHead>
                            <TableHead>Autorizzato</TableHead>
                            <TableHead>Ultima Vista</TableHead>
                            <TableHead className="text-right">Azioni</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {isLoading ? (
                            <TableRow>
                                <TableCell colSpan={9} className="h-24 text-center text-muted-foreground">
                                    Caricamento asset...
                                </TableCell>
                            </TableRow>
                        ) : filtered.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={9} className="h-32 text-center">
                                    <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                        <Shield size={40} className="opacity-30" />
                                        <p>Nessun asset rilevato. Avvia una scansione o aggiungi manualmente.</p>
                                    </div>
                                </TableCell>
                            </TableRow>
                        ) : (
                            filtered.map(asset => (
                                <>
                                    <TableRow
                                        key={asset.id}
                                        className="cursor-pointer hover:bg-muted/50"
                                        onClick={() => setExpandedId(expandedId === asset.id ? null : asset.id)}
                                    >
                                        <TableCell>
                                            {expandedId === asset.id
                                                ? <ChevronDown size={14} className="text-muted-foreground" />
                                                : <ChevronRight size={14} className="text-muted-foreground" />
                                            }
                                        </TableCell>
                                        <TableCell>
                                            <div>
                                                <p className="font-mono text-sm">{asset.ip_address}</p>
                                                <div className="flex items-center gap-1.5">
                                                    <p className="text-xs text-muted-foreground">{asset.hostname || '—'}</p>
                                                    {asset.gateway_id != null && (
                                                        <span className="inline-flex items-center rounded-none clip-chamfer-sm px-1 py-0 text-[9px] font-bold bg-blue-500/20 text-blue-400">GW</span>
                                                    )}
                                                </div>
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            <div>
                                                <p className="text-sm font-medium">{asset.vendor || '—'}</p>
                                                <p className="text-xs text-muted-foreground">{asset.model || '—'}</p>
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            <Badge variant="outline">{asset.device_type}</Badge>
                                        </TableCell>
                                        <TableCell className="text-sm">{asset.protocol || '—'}</TableCell>
                                        <TableCell>
                                            <RiskBadge score={asset.risk_score} />
                                        </TableCell>
                                        <TableCell>
                                            {asset.is_authorized
                                                ? <Badge variant="success" className="gap-1"><CheckCircle size={10} /> Sì</Badge>
                                                : <Badge variant="destructive" className="gap-1"><XCircle size={10} /> No</Badge>
                                            }
                                        </TableCell>
                                        <TableCell className="text-sm text-muted-foreground">{formatDate(asset.last_seen)}</TableCell>
                                        <TableCell className="text-right">
                                            <div className="flex items-center justify-end gap-2" onClick={e => e.stopPropagation()}>
                                                <Button
                                                    variant="outline"
                                                    size="sm"
                                                    className="h-7 text-xs"
                                                    onClick={() => {
                                                        setCveAssetId(asset.id);
                                                        setCveForm({ cve_id: '', severity: 'MEDIUM', cvss_score: undefined, description: '' });
                                                        setCveOpen(true);
                                                    }}
                                                >
                                                    + CVE
                                                </Button>
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-7 w-7 text-destructive hover:text-destructive hover:bg-destructive/10"
                                                    onClick={() => {
                                                        if (confirm('Eliminare questo asset?')) deleteAsset.mutate(asset.id);
                                                    }}
                                                >
                                                    <Trash2 size={14} />
                                                </Button>
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                    {expandedId === asset.id && (
                                        <TableRow key={`${asset.id}-cves`} className="bg-muted/20">
                                            <TableCell colSpan={9} className="p-4">
                                                <div className="space-y-2">
                                                    <p className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">CVE Associate</p>
                                                    {(!asset.cves || asset.cves.length === 0) ? (
                                                        <p className="text-sm text-muted-foreground">Nessuna CVE associata a questo asset.</p>
                                                    ) : (
                                                        <div className="space-y-1">
                                                            {asset.cves.map(cve => (
                                                                <div key={cve.id} className="flex items-center gap-3 p-2 rounded bg-card border text-sm">
                                                                    <SeverityBadge severity={cve.severity} />
                                                                    <span className="font-mono font-bold text-xs">{cve.cve_id}</span>
                                                                    {cve.cvss_score != null && (
                                                                        <span className="text-xs text-muted-foreground">CVSS: {cve.cvss_score}</span>
                                                                    )}
                                                                    <span className="text-muted-foreground flex-1">{cve.description}</span>
                                                                    <Button
                                                                        variant="ghost"
                                                                        size="icon"
                                                                        className="h-6 w-6 text-destructive"
                                                                        onClick={() => removeCVE.mutate({ assetId: asset.id, cveId: cve.cve_id })}
                                                                    >
                                                                        <Trash2 size={12} />
                                                                    </Button>
                                                                </div>
                                                            ))}
                                                        </div>
                                                    )}
                                                    {asset.notes && (
                                                        <p className="text-xs text-muted-foreground mt-2">Note: {asset.notes}</p>
                                                    )}
                                                </div>
                                            </TableCell>
                                        </TableRow>
                                    )}
                                </>
                            ))
                        )}
                    </TableBody>
                </Table>
            </div>

            {/* Scan Dialog */}
            <Dialog open={scanOpen} onOpenChange={setScanOpen}>
                <DialogContent className="max-w-sm">
                    <DialogHeader>
                        <DialogTitle>Avvia Scansione Rete</DialogTitle>
                    </DialogHeader>
                    <div className="grid gap-4 py-2">
                        <div className="grid gap-2">
                            <Label htmlFor="subnet">Subnet (CIDR)</Label>
                            <Input
                                id="subnet"
                                value={subnet}
                                onChange={e => setSubnet(e.target.value)}
                                placeholder="192.168.1.0/24"
                            />
                        </div>
                        {scanStatus === 'running' && (
                            <div className="flex items-center gap-2 text-sm text-muted-foreground">
                                <Scan size={14} className="animate-spin" />
                                Scansione in corso...
                            </div>
                        )}
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setScanOpen(false)}>Annulla</Button>
                        <Button
                            onClick={() => triggerScan.mutate(subnet)}
                            disabled={!subnet || scanStatus === 'running' || triggerScan.isPending}
                        >
                            Avvia
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Add Asset Dialog */}
            <Dialog open={assetOpen} onOpenChange={setAssetOpen}>
                <DialogContent className="max-w-lg">
                    <DialogHeader>
                        <DialogTitle>Aggiungi Asset OT</DialogTitle>
                    </DialogHeader>
                    <div className="grid gap-4 py-2">
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="grid gap-2">
                                <Label>Indirizzo IP</Label>
                                <Input
                                    value={assetForm.ip_address}
                                    onChange={e => setAssetForm(p => ({ ...p, ip_address: e.target.value }))}
                                    placeholder="192.168.1.10"
                                />
                            </div>
                            <div className="grid gap-2">
                                <Label>Hostname</Label>
                                <Input
                                    value={assetForm.hostname}
                                    onChange={e => setAssetForm(p => ({ ...p, hostname: e.target.value }))}
                                    placeholder="plc-linea1"
                                />
                            </div>
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="grid gap-2">
                                <Label>Vendor</Label>
                                <Input
                                    value={assetForm.vendor}
                                    onChange={e => setAssetForm(p => ({ ...p, vendor: e.target.value }))}
                                    placeholder="Siemens"
                                />
                            </div>
                            <div className="grid gap-2">
                                <Label>Modello</Label>
                                <Input
                                    value={assetForm.model}
                                    onChange={e => setAssetForm(p => ({ ...p, model: e.target.value }))}
                                    placeholder="S7-1200"
                                />
                            </div>
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="grid gap-2">
                                <Label>Tipo dispositivo</Label>
                                <Select
                                    value={assetForm.device_type}
                                    onValueChange={v => setAssetForm(p => ({ ...p, device_type: v as OTAsset['device_type'] }))}
                                >
                                    <SelectTrigger><SelectValue /></SelectTrigger>
                                    <SelectContent>
                                        {DEVICE_TYPES.map(t => <SelectItem key={t} value={t}>{t}</SelectItem>)}
                                    </SelectContent>
                                </Select>
                            </div>
                            <div className="grid gap-2">
                                <Label>Protocollo</Label>
                                <Select
                                    value={assetForm.protocol}
                                    onValueChange={v => setAssetForm(p => ({ ...p, protocol: v }))}
                                >
                                    <SelectTrigger><SelectValue /></SelectTrigger>
                                    <SelectContent>
                                        {PROTOCOLS.map(p => <SelectItem key={p} value={p}>{p}</SelectItem>)}
                                    </SelectContent>
                                </Select>
                            </div>
                        </div>
                        <div className="flex items-center gap-2">
                            <input
                                type="checkbox"
                                id="is_authorized"
                                checked={assetForm.is_authorized}
                                onChange={e => setAssetForm(p => ({ ...p, is_authorized: e.target.checked }))}
                                className="h-4 w-4"
                            />
                            <Label htmlFor="is_authorized">Asset autorizzato</Label>
                        </div>
                        <div className="grid gap-2">
                            <Label>Note</Label>
                            <Input
                                value={assetForm.notes}
                                onChange={e => setAssetForm(p => ({ ...p, notes: e.target.value }))}
                                placeholder="Note opzionali..."
                            />
                        </div>
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setAssetOpen(false)}>Annulla</Button>
                        <Button
                            onClick={() => createAsset.mutate(assetForm)}
                            disabled={!assetForm.ip_address || createAsset.isPending}
                        >
                            Aggiungi
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Add CVE Dialog */}
            <Dialog open={cveOpen} onOpenChange={setCveOpen}>
                <DialogContent className="max-w-md">
                    <DialogHeader>
                        <DialogTitle>Aggiungi CVE</DialogTitle>
                    </DialogHeader>
                    <div className="grid gap-4 py-2">
                        <div className="grid gap-2">
                            <Label>CVE ID</Label>
                            <Input
                                value={cveForm.cve_id}
                                onChange={e => setCveForm(p => ({ ...p, cve_id: e.target.value }))}
                                placeholder="CVE-2024-12345"
                            />
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="grid gap-2">
                                <Label>Severity</Label>
                                <Select
                                    value={cveForm.severity}
                                    onValueChange={v => setCveForm(p => ({ ...p, severity: v as CVEMatch['severity'] }))}
                                >
                                    <SelectTrigger><SelectValue /></SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="CRITICAL">CRITICAL</SelectItem>
                                        <SelectItem value="HIGH">HIGH</SelectItem>
                                        <SelectItem value="MEDIUM">MEDIUM</SelectItem>
                                        <SelectItem value="LOW">LOW</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>
                            <div className="grid gap-2">
                                <Label>CVSS Score</Label>
                                <Input
                                    type="number"
                                    step="0.1"
                                    min="0"
                                    max="10"
                                    value={cveForm.cvss_score ?? ''}
                                    onChange={e => setCveForm(p => ({ ...p, cvss_score: e.target.value ? parseFloat(e.target.value) : undefined }))}
                                    placeholder="9.8"
                                />
                            </div>
                        </div>
                        <div className="grid gap-2">
                            <Label>Descrizione</Label>
                            <Input
                                value={cveForm.description}
                                onChange={e => setCveForm(p => ({ ...p, description: e.target.value }))}
                                placeholder="Descrizione breve della vulnerabilità..."
                            />
                        </div>
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setCveOpen(false)}>Annulla</Button>
                        <Button
                            onClick={() => {
                                if (cveAssetId) addCVE.mutate({ assetId: cveAssetId, cve: cveForm });
                            }}
                            disabled={!cveForm.cve_id || addCVE.isPending}
                        >
                            Aggiungi CVE
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
