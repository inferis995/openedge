import { useState, useEffect } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
    Bell, Plus, Pencil, Trash2, Loader2, AlertCircle, CheckCircle2, Power, PowerOff,
} from 'lucide-react';

import { oeeApi, OEEAlertRule, OEEAlertRuleRequest, OEEProfile } from '@/api/dashboard';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription,
} from '@/components/ui/dialog';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';

// OEEAlertRules — gestione delle regole "OEE Linea A < 70% per 60min → email".
// Valutate dal cron worker ogni 5 minuti su oee_history.
//
// UX: lista compatta + dialog editor. Stato live: badge "violating" sulle
// regole attualmente scattate (last_state='violating').

const METRIC_LABELS: Record<string, string> = {
    oee:          'OEE',
    availability: 'Availability',
    performance:  'Performance',
    quality:      'Quality',
};

const SEVERITY_BADGE: Record<string, string> = {
    info:     'bg-slate-500/10 text-slate-500',
    warning:  'bg-amber-500/10 text-amber-500',
    critical: 'bg-red-500/10 text-red-500',
};

interface Props {
    profiles: OEEProfile[];
}

export const OEEAlertRules = ({ profiles }: Props) => {
    const [editorOpen, setEditorOpen] = useState(false);
    const [editing, setEditing] = useState<OEEAlertRule | null>(null);
    const queryClient = useQueryClient();

    const { data: rules, isLoading } = useQuery({
        queryKey: ['oee-alert-rules'],
        queryFn: oeeApi.listAlertRules,
    });

    const handleDelete = async (r: OEEAlertRule) => {
        if (!confirm(`Eliminare la regola "${r.name}"?`)) return;
        try {
            await oeeApi.deleteAlertRule(r.id);
            toast.success('Regola eliminata.');
            queryClient.invalidateQueries({ queryKey: ['oee-alert-rules'] });
        } catch (e: unknown) {
            toast.error(`Errore: ${(e as Error)?.message ?? 'unknown'}`);
        }
    };

    const handleToggle = async (r: OEEAlertRule) => {
        try {
            await oeeApi.updateAlertRule(r.id, ruleToRequest(r, { enabled: !r.enabled }));
            queryClient.invalidateQueries({ queryKey: ['oee-alert-rules'] });
        } catch (e: unknown) {
            toast.error(`Errore: ${(e as Error)?.message ?? 'unknown'}`);
        }
    };

    return (
        <div className="space-y-3">
            <div className="flex items-start justify-between gap-2">
                <div>
                    <p className="text-sm text-muted-foreground">
                        Regole notifica per soglie OEE. Quando una metrica resta sotto/sopra soglia
                        per N minuti consecutivi, parte un avviso email/Telegram via il dispatcher esistente.
                    </p>
                </div>
                <Button onClick={() => { setEditing(null); setEditorOpen(true); }} size="sm">
                    <Plus size={14} className="mr-1" /> Nuova regola
                </Button>
            </div>

            <Card>
                <CardHeader className="pb-3">
                    <CardTitle className="text-base flex items-center gap-2">
                        <Bell size={16} /> Regole configurate
                    </CardTitle>
                    <CardDescription className="text-xs">
                        {rules?.length ?? 0} regole. Valutazione ogni 5 minuti dal cron worker.
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    {isLoading ? (
                        <div className="py-8 text-center text-sm text-muted-foreground">
                            <Loader2 className="inline animate-spin mr-2" />Caricamento…
                        </div>
                    ) : (rules?.length ?? 0) === 0 ? (
                        <div className="py-12 text-center border border-dashed rounded-md">
                            <Bell size={28} className="mx-auto opacity-30 mb-2" />
                            <p className="text-sm text-muted-foreground">Nessuna regola alert configurata.</p>
                            <p className="text-xs text-muted-foreground mt-1">
                                Click "Nuova regola" per ricevere notifiche quando l'OEE scende sotto target.
                            </p>
                        </div>
                    ) : (
                        <div className="space-y-2">
                            {rules!.map((r) => (
                                <RuleRow
                                    key={r.id}
                                    r={r}
                                    profiles={profiles}
                                    onEdit={() => { setEditing(r); setEditorOpen(true); }}
                                    onDelete={() => handleDelete(r)}
                                    onToggle={() => handleToggle(r)}
                                />
                            ))}
                        </div>
                    )}
                </CardContent>
            </Card>

            <AlertRuleEditor
                open={editorOpen}
                onClose={() => setEditorOpen(false)}
                initial={editing}
                profiles={profiles}
                onSaved={() => queryClient.invalidateQueries({ queryKey: ['oee-alert-rules'] })}
            />
        </div>
    );
};

