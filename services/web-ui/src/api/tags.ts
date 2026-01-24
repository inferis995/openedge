import api from './client';
import { Tag, CreateTagDto } from '@/types';

export const tagsApi = {
    getAll: async (gatewayId?: number): Promise<Tag[]> => {
        const response = await api.get('/tags', { params: { gateway_id: gatewayId } });
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

    getCurrentValue: async (id: number): Promise<{ value: any; timestamp: string }> => {
        const response = await api.get(`/tags/${id}/current`);
        return response.data;
    }
};
