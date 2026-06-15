import apiClient from './client';

// A widget is one element on the synoptic canvas. The backend stores the
// `layout` array opaquely (JSONB) — this schema lives entirely in the frontend.
export type SynopticWidgetType =
    | 'value'       // numeric/text readout with unit + threshold coloring
    | 'indicator'   // LED dot: on/off or threshold state
    | 'gauge'       // radial gauge min..max
    | 'tank'        // level tank fill driven by value 0..100
    | 'pump'        // pump symbol, color/spin by running state
    | 'valve'       // valve symbol, open/closed by state
    | 'motor'       // motor symbol, running by state
    | 'pipe'        // static pipe segment (no tag)
    | 'label'       // static text
    | 'bargraph'    // horizontal/vertical linear bar
    | 'button';     // write command button (start/stop/toggle)

export interface SynopticWidget {
    id: string;
    type: SynopticWidgetType;
    x: number;
    y: number;
    w: number;
    h: number;
    rotation?: number;
    tagId?: number | null;
    label?: string;
    config?: {
        unit?: string;
        decimals?: number;
        min?: number;            // gauge/tank range
        max?: number;
        warnAbove?: number;      // value/indicator thresholds
        critAbove?: number;
        onValue?: number;        // indicator/pump/valve/motor: value considered "on"/"open"/"running"
        color?: string;          // base/static color (pipe, label, symbol tint)
        fontSize?: number;       // label
        vertical?: boolean;      // bargraph: true = vertical fill
        writeValue?: number;     // button: value written on activate (default 1)
        writeOffValue?: number;  // button: value written on deactivate (default 0)
        momentary?: boolean;     // button: true = write on press AND release, false = toggle
        [key: string]: unknown;
    };
}

export interface Synoptic {
    id: number;
    org_id: number;
    site_id: number | null;
    area_id: number | null;
    name: string;
    description: string;
    background_color: string;
    canvas_w: number;
    canvas_h: number;
    layout: SynopticWidget[];
    created_at: string;
    updated_at: string;
}

export interface SynopticListResponse {
    items: Synoptic[];
    total: number;
}

interface SynopticPayload {
    name: string;
    description?: string;
    site_id?: number | null;
    area_id?: number | null;
    background_color: string;
    canvas_w: number;
    canvas_h: number;
    layout: SynopticWidget[];
}

export const synopticsApi = {
    list: (): Promise<SynopticListResponse> =>
        apiClient.get('/synoptics').then(r => r.data),

    get: (id: number): Promise<Synoptic> =>
        apiClient.get(`/synoptics/${id}`).then(r => r.data),

    create: (payload: SynopticPayload): Promise<{ id: number }> =>
        apiClient.post('/synoptics', payload).then(r => r.data),

    update: (id: number, payload: SynopticPayload): Promise<void> =>
        apiClient.put(`/synoptics/${id}`, payload).then(r => r.data),

    remove: (id: number): Promise<void> =>
        apiClient.delete(`/synoptics/${id}`).then(r => r.data),
};
