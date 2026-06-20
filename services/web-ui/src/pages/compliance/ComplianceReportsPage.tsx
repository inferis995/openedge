import { useState, useEffect, useRef } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { FileText, Download, Trash2, Loader2, CheckCircle, XCircle } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
    Select, SelectContent, SelectItem, SelectTrigger, SelectValue
} from '@/components/ui/select';
import { complianceApi, ComplianceReport, GenerateReportRequest } from '@/api/compliance';

const REPORT_TYPE_LABELS: Record<ComplianceReport['report_type'], string> = {
    nis2_assessment: 'NIS2 Art.21',
    iec62443_assessment: 'IEC 62443',
    asset_inventory: 'Inventario Asset',
    incident_timeline: 'Timeline Incidenti',
    full_compliance: 'Compliance Completa',
};

const REPORT_TYPE_VARIANTS: Record<ComplianceReport['report_type'], string> = {
    nis2_assessment: 'default',
    iec62443_assessment: 'secondary',
    asset_inventory: 'outline',
    incident_timeline: 'warning',
    full_compliance: 'success',
};

function formatDate(s: string) {
    if (!s) return '—';
    try { return new Date(s).toLocaleString('it-IT'); } catch { return s; }
}

function StatusBadge({ status }: { status: ComplianceReport['status'] }) {
    if (status === 'generating') {
        return (
            <Badge variant="secondary" className="gap-1">
                <Loader2 size={10} className="animate-spin" /> Generazione...
            </Badge>
        );
    }
    if (status === 'ready') {
        return <Badge variant="success" className="gap-1"><CheckCircle size={10} /> Pronto</Badge>;
    }
    return <Badge variant="destructive" className="gap-1"><XCircle size={10} /> Errore</Badge>;
}

