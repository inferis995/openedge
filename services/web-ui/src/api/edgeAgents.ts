import api from './client';

// Una scatola installata: l'agente che interroga i PLC di un impianto.
export interface EdgeAgent {
    id: number;
    name: string;
    created_at: string;
    last_seen_at?: string;
    agent_version?: string;
    gateways: number;
}

export interface EdgeAgentsResponse {
    agents: EdgeAgent[];
    // Gateway che nessuna scatola interroga. Sempre zero quando la scatola è
    // una sola — lì l'assegnazione non si applica — quindi un numero qui vuol
    // dire che qualcuno deve intervenire.
    unassigned_gateways: number;
}

export const edgeAgentsApi = {
    list: async (orgId: number): Promise<EdgeAgentsResponse> => {
        const response = await api.get(`/organizations/${orgId}/edge-agents`);
        return response.data;
    },

    // Riavvia una scatola sola. Senza agentId riavvia tutte quelle
    // dell'organizzazione, che è quello che ha sempre fatto.
    restart: async (orgId: number, agentId?: number): Promise<void> => {
        await api.post(`/organizations/${orgId}/edge-restart`,
            agentId ? { agent_id: agentId } : {});
    },
};
