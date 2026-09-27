import api from './client';
import { Tag, CreateTagDto, WriteTagCommand, WriteTagResult } from '@/types';
import { TagWithHierarchy, TagHierarchyResponse } from '@/types/trend';

export interface TagSheetProblem {
    line: number;
    field?: string;
    code: string;
    value?: string;
}

export interface TagSheetRow {
    line: number;
    alias: string;
    address: string;
    data_type: string;
    action: 'create' | 'update' | 'unchanged' | 'invalid';
    changes?: string[];
    problems?: TagSheetProblem[];
}

export interface TagSheetImportResult {
    rows: TagSheetRow[];
    problems?: TagSheetProblem[];
    columns: string[];
    created: number;
    updated: number;
    unchanged: number;
    invalid: number;
    applied: boolean;
}

export const tagsApi = {
    getAll: async (gatewayId?: number | null, areaId?: number | null): Promise<Tag[]> => {
        const params: Record<string, string | number> = {};
        if (gatewayId !== undefined && gatewayId !== null) params.gateway_id = gatewayId;
        else if (areaId !== undefined && areaId !== null) params.area_id = areaId;
        const response = await api.get('/tags', { params });
        return response.data;
    },

    // Get all tags without filtering by gateway
    getAllTags: async (): Promise<Tag[]> => {
        const response = await api.get('/tags');
        return response.data;
    },

    // Get tags with full hierarchy information for tag browser
    getHierarchy: async (): Promise<TagHierarchyResponse> => {
        const response = await api.get('/tags/hierarchy');
        return response.data;
    },

    // Get all tags with hierarchy info (flat list with hierarchy fields)
    getAllWithHierarchy: async (): Promise<TagWithHierarchy[]> => {
        const response = await api.get('/tags/with-hierarchy');
        return response.data;
    },

    create: async (data: CreateTagDto): Promise<Tag> => {
        const response = await api.post('/tags', data);
        return response.data;
    },

    writeTag: async (id: number, command: Omit<WriteTagCommand, 'tag_id'>): Promise<WriteTagResult> => {
        const response = await api.post(`/tags/${id}/write`, command);
        return response.data;
    },

    getTagAlarms: async (tagId: number): Promise<any[]> => {
        const response = await api.get(`/tags/${tagId}/alarms`);
        return response.data;
    },

    saveTagAlarms: async (tagId: number, alarms: any[]): Promise<void> => {
        const response = await api.put(`/tags/${tagId}/alarms`, alarms);
        return response.data;
    },

    update: async (id: number, data: Partial<CreateTagDto>): Promise<Tag> => {
        const response = await api.put(`/tags/${id}`, data);
        return response.data;
    },

    delete: async (id: number): Promise<void> => {
        await api.delete(`/tags/${id}`);
    },

    getCurrentValue: async (id: number): Promise<{ value: any; timestamp: number; quality: number }> => {
        const response = await api.get(`/tags/${id}/current`);
        // Map backend keys to frontend interface
        return {
            value: response.data.v,
            timestamp: response.data.ts,
            quality: response.data.q
        };
    },

    // Import tags from PLC-style text format
    importTags: async (gatewayId: number, content: string, historize?: boolean): Promise<{ created: number; updated: number; errors?: string[] }> => {
        const response = await api.post('/tags/import', { gateway_id: gatewayId, content, historize });
        return response.data;
    },

    // Export tags to PLC-style text format
    exportTags: async (gatewayId: number): Promise<string> => {
        const response = await api.get('/tags/export', { params: { gateway_id: gatewayId } });
        return response.data.content;
    },

    // A CSV or Excel sheet. Without apply the server only says what would
    // happen, row by row; with apply it writes, all or nothing.
    importSheet: async (gatewayId: number, file: File, apply: boolean): Promise<TagSheetImportResult> => {
        const form = new FormData();
        form.append('gateway_id', String(gatewayId));
        form.append('apply', String(apply));
        form.append('file', file);
        const response = await api.post('/tags/import/file', form, {
            headers: { 'Content-Type': 'multipart/form-data' },
        });
        return response.data;
    },

    // Tags as a CSV or Excel file, or a template for this gateway's driver.
    downloadSheet: async (
        gatewayId: number, format: 'csv' | 'xlsx', lang: string, template = false,
    ): Promise<{ blob: Blob; filename: string }> => {
        const response = await api.get(template ? '/tags/import/template' : '/tags/export', {
            params: { gateway_id: gatewayId, format, lang },
            responseType: 'blob',
        });
        const cd = String(response.headers['content-disposition'] ?? '');
        const match = /filename="([^"]+)"/.exec(cd);
        return { blob: response.data as Blob, filename: match?.[1] ?? `tags.${format}` };
    },

    reorder: async (tagIds: number[]): Promise<void> => {
        await api.put('/tags/reorder', { tag_ids: tagIds });
    },

    // Tag shadow — last-known value even when edge is offline.
    // source: "live" (edge online) | "historic" (edge offline, value from DB)
    getShadow: async (tagId: number): Promise<{ tag_id: number; value: any; quality: number; ts: number; source: 'live' | 'historic' | 'unknown' }> => {
        const response = await api.get(`/tags/${tagId}/shadow`);
        return response.data;
    },

    // Batch shadows for all tags of a gateway.
    getShadowBatch: async (gatewayId: number): Promise<{ tag_id: number; value: any; quality: number; ts: number; source: string }[]> => {
        const response = await api.get('/tags/shadows', { params: { gateway_id: gatewayId } });
        return response.data;
    },
};
