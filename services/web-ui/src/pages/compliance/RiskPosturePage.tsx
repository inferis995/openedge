import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { AlertTriangle, TrendingUp, ShieldAlert, Zap, Loader2 } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter
} from '@/components/ui/dialog';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue
} from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { complianceApi, ComplianceRequirement } from '@/api/compliance';

function ScoreColor(score: number): string {
    if (score >= 80) return 'text-green-500';
    if (score >= 50) return 'text-yellow-500';
    return 'text-red-500';
}

function RiskScoreColor(score: number): string {
    if (score >= 7) return 'text-red-500';
    if (score >= 4) return 'text-yellow-500';
    return 'text-green-500';
}

function ProgressBar({ value, max = 100 }: { value: number; max?: number }) {
    const pct = Math.min(100, Math.round((value / max) * 100));
    const color = pct >= 80 ? 'bg-green-500' : pct >= 50 ? 'bg-yellow-500' : 'bg-red-500';
    return (
        <div className="w-full h-2 bg-muted rounded-full overflow-hidden">
            <div className={`h-2 rounded-full transition-all ${color}`} style={{ width: `${pct}%` }} />
        </div>
    );
}

function StatusBadge({ status }: { status: ComplianceRequirement['status'] }) {
    const map: Record<string, { label: string; variant: 'success' | 'warning' | 'destructive' | 'secondary' }> = {
        compliant: { label: 'Conforme', variant: 'success' },
        partial: { label: 'Parziale', variant: 'warning' },
        non_compliant: { label: 'Non Conforme', variant: 'destructive' },
        not_assessed: { label: 'Non Valutato', variant: 'secondary' },
    };
    const cfg = map[status] ?? { label: status, variant: 'secondary' };
    return <Badge variant={cfg.variant}>{cfg.label}</Badge>;
}

interface UpdateDialogProps {
    open: boolean;
    onClose: () => void;
    framework: string;
    requirement: ComplianceRequirement | null;
}