const RuleRow = ({
    r, profiles, onEdit, onDelete, onToggle,
}: { r: OEEAlertRule; profiles: OEEProfile[]; onEdit: () => void; onDelete: () => void; onToggle: () => void }) => {
    const profileLabel = r.profile_id
        ? profiles.find((p) => p.id === r.profile_id)?.name ?? `Profilo ${r.profile_id}`
        : 'Overall (rollup)';
    const violating = r.last_state === 'violating' && r.enabled;
    return (
        <div className={`flex items-center gap-3 p-3 border rounded-md hover:bg-muted/30 transition-colors ${!r.enabled ? 'opacity-50' : ''}`}>
            <div className={`p-2 rounded-md ${violating ? 'bg-red-500/10' : 'bg-emerald-500/10'}`}>
                {violating ? <AlertCircle size={16} className="text-red-500" /> : <CheckCircle2 size={16} className="text-emerald-500" />}
            </div>
            <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-semibold">{r.name}</span>
                    <Badge className={`text-[10px] ${SEVERITY_BADGE[r.severity]} border-none`}>{r.severity}</Badge>
                    {violating && (
                        <Badge variant="outline" className="text-[10px] border-red-500/40 text-red-500">in violazione</Badge>
                    )}
                    {!r.enabled && (
                        <Badge variant="outline" className="text-[10px]">disabilitata</Badge>
                    )}
                </div>
                <p className="text-xs text-muted-foreground mt-0.5">
                    Su <strong>{profileLabel}</strong>: {METRIC_LABELS[r.metric]} {r.op} {r.threshold}%
                    {' '}per {r.sustained_minutes} min
                </p>
                {r.last_notified_at && (
                    <p className="text-[10px] text-muted-foreground">
                        Ultima notifica: {new Date(r.last_notified_at).toLocaleString('it-IT')}
                    </p>
                )}
            </div>
            <div className="flex items-center gap-1">
                <Button variant="ghost" size="icon" onClick={onToggle} title={r.enabled ? 'Disabilita' : 'Abilita'}>
                    {r.enabled ? <Power size={14} /> : <PowerOff size={14} />}
                </Button>
                <Button variant="ghost" size="icon" onClick={onEdit}><Pencil size={14} /></Button>
                <Button variant="ghost" size="icon" onClick={onDelete} className="text-red-500 hover:text-red-600">
                    <Trash2 size={14} />
                </Button>
            </div>
        </div>
    );
};

const ruleToRequest = (r: OEEAlertRule, overrides?: Partial<OEEAlertRuleRequest>): OEEAlertRuleRequest => ({
    profile_id: r.profile_id ?? null,
    name: r.name,
    metric: r.metric,
    op: r.op,
    threshold: r.threshold,
    sustained_minutes: r.sustained_minutes,
    severity: r.severity,
    enabled: r.enabled,
    ...overrides,
});

