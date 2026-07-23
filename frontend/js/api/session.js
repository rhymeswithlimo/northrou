// Token storage.
//
// Access tokens are short-lived JWTs; refresh tokens are long-lived, rotating
// and revocable. Rotation matters here: the server invalidates the old refresh
// token the moment it issues a new pair, so a dropped write means the device is
// signed out for good. Every rotation is persisted before the new access token
// is handed back to callers.

const KEY = 'northrou.session';

let cache = null;

/**
 * @typedef {{access_token: string, refresh_token: string, expires_at: string,
 *            profile?: {id: number, name: string}}} Session
 */

/** @returns {Session|null} */
export function getSession() {
    if (cache) return cache;
    try {
        const raw = localStorage.getItem(KEY);
        cache = raw ? JSON.parse(raw) : null;
    } catch {
        cache = null;
    }
    return cache;
}

export function setSession(session) {
    cache = session;
    try {
        localStorage.setItem(KEY, JSON.stringify(session));
    } catch {
        // Storage unavailable (private mode). The session still works for this
        // page; the next load will ask for a pin again.
    }
    return session;
}

export function clearSession() {
    cache = null;
    try {
        localStorage.removeItem(KEY);
    } catch { /* nothing to do */ }
}

export const isSignedIn = () => Boolean(getSession()?.refresh_token);

/** Access tokens are short-lived; treat one about to expire as already expired. */
export function accessTokenExpired(skewMs = 5000) {
    const s = getSession();
    if (!s?.expires_at) return false;
    return Date.parse(s.expires_at) - skewMs <= Date.now();
}
