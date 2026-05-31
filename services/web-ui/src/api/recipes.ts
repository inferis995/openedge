import api from './client';

// A recipe — a named set of (tag, value) pairs the operator loads with
// one click to write multiple setpoints to the PLCs at once. Scoped to
// one organization.
export interface RecipeValue {
    tag_id: number;
    tag_alias?: string;
    gateway_id?: number;
    data_type?: string;
    value: string;
}

export interface Recipe {
    id: number;
    org_id: number;
    name: string;
    description?: string;
    created_at: string;
    updated_at: string;
    created_by?: number | null;
    value_count: number;
    values?: RecipeValue[];
}

// Per-tag result of a recipe load. The driver may publish ACKs back
// later via `sys/write-ack/*` (future); for now `ok` only means the
// MQTT publish from the cloud succeeded.
export interface RecipeRunResult {
    tag_id: number;
    tag_alias: string;
    ok: boolean;
    error?: string;
}

export interface RecipeRun {
    id: number;
    recipe_id: number;
    triggered_by?: number | null;
    triggered_username?: string;
    triggered_at: string;
    status: 'pending' | 'success' | 'partial' | 'failed';
    results: RecipeRunResult[] | null;
}

export interface CreateRecipeDto {
    name: string;
    description?: string;
    values: { tag_id: number; value: string }[];
}

export interface LoadResult {
    run_id: number;
    status: 'success' | 'partial' | 'failed';
    written: number;
    failed: number;
    results: RecipeRunResult[];
}

export const recipesApi = {
    getAll: async (): Promise<Recipe[]> => {
        const r = await api.get('/recipes');
        return r.data;
    },
    get: async (id: number): Promise<Recipe> => {
        const r = await api.get(`/recipes/${id}`);
        return r.data;
    },
    create: async (data: CreateRecipeDto): Promise<{ id: number }> => {
        const r = await api.post('/recipes', data);
        return r.data;
    },
    update: async (id: number, data: CreateRecipeDto): Promise<void> => {
        await api.put(`/recipes/${id}`, data);
    },
    delete: async (id: number): Promise<void> => {
        await api.delete(`/recipes/${id}`);
    },
    load: async (id: number): Promise<LoadResult> => {
        const r = await api.post(`/recipes/${id}/load`);
        return r.data;
    },
    runs: async (id: number, limit = 50): Promise<RecipeRun[]> => {
        const r = await api.get(`/recipes/${id}/runs`, { params: { limit } });
        return r.data;
    },
};
