import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { RefreshCw } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { forecastApi, type ForecastStatus, type TagForecast } from '@/api/forecast';
import { ForecastProfileForm } from '@/components/forecast/ForecastProfileForm';
import { FORECAST_STATUS_VARIANT, forecastProfileKey, tagForecastKey } from '@/lib/forecastUi';
import { timeAgo } from '@/lib/relativeTime';
import { showApiError } from '@/lib/api-error-handler';
import { useAuthStore } from '@/stores/useAuthStore';

/**
 * The tag's forecast: what it is now (kind found, engine, how it did on the
 * known history) and the tag's own settings, each of which can be left to
 * inherit from the gateway, area, site or organization.
 */
export function TagForecastTab({ tagId, sticky = true }: { tagId: number; sticky?: boolean }) {
    const { t } = useTranslation();
    const qc = useQueryClient();
    const { isAdmin } = useAuthStore();
    const { data: forecast } = useQuery({
        queryKey: tagForecastKey(tagId),
        queryFn: () => forecastApi.getTagForecast(tagId),
        refetchInterval: 60_000,
    });
    const { data: profile } = useQuery({
        queryKey: forecastProfileKey('tag', tagId),
        queryFn: () => forecastApi.getProfile('tag', tagId),
    });

    const refresh = async (quiet = false) => {
        const fc = await forecastApi.refresh(tagId);
        qc.setQueryData(tagForecastKey(tagId), fc);
        await qc.invalidateQueries({ queryKey: ['trend-forecasts'] });
        await qc.invalidateQueries({ queryKey: ['forecast-overview'] });
        if (!quiet) toast.success(t('forecast.refreshed'));
    };

    return (
        <div className="space-y-6 py-4">
            <ForecastStatusCard forecast={forecast} />
            <section className="space-y-4">
                <div className="space-y-1">
                    <h3 className="text-sm font-semibold">{t('forecast.settings_title')}</h3>
                    <p className="text-xs text-muted-foreground">{t('forecast.inherit_hint')}</p>
                </div>
                {profile ? (
                    <ForecastProfileForm key={JSON.stringify(profile.settings)} scope="tag" scopeId={tagId}
                        profile={profile} canEdit={isAdmin()} sticky={sticky}
                        // Show the result of the new settings straight away.
                        onSaved={() => refresh(true).catch(() => undefined)}
                        extraActions={(busy, setBusy) => (
                            <Button variant="outline" disabled={busy} className="gap-1.5" onClick={async () => {
                                setBusy(true);
                                try { await refresh(); } catch (e) { showApiError(e, t('forecast.failed')); } finally { setBusy(false); }
                            }}>
                                <RefreshCw className={`size-4 ${busy ? 'animate-spin' : ''}`} /> {t('forecast.refresh')}
                            </Button>
                        )} />
                ) : <p className="text-sm text-muted-foreground">{t('forecast.loading')}</p>}
            </section>
        </div>
    );
}

function pct(x: number | undefined) {
    return x === undefined ? '—' : `${Math.round(x * 100)}%`;
}

/** Where the tag's forecast stands, and how it did on the known history. */
export function ForecastStatusCard({ forecast }: { forecast?: TagForecast }) {
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
                <Badge variant={FORECAST_STATUS_VARIANT[status]}>{t(`forecast.status_${status}`)}</Badge>
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
