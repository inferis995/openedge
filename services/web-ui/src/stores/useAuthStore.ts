import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type UserRole = 'admin' | 'user';

export interface User {
    id: number;
    username: string;
    role: UserRole;
    full_name: string;
    i3x_write?: boolean;
}

interface AuthState {
    token: string | null;
    user: User | null;
    login: (token: string, user: User) => void;
    logout: () => void;
    isAuthenticated: () => boolean;
    isAdmin: () => boolean;
    canI3xWrite: () => boolean;
}

export const useAuthStore = create<AuthState>()(
    persist(
        (set, get) => ({
            token: null,
            user: null,
            login: (token, user) => set({ token, user }),
            logout: () => set({ token: null, user: null }),
            isAuthenticated: () => !!get().token,
            isAdmin: () => get().user?.role === 'admin',
            canI3xWrite: () => {
                const u = get().user;
                return u?.role === 'admin' || u?.i3x_write === true;
            },
        }),
        {
            name: 'auth-storage', // key in localStorage
        }
    )
);
