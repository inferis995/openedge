import api from './client';
import { Gateway, CreateGatewayDto } from '@/types';

export const gatewaysApi = {
    getAll: async (areaId?: number): Promise<Gateway[]> => {
        const response = await api.get('/gateways', { params: { area_id: areaId } });
        return response.data;
    },

    get: async (id: number): Promise<Gateway> => {
        const response = await api.get(`/gateways/${id}`);
        return response.data;
    },

    create: async (data: CreateGatewayDto): Promise<Gateway> => {
        const config = data.org_id ? {
            headers: {
                'X-Organization-ID': data.org_id.toString()
            }
        } : undefined;
        const response = await api.post('/gateways', data, config);
        return response.data;
    },

    update: async (id: number, data: Partial<CreateGatewayDto>): Promise<Gateway> => {
        const response = await api.put(`/gateways/${id}`, data);
        return response.data;
    },

    delete: async (id: number): Promise<void> => {
        await api.delete(`/gateways/${id}`);
    },

    testConnection: async (id: number): Promise<{ success: boolean; message: string }> => {
        const response = await api.post(`/gateways/${id}/test`);
        return response.data;
    }
};
