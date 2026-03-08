import api from './client';

export interface AlarmEvent {
    id: number;
    tag_id: number;
    definition_id: number | null;
    status: string;
    alarm_type: string;
    severity: string;
    message: string;
    value_at_trigger: number | null;
    trigger_time: string;
    clear_time: string | null;
    bg_ack_user: string | null;
    ack_time: string | null;
    tag_name?: string; // We might join this in the frontend or backend later
}

export const alarmsApi = {
    getActiveAlarms: async (): Promise<AlarmEvent[]> => {
        const response = await api.get('/alarms/active');
        return response.data;
    },

    // Fetch alarm counts for all tags in a gateway
    getGatewayAlarmCounts: async (gatewayId: number): Promise<Record<number, number>> => {
        const response = await api.get(`/gateways/${gatewayId}/alarms/count`);
        return response.data;
    },

    // Fetch alarm counts for all gateways in the organization
    getAllAlarmCounts: async (): Promise<Record<number, number>> => {
        const response = await api.get(`/alarms/count/all`);
        return response.data;
    },

    getAlarmHistory: async (limit: number = 100): Promise<AlarmEvent[]> => {
        const response = await api.get(`/alarms/history?limit=${limit}`);
        return response.data;
    },

    getTagAlarms: async (tagId: number): Promise<any[]> => {
        const response = await api.get(`/tags/${tagId}/alarms`);
        return response.data || [];
    },

    acknowledgeAlarm: async (id: number): Promise<void> => {
        const response = await api.post(`/alarms/${id}/ack`);
        return response.data;
    },

    deleteAlarmHistory: async (id: number): Promise<void> => {
        await api.delete(`/alarms/history/${id}`);
    },

    deleteAllAlarmHistory: async (): Promise<void> => {
        await api.delete(`/alarms/history/all`);
    }
};
