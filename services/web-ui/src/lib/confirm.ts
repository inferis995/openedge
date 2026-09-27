import { create } from 'zustand';

export interface ConfirmOptions {
    title: string;
    description?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    /** Red button: the action deletes or cannot be undone. */
    destructive?: boolean;
}

interface ConfirmState {
    request: (ConfirmOptions & { resolve: (ok: boolean) => void }) | null;
    ask: (o: ConfirmOptions) => Promise<boolean>;
    settle: (ok: boolean) => void;
}

export const useConfirmStore = create<ConfirmState>((set, get) => ({
    request: null,
    ask: (o) =>
        new Promise<boolean>((resolve) => {
            // A second question while one is open answers the first "no":
            // nothing waits forever on a dialog that is no longer shown.
            get().request?.resolve(false);
            set({ request: { ...o, resolve } });
        }),
    settle: (ok) => {
        const r = get().request;
        set({ request: null });
        r?.resolve(ok);
    },
}));

/**
 * Asks before doing something, in the application's own dialog.
 *
 *     if (!(await confirmAction({ title: 'Eliminare il gateway?', destructive: true }))) return;
 *
 * It replaces window.confirm, which the browser draws in its own style, in its
 * own language, with no way to say what will happen or to make the dangerous
 * button look dangerous.
 */
export function confirmAction(o: ConfirmOptions): Promise<boolean> {
    return useConfirmStore.getState().ask(o);
}
