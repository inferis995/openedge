import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { RefreshCw } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
    forecastApi, type ForecastKind, type ForecastProfile, type ForecastProfileSettings, type ForecastStatus, type TagForecast,
} from '@/api/forecast';
import { timeAgo } from '@/lib/relativeTime';
import { showApiError } from '@/lib/api-error-handler';

const KINDS: ForecastKind[] = ['auto', 'process', 'cyclic', 'degradation', 'counter', 'state'];
const MAX_HORIZON_HOURS = 24 * 30;
const INHERIT = 'inherit';

const STATUS_VARIANT: Record<ForecastStatus, 'success' | 'warning' | 'muted' | 'alarm'> = {
    ok: 'success', unreliable: 'warning', learning: 'muted', disabled: 'muted', error: 'alarm', pending: 'muted',
};

const forecastKey = (tagId: number) => ['tag-forecast', tagId];
const profileKey = (tagId: number) => ['forecast-profile', 'tag', tagId];

/**
 * The tag's forecast: what it is now (kind found, engine, how it did on the
 * known history) and the tag's own settings, each of which can be left to
 * inherit from the gateway, area, site or organization.
 */
export function TagForecastTab({ tagId }: { tagId: number }) {
    const { data: forecast } = useQuery({
        queryKey: forecastKey(tagId),
        queryFn: () => forecastApi.getTagForecast(tagId),
        refetchInterval: 60_000,
    });
    const { data: profile } = useQuery({
        queryKey: profileKey(tagId),
        queryFn: () => forecastApi.getProfile('tag', tagId),
    });

    return (
        <div className="space-y-6 py-4">
            <StatusCard forecast={forecast} />
            {profile
                ? <SettingsForm key={JSON.stringify(profile.settings)} tagId={tagId} profile={profile} />
                : <p className="text-sm text-muted-foreground">…</p>}
        </div>
    );
}

function pct(x: number | undefined) {
    return x === undefined ? '—' : `${Math.round(x * 100)}%`;
}

function StatusCard({ forecast }: { forecast?: TagForecast }) {
    const { t, i18n } = useTranslation();
    const status: ForecastStatus = forecast?.status ?? 'pending';
    const rows: [string, string][] = forecast && (status === 'ok' || status === 'unreliable') ? [
        [t('forecast.kind_detected'), forecast.kind ? t(`forecast.kind_${forecast.kind}`) : '—'],
        [t('forecast.engine'), forecast.model ? `${forecast.engine} · ${forecast.model}` : (forecast.engine ?? '—')],
        [t('forecast.skill'), pct(forecast.skill)],
        [t('forecast.coverage'), pct(forecast.coverage)],
        [t('forecast.generated'), timeAgo(forecast.generated_at ? Date.parse(forecast.generated_at) : null, i18n.language)],
    ] : [];
    return (
        <section className="rounded-lg border bg-card p-4 space-y-3">
            <div className="flex flex-wrap items-center gap-2">
                <h3 className="text-sm font-semibold">{t('forecast.status_title')}</h3>
                <Badge variant={STATUS_VARIANT[status]}>{t(`forecast.status_${status}`)}</Badge>
            </div>
            <p className="text-sm text-muted-foreground">{t(`forecast.desc_${status}`)}</p>
            {rows.length > 0 && (
                <dl className="grid grid-cols-1 gap-x-6 gap-y-1.5 text-sm sm:grid-cols-2">
                    {rows.map(([k, v]) => (
                        <div key={k} className="flex justify-between gap-3 border-b border-dashed py-1 sm:block sm:border-0 sm:py-0">
                            <dt className="text-muted-foreground sm:text-xs">{k}</dt>
                            <dd className="font-medium tabular-nums text-right sm:text-left">{v}</dd>
                        </div>
                    ))}
                </dl>
            )}
        </section>
    );
}

