import api from './client';

export type WebhookEvent = 'alarm.active' | 'alarm.cleared' | 'tag.write' | 'edge.online' | 'edge.offline';

export interface Webhook {
    id: number;
    org_id: number;
    url: string;
    events: WebhookEvent[];
    enabled: boolean;
    created_at: string;
    last_triggered_at: string | null;
    last_status_code: number | null;
    last_error: string | null;
    secret?: string; // only present on create response
}

export const webhooksApi = {
    list: async (orgId: number): Promise<Webhook[]> => {
        const r = await api.get(`/organizations/${orgId}/webhooks`);
        return r.data;
    },

    create: async (orgId: number, data: { url: string; events: WebhookEvent[] }): Promise<Webhook> => {
        const r = await api.post(`/organizations/${orgId}/webhooks`, data);
        return r.data;
    },

    delete: async (orgId: number, webhookId: number): Promise<void> => {
        await api.delete(`/organizations/${orgId}/webhooks/${webhookId}`);
    },
};

export const WEBHOOK_EVENTS: { value: WebhookEvent; label: string }[] = [
    { value: 'alarm.active', label: 'Alarm activated' },
    { value: 'alarm.cleared', label: 'Alarm cleared' },
    { value: 'tag.write', label: 'Tag write command' },
    { value: 'edge.online', label: 'Edge came online' },
    { value: 'edge.offline', label: 'Edge went offline' },
];
