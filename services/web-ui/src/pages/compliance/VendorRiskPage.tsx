import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';

import {
    Building2, Plus, RefreshCw, Pencil, Trash2, Loader2, Check, X, Globe, Mail, Network, Wifi
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter
} from '@/components/ui/dialog';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue
} from '@/components/ui/select';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow
} from '@/components/ui/table';
import { cn } from '@/lib/utils';
import { complianceApi, OTVendor } from '@/api/compliance';

// ─── helpers ─────────────────────────────────────────────────────────────────

function scoreColor(score: number): string {
    if (score >= 76) return 'text-green-500';
    if (score >= 51) return 'text-yellow-500';
    if (score >= 26) return 'text-orange-500';
    return 'text-red-500';
}

function ScoreBar({ score }: { score: number }) {
    const color =
        score >= 76 ? 'bg-green-500' :
        score >= 51 ? 'bg-yellow-500' :
        score >= 26 ? 'bg-orange-500' :
        'bg-red-500';
    return (
        <div className="flex items-center gap-2">
            <div className="w-24 h-2 bg-muted rounded-full overflow-hidden shrink-0">
                <div className={cn('h-2 rounded-full', color)} style={{ width: `${score}%` }} />
            </div>
            <span className={cn('text-sm font-bold tabular-nums', scoreColor(score))}>{score}/100</span>
        </div>
    );
}

const CRITICALITY_BADGE: Record<OTVendor['criticality'], string> = {
    critical: 'bg-red-500 text-white',
    high: 'bg-orange-500 text-white',
    medium: 'bg-yellow-500 text-white',
    low: 'bg-green-500 text-white',
};

const CRITICALITY_LABELS: Record<OTVendor['criticality'], string> = {
    critical: 'Critica',
    high: 'Alta',
    medium: 'Media',
    low: 'Bassa',
};

const ACCESS_BADGE: Record<OTVendor['data_access_level'], string> = {
    none: 'bg-muted text-muted-foreground',
    read: 'bg-blue-500/20 text-blue-500',
    write: 'bg-orange-500/20 text-orange-500',
    admin: 'bg-red-500/20 text-red-500',
};

function formatDate(s?: string): string {
    if (!s) return '—';
    try { return new Date(s).toLocaleDateString('it-IT'); } catch { return s; }
}

function contractStatus(dateStr?: string): { label: string; cls: string } {
    if (!dateStr) return { label: '—', cls: 'text-muted-foreground' };
    const d = new Date(dateStr);
    const now = new Date();
    const diffMs = d.getTime() - now.getTime();
    const diffDays = diffMs / 86400000;
    if (diffDays < 0) return { label: formatDate(dateStr), cls: 'text-red-500 font-bold' };
    if (diffDays < 30) return { label: formatDate(dateStr), cls: 'text-orange-500 font-bold' };
    return { label: formatDate(dateStr), cls: 'text-foreground' };
}

// ─── Vendor Form ──────────────────────────────────────────────────────────────

const emptyVendor: Partial<OTVendor> = {
    name: '',
    country: '',
    website: '',
    contact_email: '',
    products_used: '',
    has_iso27001: false,
    has_soc2: false,
    has_iec62443: false,
    network_access: false,
    remote_access: false,
    security_clauses: false,
    data_access_level: 'none',
    last_audit_date: '',
    contract_start: '',
    contract_end: '',
    notes: '',
};

interface VendorDialogProps {
    open: boolean;
    onClose: () => void;
    vendor?: OTVendor | null;
}

