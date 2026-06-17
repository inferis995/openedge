import apiClient from './client';

export interface EdgeRelease {
    id: number;
    version: string;
    release_notes: string;
    artifact_url: string;
    sha256_checksum: string;
    published_at: string;
    is_stable: boolean;
    org_status_counts?: {
        pending: number;
        approved: number;
        success: number;
        failed: number;
    };
}

export interface OrgUpdateStatus {
    available: boolean;
    release_id?: number;
    version?: string;
    release_notes?: string;
    artifact_url?: string;
    sha256?: string;
    approved: boolean;
    status?: string;
    approved_at?: string;
    started_at?: string;
    completed_at?: string;
    error_msg?: string;
}

export interface FleetUpdateRow {
    org_id: number;
    org_name: string;
    version: string;
    status: string;
    approved_at?: string;
    completed_at?: string;
    error_msg?: string;
}

export const updatesApi = {
    listReleases: (): Promise<EdgeRelease[]> =>
        apiClient.get('/releases').then(r => r.data),
    createRelease: (payload: {
        version: string;
        release_notes: string;
        artifact_url: string;
        sha256_checksum: string;
        is_stable: boolean;
    }): Promise<{ id: number }> =>
        apiClient.post('/releases', payload).then(r => r.data),
    getFleetStatus: (): Promise<FleetUpdateRow[]> =>
        apiClient.get('/releases/fleet').then(r => r.data),
    getOrgUpdate: (orgId: number): Promise<OrgUpdateStatus> =>
        apiClient.get(`/organizations/${orgId}/update`).then(r => r.data),
    approveUpdate: (orgId: number, releaseId: number): Promise<void> =>
        apiClient.post(`/organizations/${orgId}/approve-update`, { release_id: releaseId }).then(r => r.data),
};
