import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
    AlertTriangle, Clock, FileText, Plus, CheckCircle2, XCircle, Loader2
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
import { cn } from '@/lib/utils';
import { complianceApi, CSIRTIncident } from '@/api/compliance';

// ─── helpers ─────────────────────────────────────────────────────────────────

function formatRemainingH(h: number): { label: string; cls: string } {
    if (h > 0) {
        if (h > 48) {
            const days = Math.floor(h / 24);
            return { label: `${days}gg rimanenti`, cls: 'text-green-500' };
        }
        return { label: `${Math.round(h)}h rimanenti`, cls: 'text-green-500' };
    }
    const abs = Math.abs(Math.round(h));
    if (abs > 48) {
        return { label: `SCADUTA da ${Math.floor(abs / 24)}gg`, cls: 'text-red-500 font-bold' };
    }
    return { label: `SCADUTA da ${abs}h`, cls: 'text-red-500 font-bold' };
}

const SEVERITY_BORDER: Record<CSIRTIncident['severity'], string> = {
    critical: 'border-l-red-500',
    high: 'border-l-orange-500',
    medium: 'border-l-yellow-500',
    low: 'border-l-blue-500',
};

const SEVERITY_LABELS: Record<CSIRTIncident['severity'], string> = {
    critical: 'Critico',
    high: 'Alto',
    medium: 'Medio',
    low: 'Basso',
};

const SEVERITY_BADGE: Record<CSIRTIncident['severity'], string> = {
    critical: 'bg-red-500 text-white',
    high: 'bg-orange-500 text-white',
    medium: 'bg-yellow-500 text-white',
    low: 'bg-blue-500 text-white',
};

function StatusBadge({ status }: { status: CSIRTIncident['status'] }) {
    const map: Record<CSIRTIncident['status'], { label: string; cls: string }> = {
        open: { label: 'Aperto', cls: 'bg-orange-500 text-white' },
        early_warning_sent: { label: 'Early Warning Inviata', cls: 'bg-blue-500 text-white' },
        notified: { label: 'Notificato', cls: 'bg-purple-500 text-white' },
        closed: { label: 'Chiuso', cls: 'bg-slate-500 text-white' },
    };
    const cfg = map[status] ?? { label: status, cls: 'bg-muted text-muted-foreground' };
    return (
        <span className={cn('inline-flex items-center rounded-none clip-chamfer-sm px-2 py-0.5 text-xs font-bold', cfg.cls)}>
            {cfg.label}
        </span>
    );
}

// ─── Deadline Row ─────────────────────────────────────────────────────────────

interface DeadlineRowProps {
    icon: React.ReactNode;
    label: string;
    sentAt?: string;
    remainingH: number;
    overdue: boolean;
    canAct: boolean;
    onAct: () => void;
    actLabel: string;
    isPending: boolean;
}

function DeadlineRow({ icon, label, sentAt, remainingH, overdue, canAct, onAct, actLabel, isPending }: DeadlineRowProps) {
    const { label: timeLabel, cls } = formatRemainingH(remainingH);
    return (
        <div className="flex items-center gap-3 py-1.5">
            <span className="text-muted-foreground w-4 shrink-0">{icon}</span>
            <span className="text-sm w-52 shrink-0 text-muted-foreground">{label}</span>
            {sentAt ? (
                <span className="flex items-center gap-1 text-green-500 text-sm font-medium flex-1">
                    <CheckCircle2 size={14} /> Inviata ✓
                </span>
            ) : (
                <div className="flex items-center gap-2 flex-1">
                    {overdue && (
                        <span className="inline-block w-2 h-2 rounded-full bg-red-500 animate-pulse shrink-0" />
                    )}
                    <span className={cn('text-sm', cls)}>{timeLabel}</span>
                </div>
            )}
            {canAct && !sentAt && (
                <Button
                    size="sm"
                    variant="outline"
                    className="h-7 text-xs shrink-0"
                    onClick={onAct}
                    disabled={isPending}
                >
                    {isPending ? <Loader2 size={12} className="animate-spin" /> : actLabel}
                </Button>
            )}
        </div>
    );
}

// ─── Incident Card ────────────────────────────────────────────────────────────

interface IncidentCardProps {
    incident: CSIRTIncident;
    onClose: () => void;
}

