import api from './client';
import { Gateway, CreateGatewayDto, OpcUaNode } from '@/types';

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
        const payload: any = {
            area_id: data.area_id,
            name: data.name,
            driver_type: data.driver_type,
            scan_rate_ms: data.scan_rate_ms,
            enabled: data.enabled,
        };

        if (data.driver_type === 'MQTT') {
            payload.connection_config = {};
        } else if (data.driver_type === 'OPC_UA') {
            payload.connection_config = {
                endpoint: data.endpoint || '',
                auth_mode: data.auth_mode || 'Anonymous',
                username: data.username || '',
                password: data.password || '',
                cert_file: data.cert_file || '',
                key_file: data.key_file || '',
            };
        } else {
            payload.connection_config = {
                ip_address: data.ip_address,
                port: data.port,
                rack: data.rack,
                slot: data.slot,
                slave_id: data.slave_id,
            };
        }

        const response = await api.post('/gateways', payload, config);
        return response.data;
    },

    update: async (id: number, data: Partial<CreateGatewayDto>): Promise<Gateway> => {
        const config = data.org_id ? {
            headers: {
                'X-Organization-ID': data.org_id.toString()
            }
        } : undefined;

        // Build a CLEAN payload with ONLY the fields the backend UpdateGatewayRequest expects
        const payload: any = {};

        if (data.name !== undefined) payload.name = data.name;
        if (data.driver_type !== undefined) payload.driver_type = data.driver_type;
        if (data.scan_rate_ms !== undefined) payload.scan_rate_ms = data.scan_rate_ms;
        if (data.enabled !== undefined) payload.enabled = data.enabled;
        if ((data as any).zero_based !== undefined) payload.zero_based = (data as any).zero_based;

        // Build connection_config based on driver type
        if (data.driver_type === 'OPC_UA') {
            payload.connection_config = {
                endpoint: data.endpoint || '',
                auth_mode: data.auth_mode || 'Anonymous',
                username: data.username || '',
                password: data.password || '',
                cert_file: data.cert_file || '',
                key_file: data.key_file || '',
            };
        } else if (data.driver_type === 'S7') {
            payload.connection_config = {
                ip_address: data.ip_address,
                rack: (data as any).rack,
                slot: (data as any).slot,
            };
        } else if (data.driver_type === 'MODBUS_TCP') {
            payload.connection_config = {
                ip_address: data.ip_address,
                port: data.port,
                slave_id: data.slave_id,
            };
        } else if (data.driver_type === 'MQTT') {
            payload.connection_config = {};
        } else if (data.ip_address || data.port || data.rack || data.slot || data.slave_id) {
            // Fallback for partial updates without driver_type
            payload.connection_config = {
                ip_address: data.ip_address,
                port: data.port,
                rack: data.rack,
                slot: data.slot,
                slave_id: data.slave_id,
            };
        }

        // If data already has a pre-built connection_config (from GatewaysPage), use it
        if ((data as any).connection_config && !payload.connection_config) {
            payload.connection_config = (data as any).connection_config;
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
    },

    browseNodes: async (gatewayId: number, nodeId?: string): Promise<{ nodes: OpcUaNode[], error?: string }> => {
        const response = await api.post(`/gateways/${gatewayId}/browse`, { node_id: nodeId || 'i=84' });
        return response.data;
    }
};