function SettingsForm({ tagId, profile }: { tagId: number; profile: ForecastProfile }) {
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

    const refresh = async (quiet = false) => {
        const fc = await forecastApi.refresh(tagId);
        qc.setQueryData(forecastKey(tagId), fc);
        await qc.invalidateQueries({ queryKey: ['trend-forecasts'] });
        if (!quiet) toast.success(t('forecast.refreshed'));
    };

    const save = async () => {
        if (!horizonOk) return;
        const settings: ForecastProfileSettings = {};
        if (enabled !== INHERIT) settings.enabled = enabled === 'on';
        if (kind !== INHERIT) settings.kind = kind as ForecastKind;
        if (hours !== undefined) settings.horizon_hours = hours;
        setBusy(true);
        try {
            const p = await forecastApi.putProfile('tag', tagId, settings);
            qc.setQueryData(profileKey(tagId), p);
            toast.success(t('forecast.saved'));
            // Show the result of the new settings straight away.
            await refresh(true).catch(() => undefined);
        } catch (e) {
            showApiError(e, t('forecast.failed'));
        } finally {
            setBusy(false);
        }
    };

    const onRefresh = async () => {
        setBusy(true);
        try { await refresh(); } catch (e) { showApiError(e, t('forecast.failed')); } finally { setBusy(false); }
    };

    return (
        <section className="space-y-4">
            <div className="space-y-1">
                <h3 className="text-sm font-semibold">{t('forecast.settings_title')}</h3>
                <p className="text-xs text-muted-foreground">{t('forecast.inherit_hint')}</p>
            </div>
            <div className="grid gap-4 sm:grid-cols-3">
                <div className="grid gap-1.5">
                    <Label htmlFor="fc-enabled">{t('forecast.field_enabled')}</Label>
                    <Select value={enabled} onValueChange={setEnabled}>
                        <SelectTrigger id="fc-enabled"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value={INHERIT}>{t('forecast.inherit', { value: inh.enabled ? t('forecast.on') : t('forecast.off') })}</SelectItem>
                            <SelectItem value="on">{t('forecast.on')}</SelectItem>
                            <SelectItem value="off">{t('forecast.off')}</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
                <div className="grid gap-1.5">
                    <Label htmlFor="fc-kind">{t('forecast.field_kind')}</Label>
                    <Select value={kind} onValueChange={setKind}>
                        <SelectTrigger id="fc-kind"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value={INHERIT}>{t('forecast.inherit', { value: t(`forecast.kind_${inh.kind}`) })}</SelectItem>
                            {KINDS.map(k => <SelectItem key={k} value={k}>{t(`forecast.kind_${k}`)}</SelectItem>)}
                        </SelectContent>
                    </Select>
                </div>
                <div className="grid gap-1.5">
                    <Label htmlFor="fc-horizon">{t('forecast.field_horizon')}</Label>
                    <div className="flex items-center gap-2">
                        <Input id="fc-horizon" inputMode="numeric" value={horizon}
                            onChange={e => setHorizon(e.target.value.replace(/[^0-9]/g, ''))}
                            placeholder={t('forecast.inherit', { value: `${inh.horizon_hours}` })}
                            aria-invalid={!horizonOk} className={horizonOk ? '' : 'border-destructive'} />
                        <span className="shrink-0 text-xs text-muted-foreground">{t('forecast.hours')}</span>
                    </div>
                    {!horizonOk && <p className="text-xs text-destructive">{t('forecast.horizon_invalid', { max: MAX_HORIZON_HOURS })}</p>}
                </div>
            </div>

            <div className="sticky -bottom-6 z-10 -mx-6 -mb-6 flex flex-col-reverse gap-2 border-t bg-background px-6 py-3 sm:flex-row sm:justify-end max-sm:pb-[max(0.75rem,env(safe-area-inset-bottom))]">
                <Button variant="outline" onClick={() => void onRefresh()} disabled={busy} className="gap-1.5">
                    <RefreshCw className={`size-4 ${busy ? 'animate-spin' : ''}`} /> {t('forecast.refresh')}
                </Button>
                <Button onClick={() => void save()} disabled={busy || !horizonOk}>{t('forecast.save')}</Button>
            </div>
        </section>
    );
}
