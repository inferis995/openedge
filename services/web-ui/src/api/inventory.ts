import api from './client';

export interface InventoryDevice {
    gateway_id: number;
    name: string;
    site: string;
    area: string;
    organization: string;
    protocol: string;
    endpoint: string;
    scan_rate_ms: number;
    enabled: boolean;
    health: string;
    health_seen_at?: string;
    agent_version?: string;
    tags: number;
    historized_tags: number;
    alarmed_tags: number;
    first_seen: string;
}

export interface InventorySummary {
    devices: number;
    online: number;
    offline: number;
    unknown: number;
    disabled: number;
    tags: number;
    by_protocol: Record<string, number>;
}

export interface InventoryResponse {
    devices: InventoryDevice[];
    summary: InventorySummary;
    generated_at: string;
}

export const inventoryApi = {
    get: async (): Promise<InventoryResponse> => {
        const response = await api.get('/inventory');
        return response.data;
    },

    // Scaricato come blob e non aperto in una scheda: l'endpoint richiede il
    // token JWT, che una navigazione del browser non porterebbe con sé.
    exportCsv: async (): Promise<Blob> => {
        const response = await api.get('/inventory/export.csv', { responseType: 'blob' });
        return response.data;
    },
};
