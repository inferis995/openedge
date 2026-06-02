import api from './client';

// Shape ritornate dall'API /api/shifts — vedi internal/handlers/shifts.go.

export interface Shift {
    id: number;
    name: string;
    start_time: string;          // "HH:MM"
    end_time: string;            // "HH:MM"
    weekdays: number[];          // 0=Domenica … 6=Sabato
    active: boolean;
    wraps: boolean;              // start > end (turno notte attraverso mezzanotte)
}

export interface ShiftAssignment {
    id: number;
    shift_id: number;
    user_id: number;
    username?: string;
    full_name?: string;
    valid_from: string;          // "YYYY-MM-DD"
    valid_to?: string | null;
}

export interface CurrentShiftResponse {
    shift?: Shift | null;
    started_at?: string | null;
    ends_at?: string | null;
    time_left_min: number;
    operators: ShiftAssignment[];
}

export interface CreateShiftDto {
    name: string;
    start_time: string;
    end_time: string;
    weekdays: number[];
    active: boolean;
}

export interface CreateAssignmentDto {
    user_id: number;
    valid_from?: string;
    valid_to?: string | null;
}

export const WEEKDAY_LABELS = ['Dom', 'Lun', 'Mar', 'Mer', 'Gio', 'Ven', 'Sab'];

export const shiftsApi = {
    list: async (): Promise<Shift[]> => {
        const r = await api.get('/shifts');
        return r.data;
    },
    current: async (): Promise<CurrentShiftResponse> => {
        const r = await api.get('/shifts/current');
        return r.data;
    },
    create: async (data: CreateShiftDto): Promise<{ id: number }> => {
        const r = await api.post('/shifts', data);
        return r.data;
    },
    update: async (id: number, data: CreateShiftDto): Promise<void> => {
        await api.put(`/shifts/${id}`, data);
    },
    delete: async (id: number): Promise<void> => {
        await api.delete(`/shifts/${id}`);
    },
    listAssignments: async (id: number): Promise<ShiftAssignment[]> => {
        const r = await api.get(`/shifts/${id}/assignments`);
        return r.data;
    },
    createAssignment: async (id: number, data: CreateAssignmentDto): Promise<{ id: number }> => {
        const r = await api.post(`/shifts/${id}/assignments`, data);
        return r.data;
    },
    deleteAssignment: async (aid: number): Promise<void> => {
        await api.delete(`/shifts/assignments/${aid}`);
    },
};
