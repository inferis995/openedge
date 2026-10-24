import api from './client';

/** What sort of signal a tag is; decides how it is forecast (docs/FORECASTING.md). */
export type ForecastKind = 'auto' | 'process' | 'cyclic' | 'degradation' | 'counter' | 'state';

/**
 * ok: beat repeating the last value on the known history, shown.
 * unreliable: made, but did not beat it, so not drawn.
 * learning: not enough history yet. disabled: switched off by a profile.
 * error: no engine could forecast it. pending: never forecast yet.
 */
export type ForecastStatus = 'ok' | 'unreliable' | 'learning' | 'disabled' | 'error' | 'pending';

export interface ForecastPoint {
    t: string;
    q10: number | null;
    q50: number | null;
    q90: number | null;
}

/**
 * An alarm rule the forecast may trip. almost_certain: even the optimistic
 * edge of the band crosses (~90%); likely: the median does; possible: only the
 * pessimistic edge; trend: a slow drift reaches it after the horizon (the
 * remaining useful life — an extrapolation, not a forecast).
 */
export type RiskLikelihood = 'almost_certain' | 'likely' | 'possible' | 'trend';

export interface ForecastRisk {
    rule_id: number;
    type: 'high' | 'high_high' | 'low' | 'low_low' | 'bool_true' | 'bool_false';
    threshold: number;
    severity?: string;
    message?: string;
    likelihood: RiskLikelihood;
    at: string;
    extrapolated?: boolean;
}

/** The values that arrived, against the previous forecast's band. */
export interface ForecastAnomaly {
    checked: number;
    outside: number;
    at?: string;
    value?: number;
    low?: number;
    high?: number;
    expected?: number;
}

export interface ForecastSettings {
    enabled: boolean;
    kind: ForecastKind;
    horizon_hours: number;
    context_days: number;
}

export interface TagForecast {
    tag_id: number;
    status: ForecastStatus;
    reason?: string;
    kind?: ForecastKind;
    engine?: string;
    model?: string;
    step_seconds?: number;
    generated_at?: string;
    /** 1 − error / error of repeating the last value, on the known history. */
    skill?: number;
    /** Share of the true values inside the band (ideal ≈ 0.8). */
    coverage?: number;
    points: ForecastPoint[];
    settings: ForecastSettings;
    risks: ForecastRisk[];
    anomaly?: ForecastAnomaly;
}

export type ForecastScope = 'org' | 'site' | 'area' | 'gateway' | 'tag';

/** A level's own profile: an absent field inherits from the level above. */
export type ForecastProfileSettings = Partial<ForecastSettings>;

export interface ForecastProfile {
    scope: ForecastScope;
    scope_id: number;
    settings: ForecastProfileSettings;
    inherited: ForecastSettings;
    effective: ForecastSettings;
}

export const forecastApi = {
    getTagForecast: async (tagId: number): Promise<TagForecast> => {
        const data = (await api.get(`/tags/${tagId}/forecast`)).data as TagForecast;
        return { ...data, points: data.points ?? [], risks: data.risks ?? [] };
    },
    /** Forecast the tag now (admin) and return the result. */
    refresh: async (tagId: number): Promise<TagForecast> => {
        const data = (await api.post(`/tags/${tagId}/forecast/refresh`)).data as TagForecast;
        return { ...data, points: data.points ?? [], risks: data.risks ?? [] };
    },
    getProfile: async (scope: ForecastScope, scopeId: number): Promise<ForecastProfile> =>
        (await api.get('/forecast/profiles', { params: { scope, scope_id: scopeId } })).data,
    /** Empty settings remove the level's profile: it inherits everything again. */
    putProfile: async (scope: ForecastScope, scopeId: number, settings: ForecastProfileSettings): Promise<ForecastProfile> =>
        (await api.put('/forecast/profiles', { scope, scope_id: scopeId, settings })).data,
};

export interface ForecastOverviewTag {
    tag_id: number;
    alias: string;
    unit?: string;
    data_type: string;
    gateway_id: number;
    gateway_name: string;
    area_id: number;
    area_name: string;
    site_id: number;
    site_name: string;
    status: ForecastStatus;
    reason?: string;
    kind?: ForecastKind;
    engine?: string;
    skill?: number;
    coverage?: number;
    generated_at?: string;
    horizon_hours: number;
    /** The forecast at its last step: where the tag is expected to be. */
    end?: ForecastPoint;
    /** Alarm rules the forecast may trip, soonest first. */
    risks?: ForecastRisk[];
    anomaly?: ForecastAnomaly;
}

export interface ForecastOverview {
    /** Engines the server forecasts with, statistical first. */
    engines: string[];
    summary: Partial<Record<ForecastStatus | 'total' | 'at_risk' | 'anomalous', number>>;
    tags: ForecastOverviewTag[];
}

export const forecastOverviewApi = {
    get: async (): Promise<ForecastOverview> => {
        const d = (await api.get('/forecast/overview')).data as ForecastOverview;
        return { engines: d.engines ?? [], summary: d.summary ?? {}, tags: d.tags ?? [] };
    },
};