function IncidentCard({ incident, onClose }: IncidentCardProps) {
    const qc = useQueryClient();

    const markEW = useMutation({
        mutationFn: () => complianceApi.markEarlyWarning(incident.id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['csirt', 'incidents'] });
            qc.invalidateQueries({ queryKey: ['csirt', 'summary'] });
            toast.success('Early warning segnata come inviata');
        },
        onError: () => toast.error('Errore'),
    });

    const markNotify = useMutation({
        mutationFn: () => complianceApi.markNotification(incident.id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['csirt', 'incidents'] });
            qc.invalidateQueries({ queryKey: ['csirt', 'summary'] });
            toast.success('Notifica formale segnata come inviata');
        },
        onError: () => toast.error('Errore'),
    });

    const isOpen = incident.status !== 'closed';

    const formatDate = (s?: string) => {
        if (!s) return undefined;
        try { return new Date(s).toLocaleString('it-IT'); } catch { return s; }
    };

    return (
        <div className={cn(
            'border-l-4 border bg-card clip-chamfer p-4 space-y-3',
            SEVERITY_BORDER[incident.severity]
        )}>
            {/* Top row */}
            <div className="flex items-start gap-3 flex-wrap">
                <span className={cn('inline-flex items-center rounded-none clip-chamfer-sm px-2 py-0.5 text-xs font-bold shrink-0', SEVERITY_BADGE[incident.severity])}>
                    {SEVERITY_LABELS[incident.severity]}
                </span>
                <StatusBadge status={incident.status} />
                <h3 className="font-semibold text-sm flex-1 min-w-0">{incident.title}</h3>
                <span className="text-xs text-muted-foreground shrink-0">{formatDate(incident.created_at)}</span>
            </div>

            {/* Description / affected */}
            {(incident.description || incident.affected_systems) && (
                <div className="text-sm text-muted-foreground space-y-0.5">
                    {incident.description && <p>{incident.description}</p>}
                    {incident.affected_systems && (
                        <p><span className="font-medium text-foreground">Sistemi interessati:</span> {incident.affected_systems}</p>
                    )}
                </div>
            )}

            {/* Deadlines */}
            <div className="border-t pt-2 space-y-0.5">
                <DeadlineRow
                    icon={<Clock size={14} />}
                    label="Early Warning (24h)"
                    sentAt={incident.early_warning_sent_at}
                    remainingH={incident.early_warning_remaining_h}
                    overdue={incident.early_warning_overdue}
                    canAct={isOpen}
                    onAct={() => markEW.mutate()}
                    actLabel="Segna inviata"
                    isPending={markEW.isPending}
                />
                <DeadlineRow
                    icon={<FileText size={14} />}
                    label="Notifica formale (72h)"
                    sentAt={incident.notification_sent_at}
                    remainingH={incident.notification_remaining_h}
                    overdue={incident.notification_overdue}
                    canAct={isOpen}
                    onAct={() => markNotify.mutate()}
                    actLabel="Segna inviata"
                    isPending={markNotify.isPending}
                />
                <DeadlineRow
                    icon={<FileText size={14} />}
                    label="Rapporto finale (30gg)"
                    sentAt={incident.final_report_sent_at}
                    remainingH={incident.final_report_remaining_h}
                    overdue={incident.final_report_overdue}
                    canAct={false}
                    onAct={() => {}}
                    actLabel=""
                    isPending={false}
                />
            </div>

            {/* Bottom row */}
            {isOpen && (
                <div className="border-t pt-2 flex justify-end">
                    <Button
                        variant="outline"
                        size="sm"
                        className="h-7 text-xs gap-1 text-destructive border-destructive/40 hover:bg-destructive/10"
                        onClick={onClose}
                    >
                        <XCircle size={12} /> Chiudi Incidente
                    </Button>
                </div>
            )}
            {incident.closed_at && (
                <p className="text-xs text-muted-foreground">Chiuso il {formatDate(incident.closed_at)}</p>
            )}
        </div>
    );
}

// ─── New Incident Dialog ──────────────────────────────────────────────────────

interface NewIncidentDialogProps {
    open: boolean;
    onClose: () => void;
}

