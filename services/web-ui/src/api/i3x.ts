import apiClient from './client';

export interface I3XEquipment {
    id: string;
    name: string;
    type: 'Equipment' | 'Assembly';
    description?: string;
    parentId?: string;
    path?: string;
    attributes?: {
        driver_type?: string;
        scan_rate_ms?: number;
        enabled?: boolean;
        [key: string]: unknown;
    };
}

export interface I3XPropertyValue {
    value: unknown;
    quality: 0 | 64 | 192;
    timestamp: string;
}

export interface I3XProperty {
    id: string;
    name: string;
    equipmentId: string;
    dataType: 'Boolean' | 'Int32' | 'Float' | 'String';
    historize: boolean;
    current?: I3XPropertyValue;
}

export interface I3XAlarm {
    id: string;
    propertyId: string;
    equipmentId: string;
    severity: 'Info' | 'Warning' | 'Critical';
    status: 'Active' | 'Acknowledged' | 'Cleared';
    alarmType: string;
    message: string;
    value?: unknown;
    triggerTime: string;
    clearTime?: string;
    ackTime?: string;
    ackUser?: string;
}

export interface I3XListResponse<T> {
    items: T[];
    total: number;
}

export const i3xApi = {
    listEquipment: (): Promise<I3XListResponse<I3XEquipment>> =>
        apiClient.get('/i3x/v1/equipment').then(r => r.data),

    listEquipmentProperties: (id: string): Promise<I3XListResponse<I3XProperty>> =>
        apiClient.get(`/i3x/v1/equipment/${id}/properties`).then(r => r.data),

    listAlarms: (): Promise<I3XListResponse<I3XAlarm>> =>
        apiClient.get('/i3x/v1/alarms').then(r => r.data),

    listAlarmHistory: (limit = 100, offset = 0): Promise<I3XListResponse<I3XAlarm>> =>
        apiClient.get(`/i3x/v1/alarms/history?limit=${limit}&offset=${offset}`).then(r => r.data),

    writePropertyValue: (id: string, value: unknown): Promise<void> =>
        apiClient.put(`/i3x/v1/properties/${id}/value`, { value }).then(r => r.data),
};
