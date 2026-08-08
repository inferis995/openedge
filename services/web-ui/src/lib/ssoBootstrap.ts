import { useAuthStore } from '@/stores/useAuthStore';

/**
 * Consume the JWT handed back by the SSO callback.
 *
 * This MUST run before the router mounts. The backend redirects to
 * `/#sso_token=<jwt>`, but `/` is behind <RequireAuth>, which — finding no
 * session yet — renders <Navigate to="/login">. React Router replaces the
 * whole location with the string "/login", dropping the fragment, so by the
 * time LoginPage mounted the token was already gone and enterprise SSO could
 * never complete for anyone.
 *
 * Reading it at boot removes the race entirely: wherever the router decides to
 * send the user afterwards, the session is already in the store.
 *
 * The query string is still accepted as a fallback for links minted before the
 * token moved into the fragment.
 */
export function consumeSSOToken(): void {
    const fromHash = new URLSearchParams(window.location.hash.replace(/^#/, '')).get('sso_token');
    const token = fromHash ?? new URLSearchParams(window.location.search).get('sso_token');
    if (!token) return;

    try {
        const payload = JSON.parse(atob(token.split('.')[1])) as {
            user_id: number;
            username: string;
            role: 'admin' | 'user';
            org_id?: number | null;
        };
        useAuthStore.getState().login(token, {
            id: payload.user_id,
            username: payload.username,
            role: payload.role,
            full_name: payload.username,
            org_id: payload.org_id ?? null,
        });
    } catch {
        // Malformed token: leave the user logged out; LoginPage shows the form.
        console.error('[SSO] discarded malformed token from callback');
    } finally {
        // Scrub it from the address bar either way, so it is not left in
        // history or copied out of the URL bar.
        window.history.replaceState({}, '', window.location.pathname);
    }
}
