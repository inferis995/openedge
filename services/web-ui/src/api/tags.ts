import api from './client';
import { Tag, CreateTagDto } from '@/types';

export const tagsApi = {
    getAll: async (gatewayId?: number | null): Promise<Tag[]> => {
        const params = gatewayId !== undefined && gatewayId !== null ? { gateway_id: gatewayId } : {};
        const response = await api.get('/tags', { params });
        return response.data;
    },

    // Get all tags without filtering by gateway
    getAllTags: async (): Promise<Tag[]> => {
        const response = await api.get('/tags');
        return response.data;
    },

    create: async (data: CreateTagDto): Promise<Tag> => {
        const response = await api.post('/tags', data);
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
    }
};