function downloadReport(report: ComplianceReport) {
    const blob = new Blob([JSON.stringify(report.content, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `openedge-${report.report_type}-${new Date(report.generated_at).toISOString().split('T')[0]}.json`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
}

export default function ComplianceReportsPage() {
    const qc = useQueryClient();
    const [reportType, setReportType] = useState<ComplianceReport['report_type']>('full_compliance');
    const [periodFrom, setPeriodFrom] = useState('');
    const [periodTo, setPeriodTo] = useState('');
    const [pollingIds, setPollingIds] = useState<Set<number>>(new Set());
    const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

    const { data: reports = [] } = useQuery({
        queryKey: ['compliance-reports'],
        queryFn: () => complianceApi.listReports(),
        refetchInterval: 60000,
    });

    const generate = useMutation({
        mutationFn: (data: GenerateReportRequest) => complianceApi.generateReport(data),
        onSuccess: (res) => {
            qc.invalidateQueries({ queryKey: ['compliance-reports'] });
            setPollingIds(prev => new Set([...prev, res.id]));
            toast.info('Report in generazione...');
        },
        onError: () => toast.error('Errore durante la generazione del report'),
    });

    const deleteReport = useMutation({
        mutationFn: (id: number) => complianceApi.deleteReport(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['compliance-reports'] });
            toast.success('Report eliminato');
        },
        onError: () => toast.error('Errore durante l\'eliminazione'),
    });

    // Poll for reports that are still generating
    useEffect(() => {
        if (pollingIds.size === 0) return;

        pollRef.current = setInterval(async () => {
            const completed = new Set<number>();
            for (const id of pollingIds) {
                try {
                    const r = await complianceApi.getReport(id);
                    if (r.status !== 'generating') {
                        completed.add(id);
                        if (r.status === 'ready') {
                            toast.success(`Report "${REPORT_TYPE_LABELS[r.report_type]}" pronto!`);
                        } else {
                            toast.error(`Report fallito: ${r.error ?? 'Errore sconosciuto'}`);
                        }
                        qc.invalidateQueries({ queryKey: ['compliance-reports'] });
                    }
                } catch {
                    completed.add(id);
                }
            }
            if (completed.size > 0) {
                setPollingIds(prev => {
                    const next = new Set(prev);
                    completed.forEach(id => next.delete(id));
                    return next;
                });
            }
        }, 3000);

        return () => { if (pollRef.current) clearInterval(pollRef.current); };
    }, [pollingIds, qc]);

    const handleGenerate = () => {
        const payload: GenerateReportRequest = {
            report_type: reportType,
            title: REPORT_TYPE_LABELS[reportType],
            period_from: periodFrom || null,
            period_to: periodTo || null,
        };
        generate.mutate(payload);
    };

    return (
        <div className="space-y-6">
            {/* Header */}
            <div>
                <h2 className="text-2xl font-bold tracking-tight">Report di Conformità</h2>
                <p className="text-muted-foreground">Genera documentazione audit-ready automaticamente</p>
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 items-start">
                {/* Left: Generate New Report */}
                <Card className="lg:col-span-1">
                    <CardHeader className="pb-2">
                        <CardTitle className="text-base flex items-center gap-2">
                            <FileText size={18} /> Genera Nuovo Report
                        </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <div className="grid gap-2">
                            <Label>Tipo di Report</Label>
                            <Select
                                value={reportType}
                                onValueChange={v => setReportType(v as ComplianceReport['report_type'])}
                            >
                                <SelectTrigger>
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="nis2_assessment">NIS2 Art.21 Assessment</SelectItem>
                                    <SelectItem value="iec62443_assessment">IEC 62443 Assessment</SelectItem>
                                    <SelectItem value="asset_inventory">Inventario Asset</SelectItem>
                                    <SelectItem value="incident_timeline">Timeline Incidenti</SelectItem>
                                    <SelectItem value="full_compliance">Compliance Completa</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>

                        <div className="grid gap-2">
                            <Label>Periodo Da (opzionale)</Label>
                            <Input
                                type="date"
                                value={periodFrom}
                                onChange={e => setPeriodFrom(e.target.value)}
                            />
                        </div>

                        <div className="grid gap-2">
                            <Label>Periodo A (opzionale)</Label>
                            <Input
                                type="date"
                                value={periodTo}
                                onChange={e => setPeriodTo(e.target.value)}
                            />
                        </div>

                        <Button
                            className="w-full gap-2"
                            onClick={handleGenerate}
                            disabled={generate.isPending}
                        >
                            {generate.isPending ? (
                                <><Loader2 size={16} className="animate-spin" /> Avvio...</>
                            ) : (
                                <><FileText size={16} /> Genera Report</>
                            )}
                        </Button>
                    </CardContent>
                </Card>

                {/* Right: Reports List */}
                <div className="lg:col-span-2 space-y-3">
                    <div className="flex items-center justify-between">
                        <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
                            Report Generati ({reports.length})
                        </h3>
                    </div>

                    {reports.length === 0 ? (
                        <div className="clip-chamfer border bg-card p-12 text-center text-muted-foreground">
                            <FileText size={40} className="mx-auto mb-2 opacity-30" />
                            <p>Nessun report generato. Crea il primo report.</p>
                        </div>
                    ) : (
                        reports.map(report => (
                            <div key={report.id} className="clip-chamfer border bg-card p-4 space-y-3">
                                <div className="flex items-start gap-3">
                                    <div className="flex-1 min-w-0">
                                        <div className="flex items-center gap-2 flex-wrap mb-1">
                                            <Badge
                                                variant={REPORT_TYPE_VARIANTS[report.report_type] as 'default' | 'secondary' | 'outline' | 'warning' | 'success' | 'destructive'}
                                                className="text-xs shrink-0"
                                            >
                                                {REPORT_TYPE_LABELS[report.report_type]}
                                            </Badge>
                                            <StatusBadge status={report.status} />
                                        </div>
                                        <p className="text-sm font-medium truncate">{report.title}</p>
                                        <p className="text-xs text-muted-foreground">
                                            Generato: {formatDate(report.generated_at)}
                                            {report.period_from && (
                                                <span> · Periodo: {report.period_from} → {report.period_to ?? 'ora'}</span>
                                            )}
                                        </p>
                                        {report.error && (
                                            <p className="text-xs text-destructive mt-1">{report.error}</p>
                                        )}
                                    </div>
                                    <div className="flex items-center gap-2 shrink-0">
                                        {report.status === 'ready' && report.content && (
                                            <Button
                                                variant="outline"
                                                size="sm"
                                                className="h-7 text-xs gap-1"
                                                onClick={() => downloadReport(report)}
                                            >
                                                <Download size={12} /> Scarica JSON
                                            </Button>
                                        )}
                                        <Button
                                            variant="ghost"
                                            size="icon"
                                            className="h-7 w-7 text-destructive hover:text-destructive hover:bg-destructive/10"
                                            onClick={() => {
                                                if (confirm('Eliminare questo report?')) deleteReport.mutate(report.id);
                                            }}
                                        >
                                            <Trash2 size={14} />
                                        </Button>
                                    </div>
                                </div>
                            </div>
                        ))
                    )}
                </div>
            </div>
        </div>
    );
}
