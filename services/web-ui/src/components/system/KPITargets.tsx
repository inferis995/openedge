import { useState } from 'react';
import { Target as TargetIcon, Loader2, CheckCircle2, AlertCircle } from 'lucide-react';

import { systemApi, GlobalSettings } from '@/api/system';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useTranslation } from 'react-i18next';

// Definizione dichiarativa dei target — accoppia la chiave del setting
// alla label umana mostrata + alla direzione (≤ N o ≥ N). Aggiungere
// nuovi target = aggiungere una riga qui.
const TARGETS: { key: keyof GlobalSettings; label: string; direction: 'le' | 'ge'; unit?: string }[] = [
    { key: 'kpi_target_alarms_per_day' as keyof GlobalSettings,  label: 'kpiTargets.alarms_per_day', direction: 'le', unit: 'kpiTargets.per_day_unit' },
    { key: 'kpi_target_open_critical' as keyof GlobalSettings,   label: 'kpiTargets.open_critical', direction: 'le' },
    { key: 'kpi_target_bad_quality_1h' as keyof GlobalSettings,  label: 'kpiTargets.bad_quality', direction: 'le' },
    { key: 'kpi_target_writes_24h_min' as keyof GlobalSettings,  label: 'kpiTargets.writes_min', direction: 'ge' },
    { key: 'kpi_target_recipe_loads_24h_min' as keyof GlobalSettings, label: 'kpiTargets.recipes_min', direction: 'ge' },
    { key: 'kpi_target_logins_24h_min' as keyof GlobalSettings,  label: 'kpiTargets.logins_min', direction: 'ge' },
];

interface Props {
    initial?: GlobalSettings | null;
    onSaved?: () => void;
}

type Toast = { kind: 'success' | 'error'; text: string } | null;

const KPITargets = ({ initial, onSaved }: Props) => {
    const { t: tr } = useTranslation();
    const [values, setValues] = useState<Record<string, string>>({});
    const [saving, setSaving] = useState(false);
    const [toast, setToast] = useState<Toast>(null);

    // Seed the form when initial settings arrive (or change after a save).
    // Adjusted during render rather than in an effect, so the stale form is never painted.
    const [seededFrom, setSeededFrom] = useState<{ v: Props['initial'] } | null>(null);
    if (seededFrom === null || seededFrom.v !== initial) {
        setSeededFrom({ v: initial });
        if (initial) {
            const seed: Record<string, string> = {};
            for (const t of TARGETS) {
                seed[t.key as string] = (initial as unknown as Record<string, string | undefined>)[t.key as string] ?? '';
            }
            setValues(seed);
        }
    }

    const handleSave = async () => {
        setSaving(true);
        setToast(null);
        try {
            // I target sono settings flat — sono in global_settings con il
            // prefisso "kpi_target_". Riutilizziamo il gruppo "notifications"
            // del backend? No — meglio aggiungerli al passthrough generico.
            // Per ora li scriviamo tutti via il payload top-level che
            // applyPrefixedSettings supporta (cresce poco la API surface).
            const payload: Record<string, string> = {};
            for (const t of TARGETS) {
                const v = values[t.key as string]?.trim();
                if (v !== undefined) payload[t.key as string] = v;
            }
            // PUT diretto via systemApi.updateSettings — i campi flat
            // arrivano come top-level e il backend li salva via upsertSetting
            // pattern (il code path notifications/backup non gestisce
            // kpi_target_*; ci serve un piccolo PUT su /system/kpi-targets
            // — vedi commento nel commit). Per ora prepariamo il payload e
            // chiamiamo l'endpoint dedicato.
            await systemApi.updateKPITargets(payload);
            setToast({ kind: 'success', text: tr('kpiTargets.saved') });
            onSaved?.();
        } catch (e: unknown) {
            setToast({ kind: 'error', text: tr('kpiTargets.save_failed', { reason: (e as Error)?.message ?? tr('kpiTargets.unknown_error') }) });
        } finally {
            setSaving(false);
        }
    };

    return (
        <Card className="border-border shadow-sm bg-card">
            <CardHeader className="pb-4 border-b border-border">
                <div className="flex items-center gap-3">
                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0">
                        <TargetIcon className="h-4 w-4 text-primary" />
                    </div>
                    <div>
                        <CardTitle className="text-base text-foreground">{tr('kpiTargets.title')}</CardTitle>
                        <CardDescription className="text-xs mt-0.5">
                            {tr('kpiTargets.desc')}
                        </CardDescription>
                    </div>
                </div>
            </CardHeader>
            <CardContent className="pt-5 space-y-3">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    {TARGETS.map((t) => (
                        <div key={t.key as string} className="grid gap-1">
                            <Label htmlFor={t.key as string} className="text-xs">
                                {tr(t.label)}
                                <span className="text-muted-foreground ml-2">
                                    {t.direction === 'le' ? tr('kpiTargets.max') : tr('kpiTargets.min')}
                                </span>
                            </Label>
                            <div className="flex items-center gap-2">
                                <span className="text-sm text-muted-foreground font-mono w-5 text-right">
                                    {t.direction === 'le' ? '≤' : '≥'}
                                </span>
                                <Input
                                    id={t.key as string}
                                    type="number"
                                    min={0}
                                    step="0.1"
                                    value={values[t.key as string] ?? ''}
                                    onChange={(e) =>
                                        setValues((cur) => ({ ...cur, [t.key as string]: e.target.value }))
                                    }
                                    placeholder={tr('kpiTargets.ph')}
                                />
                                {t.unit && <span className="text-sm text-muted-foreground w-8">{tr(t.unit)}</span>}
                            </div>
                        </div>
                    ))}
                </div>

                <div className="flex items-center gap-3 pt-3 border-t">
                    <Button onClick={handleSave} disabled={saving}>
                        {saving && <Loader2 size={16} className="mr-2 animate-spin" />}
                        {saving ? tr('kpiTargets.saving') : tr('common.save')}
                    </Button>
                    {toast && (
                        <span className={`text-sm flex items-center gap-1 ${
                            toast.kind === 'success' ? 'text-emerald-500' : 'text-red-500'
                        }`}>
                            {toast.kind === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                            {toast.text}
                        </span>
                    )}
                </div>
            </CardContent>
        </Card>
    );
};

export default KPITargets;
