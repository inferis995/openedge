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

        // Transform flat DTO to nested backend structure
        const payload = {
            area_id: data.area_id,
            name: data.name,
            driver_type: data.driver_type,
            scan_rate_ms: data.scan_rate_ms,
            enabled: data.enabled,
            connection_config: {
                ip_address: data.ip_address,
                port: data.port,
                rack: data.rack,
                slot: data.slot,
                slave_id: data.slave_id,
            }
        };

        const response = await api.post('/gateways', payload, config);
        return response.data;
    },

    update: async (id: number, data: Partial<CreateGatewayDto>): Promise<Gateway> => {
        const config = data.org_id ? {
            headers: {
                'X-Organization-ID': data.org_id.toString()
            }
        } : undefined;

        // Transform flat DTO to nested backend structure for update
        // Only include connection_config if relevant fields are present
        let payload: any = { ...data };

        // If updating connection params, wrap them
        if (data.ip_address || data.port || data.rack || data.slot || data.slave_id) {
            // We need to fetch existing config first to merge? Or backend handles partial updates inside JSONB?
            // Backend replaces entire connection_config if provided.
            // For simplify, we assume caller provides all necessary connection params if updating connection.
            // OR we map what we have.
            payload.connection_config = {
                ip_address: data.ip_address,
                port: data.port,
                rack: data.rack,
                slot: data.slot,
                slave_id: data.slave_id,
            };
            // Remove flat fields to be clean
            delete payload.ip_address;
            delete payload.port;
            delete payload.rack;
            delete payload.slot;
            delete payload.slave_id;
        }

        const response = await api.put(`/gateways/${id}`, payload, config);
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