function UpdateDialog({ open, onClose, framework, requirement }: UpdateDialogProps) {
    const qc = useQueryClient();
    const [form, setForm] = useState<{ status: string; evidence: string; notes: string }>({
        status: requirement?.status ?? 'not_assessed',
        evidence: requirement?.evidence ?? '',
        notes: requirement?.notes ?? '',
    });

    const update = useMutation({
        mutationFn: () => complianceApi.updateAssessment(framework, requirement!.id, form),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['nis2-assessment'] });
            qc.invalidateQueries({ queryKey: ['iec62443-assessment'] });
            qc.invalidateQueries({ queryKey: ['compliance-score'] });
            toast.success('Valutazione aggiornata');
            onClose();
        },
        onError: () => toast.error('Errore aggiornamento'),
    });

    if (!requirement) return null;

    return (
        <Dialog open={open} onOpenChange={onClose}>
            <DialogContent className="max-w-md">
                <DialogHeader>
                    <DialogTitle>
                        <span className="font-mono text-sm bg-muted px-2 py-0.5 mr-2">{requirement.req_code}</span>
                        {requirement.title}
                    </DialogTitle>
                </DialogHeader>
                <div className="grid gap-4 py-2">
                    <div className="grid gap-2">
                        <Label>Stato di Conformità</Label>
                        <Select value={form.status} onValueChange={v => setForm(p => ({ ...p, status: v }))}>
                            <SelectTrigger><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value="compliant">Conforme</SelectItem>
                                <SelectItem value="partial">Parziale</SelectItem>
                                <SelectItem value="non_compliant">Non Conforme</SelectItem>
                                <SelectItem value="not_assessed">Non Valutato</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>
                    <div className="grid gap-2">
                        <Label>Evidenza</Label>
                        <textarea
                            className="flex min-h-[80px] w-full rounded-none clip-chamfer-sm border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 resize-none"
                            value={form.evidence}
                            onChange={e => setForm(p => ({ ...p, evidence: e.target.value }))}
                            placeholder="Documentazione, link, prove di conformità..."
                        />
                    </div>
                    <div className="grid gap-2">
                        <Label>Note</Label>
                        <textarea
                            className="flex min-h-[60px] w-full rounded-none clip-chamfer-sm border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 resize-none"
                            value={form.notes}
                            onChange={e => setForm(p => ({ ...p, notes: e.target.value }))}
                            placeholder="Note interne..."
                        />
                    </div>
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={onClose}>Annulla</Button>
                    <Button onClick={() => update.mutate()} disabled={update.isPending}>Salva</Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function RequirementChecklist({
    requirements,
    framework,
}: {
    requirements: ComplianceRequirement[];
    framework: string;
}) {
    const [selectedReq, setSelectedReq] = useState<ComplianceRequirement | null>(null);
    const [dialogOpen, setDialogOpen] = useState(false);

    return (
        <>
            <div className="space-y-2 mt-4">
                {requirements.map(req => (
                    <div
                        key={req.id}
                        className="flex items-center gap-3 p-3 border rounded-none clip-chamfer-sm bg-card hover:bg-muted/30 transition-colors"
                    >
                        <span className="font-mono text-xs bg-muted px-1.5 py-0.5 whitespace-nowrap">{req.req_code}</span>
                        <span className="text-sm flex-1 truncate flex items-center gap-1.5" title={req.title}>
                            {req.title}
                            {req.evidence && req.evidence.includes('[auto]') && (
                                <Zap size={11} className="text-yellow-400 shrink-0" />
                            )}
                        </span>
                        <StatusBadge status={req.status} />
                        <Button
                            variant="outline"
                            size="sm"
                            className="h-7 text-xs shrink-0"
                            onClick={() => {
                                setSelectedReq(req);
                                setDialogOpen(true);
                            }}
                        >
                            Aggiorna
                        </Button>
                    </div>
                ))}
            </div>
            <UpdateDialog
                open={dialogOpen}
                onClose={() => setDialogOpen(false)}
                framework={framework}
                requirement={selectedReq}
            />
        </>
    );
}

export default function RiskPosturePage() {
    const qc = useQueryClient();

    const { data: posture } = useQuery({
        queryKey: ['risk-posture'],
        queryFn: () => complianceApi.getRiskPosture(),
    });
    const { data: score } = useQuery({
        queryKey: ['compliance-score'],
        queryFn: () => complianceApi.getScore(),
    });
    const { data: nis2 = [] } = useQuery({
        queryKey: ['nis2-assessment'],
        queryFn: () => complianceApi.getAssessment('NIS2'),
    });
    const { data: iec62443 = [] } = useQuery({
        queryKey: ['iec62443-assessment'],
        queryFn: () => complianceApi.getAssessment('IEC62443'),
    });

    const autoAssess = useMutation({
        mutationFn: () => complianceApi.autoAssess(),
        onSuccess: (results) => {
            qc.invalidateQueries({ queryKey: ['nis2-assessment'] });
            qc.invalidateQueries({ queryKey: ['iec62443-assessment'] });
            qc.invalidateQueries({ queryKey: ['compliance-score'] });
            toast.success(`Valutazione automatica completata: ${results.length} requisiti aggiornati`);
        },
        onError: () => toast.error('Errore durante la valutazione automatica'),
    });

    const avgScore = posture?.avg_risk_score ?? 0;
    const nis2Score = score?.nis2.score ?? 0;
    const iec62443Score = score?.iec62443.score ?? 0;
    const overallScore = score?.overall ?? 0;

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex items-start justify-between gap-4">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Postura di Rischio</h2>
                    <p className="text-muted-foreground">Valutazione baseline e conformità NIS2 / IEC 62443</p>
                </div>
                <Button
                    variant="outline"
                    className="gap-2 shrink-0"
                    onClick={() => autoAssess.mutate()}
                    disabled={autoAssess.isPending}
                >
                    {autoAssess.isPending
                        ? <Loader2 size={16} className="animate-spin" />
                        : <Zap size={16} className="text-yellow-400" />
                    }
                    Auto-valuta
                </Button>
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {/* Left Column — Risk Overview */}
                <div className="space-y-4">
                    {/* Risk Posture Card */}
                    <Card>
                        <CardHeader className="pb-2">
                            <CardTitle className="text-base flex items-center gap-2">
                                <ShieldAlert size={18} /> Postura di Rischio OT
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="space-y-4">
                            <div className="flex items-center gap-4">
                                <div className={`text-6xl font-black ${RiskScoreColor(avgScore)}`}>
                                    {avgScore.toFixed(1)}
                                </div>
                                <div className="flex-1 space-y-1">
                                    <p className="text-xs text-muted-foreground uppercase tracking-wider">Score medio di rischio</p>
                                    <ProgressBar value={avgScore} max={10} />
                                    <div className="grid grid-cols-2 gap-2 mt-2 text-xs">
                                        <div>
                                            <span className="text-muted-foreground">Asset totali: </span>
                                            <span className="font-bold">{posture?.total_assets ?? 0}</span>
                                        </div>
                                        <div>
                                            <span className="text-muted-foreground">Non autorizzati: </span>
                                            <span className="font-bold text-yellow-500">{posture?.unauthorized_devices ?? 0}</span>
                                        </div>
                                        <div>
                                            <span className="text-muted-foreground">CVE critiche: </span>
                                            <span className="font-bold text-red-500">{posture?.critical_cves ?? 0}</span>
                                        </div>
                                        <div>
                                            <span className="text-muted-foreground">CVE alte: </span>
                                            <span className="font-bold text-orange-500">{posture?.high_cves ?? 0}</span>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        </CardContent>
                    </Card>

                    {/* By Type */}
                    <Card>
                        <CardHeader className="pb-2">
                            <CardTitle className="text-base">Per Tipo di Dispositivo</CardTitle>
                        </CardHeader>
                        <CardContent>
                            <div className="space-y-2">
                                {(posture?.by_type ?? []).length === 0 ? (
                                    <p className="text-sm text-muted-foreground">Nessun dato disponibile</p>
                                ) : (
                                    posture!.by_type.map(row => (
                                        <div key={row.device_type} className="flex items-center gap-3">
                                            <span className="w-24 text-sm font-medium">{row.device_type}</span>
                                            <span className="text-xs text-muted-foreground w-8">{row.count}x</span>
                                            <div className="flex-1">
                                                <ProgressBar value={row.avg_score} max={10} />
                                            </div>
                                            <span className={`text-sm font-bold w-8 text-right ${RiskScoreColor(row.avg_score)}`}>
                                                {row.avg_score.toFixed(1)}
                                            </span>
                                        </div>
                                    ))
                                )}
                            </div>
                        </CardContent>
                    </Card>

                    {/* Top 5 Risky */}
                    <Card>
                        <CardHeader className="pb-2">
                            <CardTitle className="text-base flex items-center gap-2">
                                <AlertTriangle size={16} className="text-red-500" /> Top 5 Dispositivi a Rischio
                            </CardTitle>
                        </CardHeader>
                        <CardContent>
                            <div className="space-y-2">
                                {(posture?.top_risky ?? []).slice(0, 5).map(asset => (
                                    <div key={asset.id} className="flex items-center gap-3 p-2 border rounded-none clip-chamfer-sm">
                                        <div className={`text-xl font-black w-10 text-center ${RiskScoreColor(asset.risk_score)}`}>
                                            {asset.risk_score.toFixed(0)}
                                        </div>
                                        <div className="flex-1 min-w-0">
                                            <p className="font-mono text-xs truncate">{asset.ip_address}</p>
                                            <p className="text-xs text-muted-foreground truncate">{asset.vendor} {asset.model}</p>
                                        </div>
                                        <Badge variant="outline" className="text-xs shrink-0">{asset.device_type}</Badge>
                                    </div>
                                ))}
                                {(posture?.top_risky ?? []).length === 0 && (
                                    <p className="text-sm text-muted-foreground">Nessun asset ad alto rischio</p>
                                )}
                            </div>
                        </CardContent>
                    </Card>
                </div>

                {/* Right Column — Compliance Score */}
                <div className="space-y-4">
                    <Card>
                        <CardHeader className="pb-2">
                            <CardTitle className="text-base flex items-center gap-2">
                                <TrendingUp size={18} /> Score Conformità Framework
                            </CardTitle>
                        </CardHeader>
                        <CardContent>
                            <Tabs defaultValue="nis2">
                                <TabsList className="w-full">
                                    <TabsTrigger value="nis2" className="flex-1">NIS2 Art.21</TabsTrigger>
                                    <TabsTrigger value="iec62443" className="flex-1">IEC 62443</TabsTrigger>
                                </TabsList>

                                <TabsContent value="nis2" className="space-y-4 mt-4">
                                    <div className="flex items-center gap-4">
                                        <div className={`text-5xl font-black ${ScoreColor(nis2Score)}`}>
                                            {Math.round(nis2Score)}%
                                        </div>
                                        <div className="flex-1">
                                            <ProgressBar value={nis2Score} />
                                        </div>
                                    </div>
                                    {score?.nis2 && (
                                        <div className="grid grid-cols-4 gap-2 text-center">
                                            <div className="p-2 bg-green-500/10 rounded">
                                                <p className="text-lg font-bold text-green-500">{score.nis2.compliant}</p>
                                                <p className="text-xs text-muted-foreground">Conformi</p>
                                            </div>
                                            <div className="p-2 bg-yellow-500/10 rounded">
                                                <p className="text-lg font-bold text-yellow-500">{score.nis2.partial}</p>
                                                <p className="text-xs text-muted-foreground">Parziali</p>
                                            </div>
                                            <div className="p-2 bg-red-500/10 rounded">
                                                <p className="text-lg font-bold text-red-500">{score.nis2.non_compliant}</p>
                                                <p className="text-xs text-muted-foreground">Non Conformi</p>
                                            </div>
                                            <div className="p-2 bg-muted rounded">
                                                <p className="text-lg font-bold text-muted-foreground">{score.nis2.not_assessed}</p>
                                                <p className="text-xs text-muted-foreground">Non Valutati</p>
                                            </div>
                                        </div>
                                    )}
                                    <RequirementChecklist requirements={nis2} framework="NIS2" />
                                </TabsContent>

                                <TabsContent value="iec62443" className="space-y-4 mt-4">
                                    <div className="flex items-center gap-4">
                                        <div className={`text-5xl font-black ${ScoreColor(iec62443Score)}`}>
                                            {Math.round(iec62443Score)}%
                                        </div>
                                        <div className="flex-1">
                                            <ProgressBar value={iec62443Score} />
                                        </div>
                                    </div>
                                    {score?.iec62443 && (
                                        <div className="grid grid-cols-4 gap-2 text-center">
                                            <div className="p-2 bg-green-500/10 rounded">
                                                <p className="text-lg font-bold text-green-500">{score.iec62443.compliant}</p>
                                                <p className="text-xs text-muted-foreground">Conformi</p>
                                            </div>
                                            <div className="p-2 bg-yellow-500/10 rounded">
                                                <p className="text-lg font-bold text-yellow-500">{score.iec62443.partial}</p>
                                                <p className="text-xs text-muted-foreground">Parziali</p>
                                            </div>
                                            <div className="p-2 bg-red-500/10 rounded">
                                                <p className="text-lg font-bold text-red-500">{score.iec62443.non_compliant}</p>
                                                <p className="text-xs text-muted-foreground">Non Conformi</p>
                                            </div>
                                            <div className="p-2 bg-muted rounded">
                                                <p className="text-lg font-bold text-muted-foreground">{score.iec62443.not_assessed}</p>
                                                <p className="text-xs text-muted-foreground">Non Valutati</p>
                                            </div>
                                        </div>
                                    )}
                                    <RequirementChecklist requirements={iec62443} framework="IEC62443" />
                                </TabsContent>
                            </Tabs>
                        </CardContent>
                    </Card>

                    {/* Overall Score */}
                    <Card>
                        <CardContent className="pt-6">
                            <div className="flex items-center justify-center gap-6">
                                <div className="text-center">
                                    <p className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Score Complessivo</p>
                                    <div className={`text-7xl font-black ${ScoreColor(overallScore)}`}>
                                        {Math.round(overallScore)}
                                        <span className="text-2xl">%</span>
                                    </div>
                                    <ProgressBar value={overallScore} />
                                    <p className="text-xs text-muted-foreground mt-2">
                                        {overallScore >= 80 ? 'Conforme' : overallScore >= 50 ? 'Parzialmente Conforme' : 'Non Conforme'}
                                    </p>
                                </div>
                            </div>
                        </CardContent>
                    </Card>
                </div>
            </div>
        </div>
    );
}
