import { useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
    forecastApi, type ForecastKind, type ForecastProfile, type ForecastProfileSettings, type ForecastScope,
} from '@/api/forecast';
import { showApiError } from '@/lib/api-error-handler';
import { FORECAST_KINDS, MAX_HORIZON_HOURS, forecastProfileKey } from '@/lib/forecastUi';

const INHERIT = 'inherit';

/**
 * One level's forecast settings — organization, site, area, gateway or tag.
 * Each field can be left on "Inherit", which shows the value it takes from
 * the levels above. Saving with every field on "Inherit" removes the level's
 * profile.
 *
 * Remount it (key) when the profile changes, so the fields start from it.
 */
export function ForecastProfileForm({ scope, scopeId, profile, canEdit = true, sticky = false, onSaved, extraActions }: {
    scope: ForecastScope;
    scopeId: number;
    profile: ForecastProfile;
    canEdit?: boolean;
    /** Footer pinned to the bottom of a dialog. */
    sticky?: boolean;
    onSaved?: (p: ForecastProfile) => Promise<void> | void;
    extraActions?: (busy: boolean, setBusy: (b: boolean) => void) => ReactNode;
}) {
    const { t } = useTranslation();
    const qc = useQueryClient();
    const own = profile.settings;
    const inh = profile.inherited;
    const [enabled, setEnabled] = useState<string>(own.enabled === undefined ? INHERIT : own.enabled ? 'on' : 'off');
    const [kind, setKind] = useState<string>(own.kind ?? INHERIT);
    const [horizon, setHorizon] = useState(own.horizon_hours === undefined ? '' : String(own.horizon_hours));
    const [busy, setBusy] = useState(false);

    const hours = horizon.trim() === '' ? undefined : Number(horizon);
    const horizonOk = hours === undefined || (Number.isInteger(hours) && hours >= 1 && hours <= MAX_HORIZON_HOURS);
    const dirty = (enabled === INHERIT ? undefined : enabled === 'on') !== own.enabled
        || (kind === INHERIT ? undefined : kind) !== own.kind
        || hours !== own.horizon_hours;

    const save = async () => {
        if (!horizonOk) return;
        const settings: ForecastProfileSettings = {};
        if (enabled !== INHERIT) settings.enabled = enabled === 'on';
        if (kind !== INHERIT) settings.kind = kind as ForecastKind;
        if (hours !== undefined) settings.horizon_hours = hours;
        setBusy(true);
        try {
            const p = await forecastApi.putProfile(scope, scopeId, settings);
            qc.setQueryData(forecastProfileKey(scope, scopeId), p);
            await qc.invalidateQueries({ queryKey: ['forecast-overview'] });
            toast.success(t('forecast.saved'));
            await onSaved?.(p);
        } catch (e) {
            showApiError(e, t('forecast.failed'));
        } finally {
            setBusy(false);
        }
    };

    const footer = sticky
        ? 'sticky -bottom-6 z-10 -mx-6 -mb-6 border-t bg-background px-6 py-3 max-sm:pb-[max(0.75rem,env(safe-area-inset-bottom))]'
        : 'border-t pt-4';

    return (
        <div className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-3">
                <div className="grid gap-1.5">
                    <Label htmlFor={`fc-enabled-${scope}`}>{t('forecast.field_enabled')}</Label>
                    <Select value={enabled} onValueChange={setEnabled} disabled={!canEdit}>
                        <SelectTrigger id={`fc-enabled-${scope}`}><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value={INHERIT}>{t('forecast.inherit', { value: inh.enabled ? t('forecast.on') : t('forecast.off') })}</SelectItem>
                            <SelectItem value="on">{t('forecast.on')}</SelectItem>
                            <SelectItem value="off">{t('forecast.off')}</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
                <div className="grid gap-1.5">
                    <Label htmlFor={`fc-kind-${scope}`}>{t('forecast.field_kind')}</Label>
                    <Select value={kind} onValueChange={setKind} disabled={!canEdit}>
                        <SelectTrigger id={`fc-kind-${scope}`}><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value={INHERIT}>{t('forecast.inherit', { value: t(`forecast.kind_${inh.kind}`) })}</SelectItem>
                            {FORECAST_KINDS.map(k => <SelectItem key={k} value={k}>{t(`forecast.kind_${k}`)}</SelectItem>)}
                        </SelectContent>
                    </Select>
                </div>
                <div className="grid gap-1.5">
                    <Label htmlFor={`fc-horizon-${scope}`}>{t('forecast.field_horizon')}</Label>
                    <div className="flex items-center gap-2">
                        <Input id={`fc-horizon-${scope}`} inputMode="numeric" value={horizon} disabled={!canEdit}
                            onChange={e => setHorizon(e.target.value.replace(/[^0-9]/g, ''))}
                            placeholder={t('forecast.inherit', { value: `${inh.horizon_hours}` })}
                            aria-invalid={!horizonOk} className={horizonOk ? '' : 'border-destructive'} />
                        <span className="shrink-0 text-xs text-muted-foreground">{t('forecast.hours')}</span>
                    </div>
                    {!horizonOk && <p className="text-xs text-destructive">{t('forecast.horizon_invalid', { max: MAX_HORIZON_HOURS })}</p>}
                </div>
            </div>

            {canEdit && (
                <div className={`${footer} flex flex-col-reverse gap-2 sm:flex-row sm:items-center sm:justify-end`}>
                    {dirty && <p className="text-xs text-muted-foreground sm:mr-auto">{t('forecast.unsaved')}</p>}
                    {extraActions?.(busy, setBusy)}
                    <Button onClick={() => void save()} disabled={busy || !horizonOk || !dirty}>{t('forecast.save')}</Button>
                </div>
            )}
        </div>
    );
}
