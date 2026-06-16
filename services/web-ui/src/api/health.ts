import api from './client';

export interface SystemHealth {
  db: { ok: boolean; latency_ms: number; open_connections: number; in_use: number };
  redis: { ok: boolean; latency_ms: number } | null;
  memory: { alloc_mb: number; sys_mb: number; num_gc: number };
  goroutines: number;
  uptime_seconds: number;
}

export const healthApi = {
  detailed: (): Promise<SystemHealth> =>
    api.get('/health/detailed').then(r => r.data),
};