function VendorDialog({ open, onClose, vendor }: VendorDialogProps) {
    const qc = useQueryClient();
    const [form, setForm] = useState<Partial<OTVendor>>(vendor ?? emptyVendor);

    // Sync form when vendor changes
    const handleOpen = (o: boolean) => {
        if (o) setForm(vendor ?? emptyVendor);
        if (!o) onClose();
    };

    const isEdit = !!vendor;

    const save = useMutation({
        mutationFn: () =>
            isEdit
                ? complianceApi.updateVendor(vendor!.id, form)
                : complianceApi.createVendor(form),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['vendors'] });
            qc.invalidateQueries({ queryKey: ['vendors', 'summary'] });
            toast.success(isEdit ? 'Fornitore aggiornato' : 'Fornitore aggiunto');
            onClose();
        },
        onError: () => toast.error('Errore nel salvataggio'),
    });

    const cb = (field: keyof OTVendor) => (e: React.ChangeEvent<HTMLInputElement>) =>
        setForm(p => ({ ...p, [field]: e.target.checked }));
    const txt = (field: keyof OTVendor) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
        setForm(p => ({ ...p, [field]: e.target.value }));

    return (
        <Dialog open={open} onOpenChange={handleOpen}>
            <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>{isEdit ? 'Modifica Fornitore' : 'Aggiungi Fornitore'}</DialogTitle>
                </DialogHeader>
                <div className="grid gap-4 py-2">
                    {/* Base info */}
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                        <div className="grid gap-2">
                            <Label>Nome <span className="text-destructive">*</span></Label>
                            <Input value={form.name} onChange={txt('name')} placeholder="Siemens AG" />
                        </div>
                        <div className="grid gap-2">
                            <Label>Paese</Label>
                            <Input value={form.country ?? ''} onChange={txt('country')} placeholder="DE" />
                        </div>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                        <div className="grid gap-2">
                            <Label>Sito web</Label>
                            <Input value={form.website ?? ''} onChange={txt('website')} placeholder="https://siemens.com" />
                        </div>
                        <div className="grid gap-2">
                            <Label>Email contatto</Label>
                            <Input value={form.contact_email ?? ''} onChange={txt('contact_email')} placeholder="security@vendor.com" />
                        </div>
                    </div>
                    <div className="grid gap-2">
                        <Label>Prodotti utilizzati</Label>
                        <textarea
                            className="flex min-h-[60px] w-full rounded-none clip-chamfer-sm border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring resize-none"
                            value={form.products_used ?? ''}
                            onChange={txt('products_used')}
                            placeholder="PLC S7-1200, SCADA WinCC..."
                        />
                    </div>

                    {/* Certifications & access */}
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-6">
                        <div className="space-y-2">
                            <Label className="text-xs uppercase tracking-wider text-muted-foreground">Certificazioni</Label>
                            {[
                                { field: 'has_iso27001' as const, label: 'ISO 27001' },
                                { field: 'has_soc2' as const, label: 'SOC 2' },
                                { field: 'has_iec62443' as const, label: 'IEC 62443' },
                            ].map(({ field, label }) => (
                                <label key={field} className="flex items-center gap-2 cursor-pointer">
                                    <input
                                        type="checkbox"
                                        checked={!!form[field]}
                                        onChange={cb(field)}
                                        className="h-4 w-4"
                                    />
                                    <span className="text-sm">{label}</span>
                                </label>
                            ))}
                        </div>
                        <div className="space-y-2">
                            <Label className="text-xs uppercase tracking-wider text-muted-foreground">Accesso</Label>
                            {[
                                { field: 'network_access' as const, label: 'Accesso di rete' },
                                { field: 'remote_access' as const, label: 'Accesso remoto' },
                                { field: 'security_clauses' as const, label: 'Clausole di sicurezza' },
                            ].map(({ field, label }) => (
                                <label key={field} className="flex items-center gap-2 cursor-pointer">
                                    <input
                                        type="checkbox"
                                        checked={!!form[field]}
                                        onChange={cb(field)}
                                        className="h-4 w-4"
                                    />
                                    <span className="text-sm">{label}</span>
                                </label>
                            ))}
                        </div>
                    </div>

                    <div className="grid gap-2">
                        <Label>Livello accesso ai dati</Label>
                        <Select
                            value={form.data_access_level}
                            onValueChange={v => setForm(p => ({ ...p, data_access_level: v as OTVendor['data_access_level'] }))}
                        >
                            <SelectTrigger><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value="none">Nessuno</SelectItem>
                                <SelectItem value="read">Lettura</SelectItem>
                                <SelectItem value="write">Scrittura</SelectItem>
                                <SelectItem value="admin">Admin</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>

                    {/* Dates */}
                    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                        <div className="grid gap-2">
                            <Label>Ultimo audit</Label>
                            <Input type="date" value={form.last_audit_date ?? ''} onChange={txt('last_audit_date')} />
                        </div>
                        <div className="grid gap-2">
                            <Label>Inizio contratto</Label>
                            <Input type="date" value={form.contract_start ?? ''} onChange={txt('contract_start')} />
                        </div>
                        <div className="grid gap-2">
                            <Label>Fine contratto</Label>
                            <Input type="date" value={form.contract_end ?? ''} onChange={txt('contract_end')} />
                        </div>
                    </div>

                    <div className="grid gap-2">
                        <Label>Note</Label>
                        <textarea
                            className="flex min-h-[60px] w-full rounded-none clip-chamfer-sm border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring resize-none"
                            value={form.notes ?? ''}
                            onChange={txt('notes')}
                            placeholder="Note interne sul fornitore..."
                        />
                    </div>
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={onClose}>Annulla</Button>
                    <Button onClick={() => save.mutate()} disabled={!form.name || save.isPending}>
                        {save.isPending ? <Loader2 size={14} className="animate-spin mr-1" /> : null}
                        {isEdit ? 'Salva modifiche' : 'Aggiungi'}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export default function VendorRiskPage() {
    const qc = useQueryClient();
    const [vendorDialogOpen, setVendorDialogOpen] = useState(false);
    const [editVendor, setEditVendor] = useState<OTVendor | null>(null);

    const { data: vendors = [], isLoading } = useQuery({
        queryKey: ['vendors'],
        queryFn: () => complianceApi.listVendors(),
    });

    const { data: summary } = useQuery({
        queryKey: ['vendors', 'summary'],
        queryFn: () => complianceApi.getVendorSummary(),
    });

    const deleteVendor = useMutation({
        mutationFn: (id: number) => complianceApi.deleteVendor(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['vendors'] });
            qc.invalidateQueries({ queryKey: ['vendors', 'summary'] });
            toast.success('Fornitore eliminato');
        },
        onError: () => toast.error('Errore durante l\'eliminazione'),
    });

    const syncGW = useMutation({
        mutationFn: () => complianceApi.syncVendorsFromGateways(),
        onSuccess: (r) => {
            qc.invalidateQueries({ queryKey: ['vendors'] });
            qc.invalidateQueries({ queryKey: ['vendors', 'summary'] });
            toast.success(`Importati ${r.imported} fornitori, aggiornati ${r.updated}`);
        },
        onError: () => toast.error('Errore durante la sincronizzazione'),
    });

    const openNew = () => {
        setEditVendor(null);
        setVendorDialogOpen(true);
    };

    const openEdit = (v: OTVendor) => {
        setEditVendor(v);
        setVendorDialogOpen(true);
    };

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between flex-wrap gap-3">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight flex items-center gap-2">
                        <Building2 size={22} className="text-blue-500" />
                        Vendor Risk — Art.18 NIS2
                    </h2>
                    <p className="text-muted-foreground">Gestione del rischio nella catena di fornitura OT</p>
                </div>
                <div className="flex gap-2">
                    <Button
                        variant="outline"
                        className="gap-2"
                        onClick={() => syncGW.mutate()}
                        disabled={syncGW.isPending}
                    >
                        {syncGW.isPending
                            ? <Loader2 size={16} className="animate-spin" />
                            : <RefreshCw size={16} />
                        }
                        Sincronizza da Gateway
                    </Button>
                    <Button className="gap-2" onClick={openNew}>
                        <Plus size={16} /> Aggiungi Fornitore
                    </Button>
                </div>
            </div>

            {/* Summary cards */}
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Totale fornitori</p>
                    <p className="text-3xl font-bold mt-1">{summary?.total ?? 0}</p>
                </div>
                <div className={cn('clip-chamfer border bg-card p-4', (summary?.critical ?? 0) > 0 && 'border-red-500/40')}>
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Criticità critica</p>
                    <p className={cn('text-3xl font-bold mt-1', (summary?.critical ?? 0) > 0 ? 'text-red-500' : '')}>{summary?.critical ?? 0}</p>
                </div>
                <div className={cn('clip-chamfer border bg-card p-4', (summary?.high ?? 0) > 0 && 'border-orange-500/40')}>
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Criticità alta</p>
                    <p className={cn('text-3xl font-bold mt-1', (summary?.high ?? 0) > 0 ? 'text-orange-500' : '')}>{summary?.high ?? 0}</p>
                </div>
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Score medio</p>
                    <p className={cn('text-3xl font-bold mt-1', scoreColor(summary?.avg_score ?? 0))}>{summary?.avg_score?.toFixed(0) ?? '—'}<span className="text-base text-muted-foreground">/100</span></p>
                </div>
            </div>

            {/* Table */}
            <div className="clip-chamfer border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Nome</TableHead>
                            <TableHead>Paese</TableHead>
                            <TableHead>Score</TableHead>
                            <TableHead>Criticità</TableHead>
                            <TableHead>Certificazioni</TableHead>
                            <TableHead>Accesso</TableHead>
                            <TableHead>Contratto (fine)</TableHead>
                            <TableHead className="text-right">Azioni</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {isLoading ? (
                            <TableRow>
                                <TableCell colSpan={8} className="h-24 text-center text-muted-foreground">
                                    <Loader2 size={16} className="animate-spin inline mr-2" />
                                    Caricamento fornitori...
                                </TableCell>
                            </TableRow>
                        ) : vendors.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={8} className="h-32 text-center">
                                    <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                        <Building2 size={40} className="opacity-30" />
                                        <p>Nessun fornitore registrato. Aggiungine uno o sincronizza dai gateway.</p>
                                    </div>
                                </TableCell>
                            </TableRow>
                        ) : (
                            vendors.map(v => {
                                const cs = contractStatus(v.contract_end);
                                return (
                                    <TableRow key={v.id}>
                                        <TableCell>
                                            <div className="flex items-center gap-2">
                                                <span className="font-medium text-sm">{v.name}</span>
                                                {v.auto_imported && (
                                                    <span className="inline-flex items-center rounded-none clip-chamfer-sm px-1.5 py-0.5 text-[10px] font-bold bg-blue-500/20 text-blue-400">GW</span>
                                                )}
                                            </div>
                                            {v.website && (
                                                <a href={v.website} target="_blank" rel="noopener noreferrer" className="flex items-center gap-1 text-xs text-muted-foreground hover:text-blue-400">
                                                    <Globe size={10} /> {v.website.replace(/^https?:\/\//, '')}
                                                </a>
                                            )}
                                        </TableCell>
                                        <TableCell className="text-sm text-muted-foreground">{v.country || '—'}</TableCell>
                                        <TableCell>
                                            <ScoreBar score={v.risk_score} />
                                        </TableCell>
                                        <TableCell>
                                            <span className={cn('inline-flex items-center rounded-none clip-chamfer-sm px-2 py-0.5 text-xs font-bold', CRITICALITY_BADGE[v.criticality])}>
                                                {CRITICALITY_LABELS[v.criticality]}
                                            </span>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-1.5 text-xs">
                                                <span className={cn('flex items-center gap-0.5', v.has_iso27001 ? 'text-green-500' : 'text-muted-foreground/40')}>
                                                    {v.has_iso27001 ? <Check size={11} /> : <X size={11} />} ISO27001
                                                </span>
                                                <span className={cn('flex items-center gap-0.5', v.has_soc2 ? 'text-green-500' : 'text-muted-foreground/40')}>
                                                    {v.has_soc2 ? <Check size={11} /> : <X size={11} />} SOC2
                                                </span>
                                                <span className={cn('flex items-center gap-0.5', v.has_iec62443 ? 'text-green-500' : 'text-muted-foreground/40')}>
                                                    {v.has_iec62443 ? <Check size={11} /> : <X size={11} />} IEC62443
                                                </span>
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-1.5 flex-wrap">
                                                <span className={cn('inline-flex items-center rounded-none clip-chamfer-sm px-1.5 py-0.5 text-[10px] font-bold', ACCESS_BADGE[v.data_access_level])}>
                                                    {v.data_access_level}
                                                </span>
                                                {v.network_access && (
                                                    <span title="Accesso di rete" className="text-orange-400"><Network size={12} /></span>
                                                )}
                                                {v.remote_access && (
                                                    <span title="Accesso remoto" className="text-red-400"><Wifi size={12} /></span>
                                                )}
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            <span className={cs.cls}>{cs.label}</span>
                                        </TableCell>
                                        <TableCell className="text-right">
                                            <div className="flex items-center justify-end gap-1">
                                                {v.contact_email && (
                                                    <Button variant="ghost" size="icon" className="h-7 w-7" asChild>
                                                        <a href={`mailto:${v.contact_email}`} title={v.contact_email}>
                                                            <Mail size={13} />
                                                        </a>
                                                    </Button>
                                                )}
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-7 w-7"
                                                    onClick={() => openEdit(v)}
                                                >
                                                    <Pencil size={13} />
                                                </Button>
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-7 w-7 text-destructive hover:text-destructive hover:bg-destructive/10"
                                                    onClick={() => {
                                                        if (confirm(`Eliminare il fornitore "${v.name}"?`)) deleteVendor.mutate(v.id);
                                                    }}
                                                >
                                                    <Trash2 size={13} />
                                                </Button>
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                );
                            })
                        )}
                    </TableBody>
                </Table>
            </div>

            <VendorDialog
                open={vendorDialogOpen}
                onClose={() => {
                    setVendorDialogOpen(false);
                    setEditVendor(null);
                }}
                vendor={editVendor}
            />
        </div>
    );
}
