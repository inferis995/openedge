import api from './client';

// Applications the signed-in user has authorized through the OAuth flow.
//
// These are grants, not sessions: a client holds a refresh token and mints its
// own access tokens from it. Revoking here ends the grant — but not the access
// token already in flight, which is a signed JWT nobody consults a database
// about. The API says so in `note`, and the page repeats it, because a revoke
// button that quietly means "in up to an hour" is worse than none.

export interface OAuthAuthorization {
    client_id: string;
    client_name: string;
    scope: string;
    authorized_at: string;
    last_issued_at: string;
    expires_at: string;
}

export interface RevokeResult {
    revoked: number;
    note: string;
    access_token_ttl_seconds: number;
}

export const oauthApi = {
    listAuthorizations: (): Promise<{ items: OAuthAuthorization[]; total: number }> =>
        api.get('/oauth/authorizations').then((r) => r.data),

    revokeAuthorization: (clientID: string): Promise<RevokeResult> =>
        api.delete(`/oauth/authorizations/${encodeURIComponent(clientID)}`).then((r) => r.data),
};

/** Turns "openedge:read openedge:write" into something a person can act on. */
export const describeScope = (scope: string): string[] =>
    scope
        .split(/\s+/)
        .filter(Boolean)
        .map((s) => {
            if (s === 'openedge:read') return 'Lettura di configurazione e valori';
            if (s === 'openedge:write') return 'Scrittura: comandi ai tag, gateway, tipi, sinottici';
            return s;
        });
