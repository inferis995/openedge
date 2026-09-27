import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
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
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';

// OEEAlertRules — gestione delle regole "OEE Linea A < 70% per 60min → email".
// Valutate dal cron worker ogni 5 minuti su oee_history.
//
// UX: lista compatta + dialog editor. Stato live: badge "violating" sulle
// regole attualmente scattate (last_state='violating').

// i18n keys, resolved with t() at render.
const METRIC_LABELS: Record<string, string> = {
    oee:          'oee.metric_oee',
    availability: 'oee.availability',
    performance:  'oee.performance',
    quality:      'oee.quality',
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
    const { t } = useTranslation();
    const [editorOpen, setEditorOpen] = useState(false);
    const [editing, setEditing] = useState<OEEAlertRule | null>(null);
    const queryClient = useQueryClient();

    const { data: rules, isLoading } = useQuery({
        queryKey: ['oee-alert-rules'],
        queryFn: oeeApi.listAlertRules,
    });

    const handleDelete = async (r: OEEAlertRule) => {
        if (!(await confirmAction({ title: i18n.t('ask.delete_named', { name: r.name }), destructive: true }))) return;
        try {
            await oeeApi.deleteAlertRule(r.id);
            toast.success(t('oee.rule_deleted'));
            queryClient.invalidateQueries({ queryKey: ['oee-alert-rules'] });
        } catch (e: unknown) {
            toast.error(t('oee.error_with_msg', { msg: (e as Error)?.message ?? t('oee.unknown_error') }));
        }
    };

    const handleToggle = async (r: OEEAlertRule) => {
        try {
            await oeeApi.updateAlertRule(r.id, ruleToRequest(r, { enabled: !r.enabled }));
            queryClient.invalidateQueries({ queryKey: ['oee-alert-rules'] });
        } catch (e: unknown) {
            toast.error(t('oee.error_with_msg', { msg: (e as Error)?.message ?? t('oee.unknown_error') }));
        }
    };

    return (
        <div className="space-y-3">
            <div className="flex items-start justify-between gap-2">
                <div>
                    <p className="text-sm text-muted-foreground">
                        {t('oee.rules_intro')}
                    </p>
                </div>
                <Button onClick={() => { setEditing(null); setEditorOpen(true); }} size="sm">
                    <Plus size={14} className="mr-1" /> {t('oee.new_rule')}
                </Button>
            </div>

            <Card>
                <CardHeader className="pb-3">
                    <CardTitle className="text-base flex items-center gap-2">
                        <Bell size={16} /> {t('oee.rules_title')}
                    </CardTitle>
                    <CardDescription className="text-xs">
                        {t('oee.rules_count', { count: rules?.length ?? 0 })}
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    {isLoading ? (
                        <div className="py-8 text-center text-sm text-muted-foreground">
                            <Loader2 className="inline animate-spin mr-2" />{t('common.loading')}
                        </div>
                    ) : (rules?.length ?? 0) === 0 ? (
                        <div className="py-12 text-center border border-dashed rounded-md">
                            <Bell size={28} className="mx-auto opacity-30 mb-2" />
                            <p className="text-sm text-muted-foreground">{t('oee.rules_empty')}</p>
                            <p className="text-xs text-muted-foreground mt-1">
                                {t('oee.rules_empty_hint')}
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
    const { t, i18n: i18nInst } = useTranslation();
    const profileLabel = r.profile_id
        ? profiles.find((p) => p.id === r.profile_id)?.name ?? t('oee.profile_n', { id: r.profile_id })
        : t('oee.overall_rollup');
    const violating = r.last_state === 'violating' && r.enabled;
    return (
        <div className={`flex items-center gap-3 p-3 border rounded-md hover:bg-muted/30 transition-colors ${!r.enabled ? 'opacity-50' : ''}`}>
            <div className={`p-2 rounded-md ${violating ? 'bg-red-500/10' : 'bg-emerald-500/10'}`}>
                {violating ? <AlertCircle size={16} className="text-red-500" /> : <CheckCircle2 size={16} className="text-emerald-500" />}
            </div>
            <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-semibold">{r.name}</span>
                    <Badge className={`text-[10px] ${SEVERITY_BADGE[r.severity]} border-none`}>{t(`oee.severity_${r.severity}`, { defaultValue: r.severity })}</Badge>
                    {violating && (
                        <Badge variant="outline" className="text-[10px] border-red-500/40 text-red-500">{t('oee.violating')}</Badge>
                    )}
                    {!r.enabled && (
                        <Badge variant="outline" className="text-[10px]">{t('oee.disabled')}</Badge>
                    )}
                </div>
                <p className="text-xs text-muted-foreground mt-0.5">
                    {t('oee.rule_on')} <strong>{profileLabel}</strong>: {METRIC_LABELS[r.metric] ? t(METRIC_LABELS[r.metric]) : r.metric} {r.op} {r.threshold}%
                    {' '}{t('oee.rule_for_minutes', { count: r.sustained_minutes })}
                </p>
                {r.last_notified_at && (
                    <p className="text-[10px] text-muted-foreground">
                        {t('oee.last_notified', { when: new Date(r.last_notified_at).toLocaleString(i18nInst.language) })}
                    </p>
                )}
            </div>
            <div className="flex items-center gap-1">
                <Button variant="ghost" size="icon" onClick={onToggle} title={r.enabled ? t('oee.disable') : t('oee.enable')}>
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
    const { t } = useTranslation();
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
            toast.error(t('oee.name_required'));
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
                toast.success(t('oee.rule_updated'));
            } else {
                await oeeApi.createAlertRule(payload);
                toast.success(t('oee.rule_created'));
            }
            onSaved();
            onClose();
        } catch (e: unknown) {
            toast.error(t('oee.error_with_msg', { msg: (e as Error)?.message ?? t('oee.unknown_error') }));
        } finally {
            setSaving(false);
        }
    };

    return (
        <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>{initial ? t('oee.edit_rule_title') : t('oee.new_rule_title')}</DialogTitle>
                    <DialogDescription>
                        {t('oee.rule_dialog_desc')}
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-3 py-2">
                    <div className="space-y-1">
                        <Label>{t('common.name')}</Label>
                        <Input
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            placeholder={t('oee.rule_name_placeholder')}
                        />
                    </div>

                    <div className="space-y-1">
                        <Label>{t('oee.profile')}</Label>
                        <Select
                            value={profileId === null ? 'rollup' : String(profileId)}
                            onValueChange={(v) => setProfileId(v === 'rollup' ? null : parseInt(v, 10))}
                        >
                            <SelectTrigger><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value="rollup">{t('oee.overall_rollup_all')}</SelectItem>
                                {profiles.map((p) => (
                                    <SelectItem key={p.id} value={String(p.id)}>{p.name}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                        <div className="space-y-1">
                            <Label>{t('oee.metric')}</Label>
                            <Select value={metric} onValueChange={(v) => setMetric(v as typeof metric)}>
                                <SelectTrigger><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="oee">OEE</SelectItem>
                                    <SelectItem value="availability">{t('oee.availability')}</SelectItem>
                                    <SelectItem value="performance">{t('oee.performance')}</SelectItem>
                                    <SelectItem value="quality">{t('oee.quality')}</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-1">
                            <Label>{t('oee.condition')}</Label>
                            <Select value={op} onValueChange={(v) => setOp(v as '<' | '>')}>
                                <SelectTrigger><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="<">&lt; ({t('oee.below')})</SelectItem>
                                    <SelectItem value=">">&gt; ({t('oee.above')})</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="space-y-1">
                            <Label>{t('oee.threshold_pct')}</Label>
                            <Input
                                type="number" min={0} max={100} step={1}
                                value={threshold}
                                onChange={(e) => setThreshold(parseFloat(e.target.value) || 0)}
                            />
                        </div>
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div className="space-y-1">
                            <Label>{t('oee.min_duration')}</Label>
                            <Input
                                type="number" min={60} step={60}
                                value={sustained}
                                onChange={(e) => setSustained(parseInt(e.target.value) || 60)}
                            />
                            <p className="text-[10px] text-muted-foreground">
                                {t('oee.min_duration_hint')}
                            </p>
                        </div>
                        <div className="space-y-1">
                            <Label>{t('oee.severity')}</Label>
                            <Select value={severity} onValueChange={(v) => setSeverity(v as typeof severity)}>
                                <SelectTrigger><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="info">{t('oee.severity_info_desc')}</SelectItem>
                                    <SelectItem value="warning">{t('oee.severity_warning_desc')}</SelectItem>
                                    <SelectItem value="critical">{t('oee.severity_critical_desc')}</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    <div className="flex items-center gap-2 pt-2 border-t">
                        <Switch checked={enabled} onCheckedChange={setEnabled} />
                        <Label className="cursor-pointer">{t('oee.active')}</Label>
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={onClose} disabled={saving}>{t('common.cancel')}</Button>
                    <Button onClick={handleSave} disabled={saving}>
                        {saving && <Loader2 size={14} className="mr-1 animate-spin" />}
                        {t('common.save')}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
};