const AlertRuleEditor = ({
    open, onClose, initial, profiles, onSaved,
}: {
    open: boolean;
    onClose: () => void;
    initial: OEEAlertRule | null;
    profiles: OEEProfile[];
    onSaved: () => void;
}) => {
    const [name, setName]       = useState('');
    const [profileId, setProfileId] = useState<number | null>(null);
    const [metric, setMetric]   = useState<'oee' | 'availability' | 'performance' | 'quality'>('oee');
    const [op, setOp]           = useState<'<' | '>'>('<');
    const [threshold, setThreshold] = useState(70);
    const [sustained, setSustained] = useState(60);
    const [severity, setSeverity] = useState<'info' | 'warning' | 'critical'>('warning');
    const [enabled, setEnabled] = useState(true);
    const [saving, setSaving]   = useState(false);

    useEffect(() => {
        if (!open) return;
        if (initial) {
            setName(initial.name);
            setProfileId(initial.profile_id ?? null);
            setMetric(initial.metric);
            setOp(initial.op);
            setThreshold(initial.threshold);
            setSustained(initial.sustained_minutes);
            setSeverity(initial.severity);
            setEnabled(initial.enabled);
        } else {
            setName(''); setProfileId(null); setMetric('oee'); setOp('<');
            setThreshold(70); setSustained(60); setSeverity('warning'); setEnabled(true);
        }
    }, [open, initial]);

    const handleSave = async () => {
        if (!name.trim()) {
            toast.error('Il nome è obbligatorio.');
            return;
        }
        setSaving(true);
        try {
            const payload: OEEAlertRuleRequest = {
                profile_id: profileId,
                name: name.trim(),
                metric, op, threshold,
                sustained_minutes: Math.max(60, sustained),
                severity, enabled,
            };
            if (initial) {
                await oeeApi.updateAlertRule(initial.id, payload);
                toast.success('Regola aggiornata.');
            } else {
                await oeeApi.createAlertRule(payload);
                toast.success('Regola creata.');
            }
            onSaved();
            onClose();
        } catch (e: unknown) {
            toast.error(`Errore: ${(e as Error)?.message ?? 'unknown'}`);
        } finally {
            setSaving(false);
        }
    };

    return (
        <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>{initial ? 'Modifica regola alert' : 'Nuova regola alert'}</DialogTitle>
                    <DialogDescription>
                        Notifica quando una metrica supera/scende sotto una soglia per N minuti consecutivi.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-3 py-2">
                    <div className="space-y-1">
                        <Label>Nome</Label>
                        <Input
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            placeholder="es. OEE Linea A sotto target"
                        />
                    </div>

                    <div className="space-y-1">
                        <Label>Profilo</Label>
                        <Select
                            value={profileId === null ? 'rollup' : String(profileId)}
                            onValueChange={(v) => setProfileId(v === 'rollup' ? null : parseInt(v, 10))}
                        >
                            <SelectTrigger><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value="rollup">OEE Overall (rollup di tutti)</SelectItem>
                                {profiles.map((p) => (
                                    <SelectItem key={p.id} value={String(p.id)}>{p.name}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="grid grid-cols-3 gap-3">
                        <div className="space-y-1">
                            <Label>Metrica</Label>
                            <Select value={metric} onValueChange={(v) => setMetric(v as typeof metric)}>
                                <SelectTrigger><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="oee">OEE</SelectItem>
                                    <SelectItem value="availability">Availability</SelectItem>
                                    <SelectItem value="performance">Performance</SelectItem>
                                    <SelectItem value="quality">Quality</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-1">
                            <Label>Condizione</Label>
                            <Select value={op} onValueChange={(v) => setOp(v as '<' | '>')}>
                                <SelectTrigger><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="<">&lt; (sotto)</SelectItem>
                                    <SelectItem value=">">&gt; (sopra)</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-1">
                            <Label>Soglia (%)</Label>
                            <Input
                                type="number" min={0} max={100} step={1}
                                value={threshold}
                                onChange={(e) => setThreshold(parseFloat(e.target.value) || 0)}
                            />
                        </div>
                    </div>

                    <div className="grid grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <Label>Durata minima (min)</Label>
                            <Input
                                type="number" min={60} step={60}
                                value={sustained}
                                onChange={(e) => setSustained(parseInt(e.target.value) || 60)}
                            />
                            <p className="text-[10px] text-muted-foreground">
                                Minimo 60 min (granularità oee_history). Multipli di 60 consigliati.
                            </p>
                        </div>
                        <div className="space-y-1">
                            <Label>Severity</Label>
                            <Select value={severity} onValueChange={(v) => setSeverity(v as typeof severity)}>
                                <SelectTrigger><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="info">Info — solo log</SelectItem>
                                    <SelectItem value="warning">Warning — email/Telegram</SelectItem>
                                    <SelectItem value="critical">Critical — escalation</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    <div className="flex items-center gap-2 pt-2 border-t">
                        <Switch checked={enabled} onCheckedChange={setEnabled} />
                        <Label className="cursor-pointer">Attiva</Label>
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={onClose} disabled={saving}>Annulla</Button>
                    <Button onClick={handleSave} disabled={saving}>
                        {saving && <Loader2 size={14} className="mr-1 animate-spin" />}
                        Salva
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
};
