import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type UserRole = 'admin' | 'user';

export interface User {
    id: number;
    username: string;
    role: UserRole;
    full_name: string;
    org_id?: number | null; // null = global admin, set = org-scoped
    i3x_write?: boolean;
    /** Still on the default password: the server allows nothing but changing it. */
    must_change_password?: boolean;
}

interface AuthState {
    token: string | null;
    user: User | null;
    login: (token: string, user: User) => void;
    logout: () => void;
    /** The server refused a request because the default password must be changed first. */
    requirePasswordChange: () => void;
    isAuthenticated: () => boolean;
    isAdmin: () => boolean;
    isGlobalAdmin: () => boolean; // admin with no org (superuser)
    isOrgAdmin: () => boolean;    // admin scoped to a single org
    isOrgScoped: () => boolean;   // any user scoped to a single org
    canI3xWrite: () => boolean;
}

export const useAuthStore = create<AuthState>()(
    persist(
        (set, get) => ({
            token: null,
            user: null,
            login: (token, user) => set({ token, user }),
            logout: () => set({ token: null, user: null }),
            requirePasswordChange: () => {
                const u = get().user;
                if (u && !u.must_change_password) set({ user: { ...u, must_change_password: true } });
            },
            isAuthenticated: () => !!get().token,
            isAdmin: () => get().user?.role === 'admin',
            isGlobalAdmin: () => get().user?.role === 'admin' && !get().user?.org_id,
            isOrgAdmin: () => get().user?.role === 'admin' && !!get().user?.org_id,
            isOrgScoped: () => !!get().user?.org_id,
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
