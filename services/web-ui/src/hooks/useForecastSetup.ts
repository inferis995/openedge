import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { assetsApi, forecastApi, oeeForecastApi, type ForecastOverview } from '@/api/forecast';
import { shiftsApi } from '@/api/shifts';
import { maintenanceApi } from '@/api/maintenance';
import { useOrgModules } from '@/hooks/useOrgModules';
import { useAuthStore } from '@/stores/useAuthStore';
import { useNavigationStore } from '@/stores/useNavigationStore';

/**
 * done: in place. todo: needed for something the page offers. wait: in
 * place, data still coming. optional: worth it in some plants only.
 */
export type StepState = 'done' | 'todo' | 'wait' | 'optional';

export interface SetupStep {
    key: string;
    state: StepState;
    title: string;
    detail: string;
    action?: { label: string; to?: string; tab?: string; run?: () => Promise<void> };
}


/** What forecasts already have to work with, and what to set up next. */
export function useSetupSteps(overview: ForecastOverview | undefined): SetupStep[] {
    const { t } = useTranslation();
    const qc = useQueryClient();
    const { isAdmin } = useAuthStore();
    const { selectedOrgId } = useNavigationStore();
    const modules = useOrgModules(selectedOrgId);
    const has = (m: string) => !modules || modules.has(m);
    const opts = { enabled: !!selectedOrgId, retry: false, staleTime: 60_000 } as const;
    const { data: assets } = useQuery({ queryKey: ['forecast-assets', selectedOrgId], queryFn: () => assetsApi.list(), ...opts });
    const { data: oee } = useQuery({ queryKey: ['forecast-oee', selectedOrgId], queryFn: () => oeeForecastApi.list(), ...opts });
    const { data: custom } = useQuery({ queryKey: ['forecast-custom-profiles', selectedOrgId], queryFn: () => forecastApi.customProfiles(), ...opts });
    const { data: shifts } = useQuery({ queryKey: ['shifts'], queryFn: () => shiftsApi.list(), ...opts, enabled: opts.enabled && has('shifts') });
    // Windows not over yet; counted when fetched, not while rendering.
    const { data: ahead } = useQuery({
        queryKey: ['maintenance', 'ahead'],
        queryFn: async () => { const now = Date.now(); return (await maintenanceApi.list()).filter(m => Date.parse(m.end_at) > now).length; },
        ...opts, enabled: opts.enabled && has('maintenance'),
    });
    const admin = isAdmin();
    if (!overview) return [];

    const tags = overview.tags;
    const ok = tags.filter(tg => tg.status === 'ok').length;
    const learning = tags.filter(tg => tg.status === 'learning' || tg.status === 'pending').length;
    const steps: SetupStep[] = [];

    steps.push(tags.length === 0
        ? { key: 'tags', state: 'todo', title: t('forecast.setup_tags'), detail: t('forecast.setup_tags_none'), action: { label: t('forecast.setup_open_tags'), to: '/tags' } }
        : ok === 0
            ? { key: 'tags', state: 'wait', title: t('forecast.setup_tags'), detail: t('forecast.setup_tags_learning', { count: tags.length }) }
            : { key: 'tags', state: 'done', title: t('forecast.setup_tags'), detail: t('forecast.setup_tags_ok', { ok, total: tags.length, learning }), action: { label: t('forecast.setup_see'), tab: 'tags' } });

    const model = overview.engines.some(e => e !== 'statistical');
    steps.push({ key: 'model', state: model ? 'done' : 'optional', title: t('forecast.setup_model'),
        detail: model ? t('forecast.setup_model_on') : t('forecast.setup_model_off') });

    const nAssets = assets?.length ?? 0;
    steps.push(nAssets > 0
        ? { key: 'machines', state: 'done', title: t('forecast.setup_machines'), detail: t('forecast.setup_machines_ok', { count: nAssets }), action: { label: t('forecast.setup_see'), tab: 'machines' } }
        : { key: 'machines', state: 'todo', title: t('forecast.setup_machines'), detail: t('forecast.setup_machines_none'),
            action: admin ? { label: t('forecast.machines_from_gateways'), run: async () => {
                const r = await assetsApi.fromGateways();
                toast.success(t('forecast.machines_created', { count: r.created }));
                await qc.invalidateQueries({ queryKey: ['forecast-assets'] });
            } } : { label: t('forecast.setup_see'), tab: 'machines' } });

    if (nAssets > 0) {
        const withState = (assets ?? []).filter(a => a.state_tag_id).length;
        steps.push({ key: 'state', state: withState > 0 ? 'done' : 'optional', title: t('forecast.setup_state'),
            detail: t('forecast.setup_state_detail', { count: withState, total: nAssets }), action: { label: t('forecast.setup_see'), tab: 'machines' } });
    }

    if (has('shifts')) {
        const active = (shifts ?? []).filter(s => s.active);
        steps.push(active.length > 0
            ? { key: 'shifts', state: 'done', title: t('forecast.setup_shifts'),
                detail: t('forecast.setup_shifts_ok', { list: active.map(s => `${s.name} ${s.start_time}–${s.end_time}`).join(' · ') }),
                action: { label: t('forecast.setup_manage'), to: '/shifts' } }
            : { key: 'shifts', state: 'todo', title: t('forecast.setup_shifts'), detail: t('forecast.setup_shifts_none'), action: { label: t('forecast.setup_manage'), to: '/shifts' } });
    }

    if (has('maintenance')) {
        steps.push({ key: 'maintenance', state: (ahead ?? 0) > 0 ? 'done' : 'optional', title: t('forecast.setup_maintenance'),
            detail: (ahead ?? 0) > 0 ? t('forecast.setup_maintenance_ok', { count: ahead }) : t('forecast.setup_maintenance_none'),
            action: { label: t('forecast.setup_manage'), to: '/maintenance' } });
    }

    if (has('oee')) {
        const profiles = oee ?? [];
        const ready = profiles.filter(p => p.oee).length;
        steps.push(profiles.length === 0
            ? { key: 'oee', state: 'optional', title: t('forecast.setup_oee'), detail: t('forecast.setup_oee_none'), action: { label: t('forecast.setup_manage'), to: '/oee-profiles' } }
            : { key: 'oee', state: ready === profiles.length ? 'done' : 'todo', title: t('forecast.setup_oee'),
                detail: t('forecast.setup_oee_detail', { ready, total: profiles.length }),
                action: ready === profiles.length ? { label: t('forecast.setup_see'), tab: 'production' } : { label: t('forecast.setup_manage'), to: '/oee-profiles' } });
    }

    steps.push({ key: 'levels', state: 'done', title: t('forecast.setup_levels'),
        detail: (custom?.length ?? 0) > 0 ? t('forecast.setup_levels_custom', { count: custom?.length }) : t('forecast.setup_levels_default') });
    return steps;
}

/** Steps that count towards being set up: the optional ones do not. */
export const setupProgress = (steps: SetupStep[]) => {
    const counted = steps.filter(s => s.state !== 'optional');
    return { done: counted.filter(s => s.state === 'done' || s.state === 'wait').length, total: counted.length };
};
