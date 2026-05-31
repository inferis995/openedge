import api from './client';

export interface HostInfo {
    hostname: string;
    os: string;
    kernel?: string;
    arch: string;
    uptime_sec?: number;
    api_uptime_sec: number;
    load_average?: number[];
    now_utc: string;
}

export interface CPUInfo {
    cores: number;
    model_name?: string;
    usage_pct?: number;
}

export interface MemoryInfo {
    total_bytes: number;
    available_bytes: number;
    used_pct: number;
}

export interface DiskInfo {
    path: string;
    filesystem?: string;
    total_bytes: number;
    used_bytes: number;
    used_pct: number;
}

export interface NetworkIfInfo {
    name: string;
    link_up: boolean;
    rx_bytes: number;
    tx_bytes: number;
    rx_errors: number;
    tx_errors: number;
}

export interface ServiceHealth {
    ok: boolean;
    message?: string;
    latency_ms?: number;
}

export interface Diagnostics {
    generated_at: string;
    host: HostInfo;
    cpu?: CPUInfo;
    memory?: MemoryInfo;
    disk?: DiskInfo[];
    network?: NetworkIfInfo[];
    services: Record<string, ServiceHealth>;
}

export const diagnosticsApi = {
    get: async (): Promise<Diagnostics> => {
        const r = await api.get('/system/diagnostics');
        return r.data;
    },
};
