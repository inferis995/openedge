import api from './client';

// User-defined types.
//
// A type declares the shape of a piece of equipment once; every instance is
// generated from it and stays bound to it. Editing the type reconciles every
// instance, which is the whole point — and also why removing a member is the
// one operation in this file that can destroy data.

export interface UDTAlarm {
    id?: number;
    alarm_type: 'high' | 'low';
    threshold: number | null;
    deadband: number;
    delay_seconds: number;
    severity: 'info' | 'warning' | 'critical';
    message: string;
    enabled: boolean;
}

export interface UDTMember {
    id?: number;
    name: string;
    /**
     * Appended to the instance's base address. The type does not know which
     * protocol it will land on, so the suffix carries whatever separator the
     * address language needs: "+2" on Modbus, ".DBX0.1" on S7.
     */
    address_suffix: string;
    data_type: 'BOOL' | 'INT' | 'REAL' | 'DINT' | 'STRING';
    historize: boolean;
    historize_deadband: number;

    scaling_enabled: boolean;
    scaling_raw_min: number;
    scaling_raw_max: number;
    scaling_eu_min: number;
    scaling_eu_max: number;
    scaling_clamp: boolean;
    eu_unit: string;
    eu_decimals: number;
    invert: boolean;

    sort_order: number;
    alarms: UDTAlarm[];
}

export interface UDTType {
    id: number;
    org_id: number;
    name: string;
    description: string;
    members: UDTMember[];
    instance_count: number;
}

export interface UDTInstance {
    id: number;
    type_id: number;
    type_name?: string;
    gateway_id: number;
    name: string;
    base_address: string;
    tag_count?: number;
}

export interface ReconcileResult {
    status: string;
    reconciled: {
        tags_created: number;
        tags_updated: number;
        tags_deleted: number;
    };
}

/**
 * What the API reports when an edit would delete tags, and with them
 * everything the historian recorded for those tags.
 */
export interface DataLossImpact {
    members: string[];
    tags: number;
    history_rows: number;
}

export interface DataLossRefusal {
    error: string;
    impact: DataLossImpact;
}

export interface UDTTypePayload {
    name: string;
    description: string;
    members: UDTMember[];
    /** Required to remove a member that instances already carry. */
    confirm_data_loss?: boolean;
}

export const emptyMember = (): UDTMember => ({
    name: '',
    address_suffix: '',
    data_type: 'REAL',
    historize: true,
    historize_deadband: 0,
    scaling_enabled: false,
    scaling_raw_min: 0,
    scaling_raw_max: 27648,
    scaling_eu_min: 0,
    scaling_eu_max: 100,
    scaling_clamp: true,
    eu_unit: '',
    eu_decimals: 2,
    invert: false,
    sort_order: 0,
    alarms: [],
});

export const udtApi = {
    listTypes: (): Promise<{ items: UDTType[]; total: number }> =>
        api.get('/udt/types').then((r) => r.data),

    getType: (id: number): Promise<UDTType> =>
        api.get(`/udt/types/${id}`).then((r) => r.data),

    createType: (payload: UDTTypePayload): Promise<{ id: number }> =>
        api.post('/udt/types', payload).then((r) => r.data),

    updateType: (id: number, payload: UDTTypePayload): Promise<ReconcileResult> =>
        api.put(`/udt/types/${id}`, payload).then((r) => r.data),

    deleteType: (id: number): Promise<void> =>
        api.delete(`/udt/types/${id}`).then((r) => r.data),

    listInstances: (typeId?: number): Promise<{ items: UDTInstance[]; total: number }> =>
        api
            .get('/udt/instances', { params: typeId ? { type_id: typeId } : undefined })
            .then((r) => r.data),

    createInstance: (payload: {
        type_id: number;
        gateway_id: number;
        name: string;
        base_address: string;
    }): Promise<{ id: number; tags_created: number }> =>
        api.post('/udt/instances', payload).then((r) => r.data),

    deleteInstance: (id: number): Promise<{ tags_deleted: number }> =>
        api.delete(`/udt/instances/${id}`).then((r) => r.data),
};