function NewIncidentDialog({ open, onClose }: NewIncidentDialogProps) {
    const qc = useQueryClient();
    const [form, setForm] = useState<Partial<CSIRTIncident>>({
        title: '',
        severity: 'high',
        description: '',
        affected_systems: '',
        impact_description: '',
    });

    const create = useMutation({
        mutationFn: () => complianceApi.createIncident(form),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['csirt', 'incidents'] });
            qc.invalidateQueries({ queryKey: ['csirt', 'summary'] });
            toast.success('Incidente creato');
            onClose();
            setForm({ title: '', severity: 'high', description: '', affected_systems: '', impact_description: '' });
        },
        onError: () => toast.error('Errore nella creazione dell\'incidente'),
    });

    return (
        <Dialog open={open} onOpenChange={onClose}>
            <DialogContent className="max-w-lg">
                <DialogHeader>
                    <DialogTitle>Nuovo Incidente CSIRT</DialogTitle>
                </DialogHeader>
                <div className="grid gap-4 py-2">
                    <div className="grid gap-2">
                        <Label>Titolo <span className="text-destructive">*</span></Label>
                        <Input
                            value={form.title}
                            onChange={e => setForm(p => ({ ...p, title: e.target.value }))}
                            placeholder="Es. Accesso non autorizzato a PLC linea 3"
                        />
                    </div>
                    <div className="grid gap-2">
                        <Label>Severità</Label>
                        <Select
                            value={form.severity}
                            onValueChange={v => setForm(p => ({ ...p, severity: v as CSIRTIncident['severity'] }))}
                        >
                            <SelectTrigger><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value="critical">Critico</SelectItem>
                                <SelectItem value="high">Alto</SelectItem>
                                <SelectItem value="medium">Medio</SelectItem>
                                <SelectItem value="low">Basso</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>
                    <div className="grid gap-2">
                        <Label>Descrizione</Label>
                        <textarea
                            className="flex min-h-[80px] w-full rounded-none clip-chamfer-sm border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring resize-none"
                            value={form.description}
                            onChange={e => setForm(p => ({ ...p, description: e.target.value }))}
                            placeholder="Descrizione dettagliata dell'incidente..."
                        />
                    </div>
                    <div className="grid gap-2">
                        <Label>Sistemi interessati</Label>
                        <Input
                            value={form.affected_systems}
                            onChange={e => setForm(p => ({ ...p, affected_systems: e.target.value }))}
                            placeholder="PLC linea 3, HMI sala controllo..."
                        />
                    </div>
                    <div className="grid gap-2">
                        <Label>Impatto</Label>
                        <textarea
                            className="flex min-h-[60px] w-full rounded-none clip-chamfer-sm border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring resize-none"
                            value={form.impact_description}
                            onChange={e => setForm(p => ({ ...p, impact_description: e.target.value }))}
                            placeholder="Impatto sulle operazioni industriali..."
                        />
                    </div>
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={onClose}>Annulla</Button>
                    <Button
                        onClick={() => create.mutate()}
                        disabled={!form.title || create.isPending}
                    >
                        {create.isPending ? <Loader2 size={14} className="animate-spin mr-1" /> : null}
                        Crea Incidente
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

// ─── Close Incident Dialog ────────────────────────────────────────────────────

interface CloseIncidentDialogProps {
    open: boolean;
    incidentId: number | null;
    onClose: () => void;
}

function CloseIncidentDialog({ open, incidentId, onClose }: CloseIncidentDialogProps) {
    const qc = useQueryClient();
    const [form, setForm] = useState({ root_cause: '', remediation: '' });

    const close = useMutation({
        mutationFn: () => complianceApi.closeIncident(incidentId!, form),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['csirt', 'incidents'] });
            qc.invalidateQueries({ queryKey: ['csirt', 'summary'] });
            toast.success('Incidente chiuso');
            onClose();
            setForm({ root_cause: '', remediation: '' });
        },
        onError: () => toast.error('Errore nella chiusura'),
    });

    return (
        <Dialog open={open} onOpenChange={onClose}>
            <DialogContent className="max-w-md">
                <DialogHeader>
                    <DialogTitle>Chiudi Incidente</DialogTitle>
                </DialogHeader>
                <div className="grid gap-4 py-2">
                    <div className="grid gap-2">
                        <Label>Causa radice</Label>
                        <textarea
                            className="flex min-h-[80px] w-full rounded-none clip-chamfer-sm border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring resize-none"
                            value={form.root_cause}
                            onChange={e => setForm(p => ({ ...p, root_cause: e.target.value }))}
                            placeholder="Descrizione della causa radice..."
                        />
                    </div>
                    <div className="grid gap-2">
                        <Label>Rimediazione</Label>
                        <textarea
                            className="flex min-h-[80px] w-full rounded-none clip-chamfer-sm border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring resize-none"
                            value={form.remediation}
                            onChange={e => setForm(p => ({ ...p, remediation: e.target.value }))}
                            placeholder="Azioni correttive adottate..."
                        />
                    </div>
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={onClose}>Annulla</Button>
                    <Button
                        onClick={() => close.mutate()}
                        disabled={close.isPending}
                        variant="destructive"
                    >
                        {close.isPending ? <Loader2 size={14} className="animate-spin mr-1" /> : null}
                        Chiudi Incidente
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export default function CSIRTPage() {
    const [newOpen, setNewOpen] = useState(false);
    const [closeId, setCloseId] = useState<number | null>(null);

    const { data: incidents = [], isLoading } = useQuery({
        queryKey: ['csirt', 'incidents'],
        queryFn: () => complianceApi.listIncidents(),
        refetchInterval: 30_000,
    });

    const { data: summary } = useQuery({
        queryKey: ['csirt', 'summary'],
        queryFn: () => complianceApi.getIncidentSummary(),
        refetchInterval: 30_000,
    });

    const sorted = [...incidents].sort(
        (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
    );

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight flex items-center gap-2">
                        <AlertTriangle size={22} className="text-orange-500" />
                        CSIRT — Gestione Incidenti Art.23
                    </h2>
                    <p className="text-muted-foreground">Gestione e tracciamento incidenti NIS2 con scadenze di notifica obbligatorie</p>
                </div>
                <Button className="gap-2" onClick={() => setNewOpen(true)}>
                    <Plus size={16} /> Nuovo Incidente
                </Button>
            </div>

            {/* Summary cards */}
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div className="clip-chamfer border bg-card p-4">
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Totale incidenti</p>
                    <p className="text-3xl font-bold mt-1">{summary?.total ?? 0}</p>
                </div>
                <div className={cn('clip-chamfer border bg-card p-4', (summary?.open ?? 0) > 0 && 'border-orange-500/40')}>
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Aperti</p>
                    <p className={cn('text-3xl font-bold mt-1', (summary?.open ?? 0) > 0 ? 'text-orange-500' : '')}>{summary?.open ?? 0}</p>
                </div>
                <div className={cn('clip-chamfer border bg-card p-4', (summary?.overdue_early_warning ?? 0) > 0 && 'border-red-500/40')}>
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Early Warning scadute</p>
                    <p className={cn('text-3xl font-bold mt-1', (summary?.overdue_early_warning ?? 0) > 0 ? 'text-red-500' : '')}>{summary?.overdue_early_warning ?? 0}</p>
                </div>
                <div className={cn('clip-chamfer border bg-card p-4', (summary?.overdue_notification ?? 0) > 0 && 'border-red-500/40')}>
                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Notifiche scadute</p>
                    <p className={cn('text-3xl font-bold mt-1', (summary?.overdue_notification ?? 0) > 0 ? 'text-red-500' : '')}>{summary?.overdue_notification ?? 0}</p>
                </div>
            </div>

            {/* Incident list */}
            {isLoading ? (
                <div className="flex items-center justify-center py-16 text-muted-foreground gap-2">
                    <Loader2 size={20} className="animate-spin" /> Caricamento incidenti...
                </div>
            ) : sorted.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-16 text-muted-foreground gap-3">
                    <AlertTriangle size={40} className="opacity-30" />
                    <p>Nessun incidente registrato. Crea il primo incidente con il pulsante in alto.</p>
                </div>
            ) : (
                <div className="space-y-4">
                    {sorted.map(inc => (
                        <IncidentCard
                            key={inc.id}
                            incident={inc}
                            onClose={() => setCloseId(inc.id)}
                        />
                    ))}
                </div>
            )}

            <NewIncidentDialog open={newOpen} onClose={() => setNewOpen(false)} />
            <CloseIncidentDialog
                open={closeId !== null}
                incidentId={closeId}
                onClose={() => setCloseId(null)}
            />
        </div>
    );
}
