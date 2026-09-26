import api from './client';

// Cosa interroga una scatola oltre ai gateway assegnati a lei.
//   assigned — solo quelli assegnati. Il default, e l'unico sicuro quando il
//              server interroga anche lui i suoi PLC.
//   all      — anche tutti quelli non assegnati a nessuna scatola. Per un
//              server in cloud che non interroga niente. Al massimo una per
//              organizzazione.
export type EdgeAgentScope = 'assigned' | 'all';

// Una scatola installata: l'agente che interroga i PLC di un impianto.
export interface EdgeAgent {
    id: number;
    name: string;
    scope: EdgeAgentScope;
    created_at: string;
    last_seen_at?: string;
    agent_version?: string;
    // Quanti gateway interroga davvero: gli assegnati e, con scope "all",
    // quelli di nessuno.
    gateways: number;
}

export interface EdgeAgentsResponse {
    agents: EdgeAgent[];
    // Gateway che nessuna scatola interroga: sono del server.
    server_gateways: number;
    // Se il driver-manager del server gira. Dove non gira (server in cloud)
    // i gateway del server non li interroga nessuno.
    server_polls: boolean;
    // Gateway che nessuno interroga: da risolvere.
    unpolled_gateways: number;
}

export const edgeAgentsApi = {
    list: async (orgId: number): Promise<EdgeAgentsResponse> => {
        const response = await api.get(`/organizations/${orgId}/edge-agents`);
        return response.data;
    },

    update: async (orgId: number, agentId: number,
        change: { name?: string; scope?: EdgeAgentScope }): Promise<void> => {
        await api.put(`/organizations/${orgId}/edge-agents/${agentId}`, change);
    },

    // Rimuove la scatola e revoca la sua chiave. I suoi gateway tornano al
    // server.
    remove: async (orgId: number, agentId: number): Promise<void> => {
        await api.delete(`/organizations/${orgId}/edge-agents/${agentId}`);
    },

    // Riavvia una scatola sola. Senza agentId riavvia tutte quelle
    // dell'organizzazione, che è quello che ha sempre fatto.
    restart: async (orgId: number, agentId?: number): Promise<void> => {
        await api.post(`/organizations/${orgId}/edge-restart`,
            agentId ? { agent_id: agentId } : {});
    },
};
