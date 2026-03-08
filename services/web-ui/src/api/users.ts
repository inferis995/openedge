import api from './client';

export interface User {
    id: number;
    username: string;
    role: 'admin' | 'user';
    full_name: string;
    created_at: string;
}

export interface CreateUserRequest {
    username: string;
    password: string;
    role: 'admin' | 'user';
    full_name?: string;
}

export interface UpdateUserRequest {
    password?: string;
    role?: 'admin' | 'user';
    full_name?: string;
}

export const usersApi = {
    list: async (): Promise<User[]> => {
        const response = await api.get<User[]>('/users');
        return response.data;
    },

    create: async (data: CreateUserRequest): Promise<User> => {
        const response = await api.post<User>('/users', data);
        return response.data;
    },

    update: async (id: number, data: UpdateUserRequest): Promise<User> => {
        const response = await api.put<User>(`/users/${id}`, data);
        return response.data;
    },

    delete: async (id: number): Promise<void> => {
        await api.delete(`/users/${id}`);
    },
};
