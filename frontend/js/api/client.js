// The API client: bearer injection, token refresh, typed errors.

import { getTransport, apiUrl } from './transport.js';
import * as session from './session.js';

/** An API error carrying the HTTP status, so callers can branch on it. */
export class ApiError extends Error {
    constructor(status, message, body) {
        super(message);
        this.name = 'ApiError';
        this.status = status;
        this.body = body;
    }
    /** The request was rejected on its merits; `body.error` says why. */
    get isBadRequest() { return this.status === 400; }
    get isAuth() { return this.status === 401; }
    get isForbidden() { return this.status === 403; }
    get isNotFound() { return this.status === 404; }
    get isConflict() { return this.status === 409; }
    /** The server is at its transcode cap. `retryAfter` is in seconds. */
    get isBusy() { return this.status === 503; }
}

/** Thrown when the server can't be reached at all (offline, box down). */
export class NetworkError extends Error {
    constructor(cause) {
        super('Could not reach the server.');
        this.name = 'NetworkError';
        this.cause = cause;
    }
}

async function parseBody(res) {
    const type = res.headers.get('content-type') ?? '';
    if (!type.includes('json')) return null;
    return res.json().catch(() => null);
}

async function toError(res) {
    const body = await parseBody(res);
    const err = new ApiError(res.status, body?.error ?? res.statusText ?? 'Request failed', body);
    if (res.status === 503) {
        err.retryAfter = Number(res.headers.get('retry-after')) || 0;
    }
    return err;
}

/* ---------- refresh ---------- */

// One refresh at a time. Without this, a page that fires five requests on load
// races five rotations: the first wins, the rest present a token the server has
// already invalidated, and the device is signed out for good.
let refreshing = null;

async function refreshTokens() {
    if (refreshing) return refreshing;

    refreshing = (async () => {
        const current = session.getSession();
        if (!current?.refresh_token) throw new ApiError(401, 'Not signed in');

        let res;
        try {
            res = await getTransport()(apiUrl('/api/auth/refresh'), {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ refresh_token: current.refresh_token }),
            });
        } catch (e) {
            // A network blip must not clear the session; the tokens are still
            // valid once the box is reachable again.
            throw new NetworkError(e);
        }

        if (!res.ok) {
            // The refresh token is genuinely dead (rotated, revoked, expired).
            session.clearSession();
            throw await toError(res);
        }

        const next = await res.json();
        return session.setSession({ ...current, ...next });
    })();

    try {
        return await refreshing;
    } finally {
        refreshing = null;
    }
}

/* ---------- request ---------- */

/**
 * @param {string} path e.g. "/api/home"
 * @param {{method?: string, body?: any, auth?: boolean, elevated?: boolean,
 *          signal?: AbortSignal, query?: Record<string, any>}} [opts]
 */
export async function request(path, opts = {}) {
    const { method = 'GET', body, auth = true, elevated = false, signal, query } = opts;

    let url = apiUrl(path);
    if (query) {
        const qs = new URLSearchParams(
            Object.entries(query).filter(([, v]) => v !== undefined && v !== null && v !== ''),
        ).toString();
        if (qs) url += `?${qs}`;
    }

    const send = async (token) => {
        const headers = {};
        if (body !== undefined) headers['Content-Type'] = 'application/json';
        if (token) headers.Authorization = `Bearer ${token}`;

        try {
            return await getTransport()(url, {
                method,
                headers,
                signal,
                body: body === undefined ? undefined : JSON.stringify(body),
            });
        } catch (e) {
            if (e?.name === 'AbortError') throw e;
            throw new NetworkError(e);
        }
    };

    let token = null;
    if (elevated) {
        // Admin mutations must present the elevated token; the ordinary access
        // token would come back 403.
        token = session.elevatedToken();
        if (!token) throw new ApiError(403, 'admin elevation required');
    } else if (auth) {
        // Refresh proactively when the token is known to be expired: cheaper
        // than a guaranteed 401 round trip.
        if (session.accessTokenExpired() && session.getSession()?.refresh_token) {
            await refreshTokens();
        }
        token = session.getSession()?.access_token ?? null;
    }

    let res = await send(token);

    // Retry once behind a refresh: the token may have expired between our check
    // and the server reading it, or been revoked server-side.
    if (res.status === 401 && auth && !elevated && session.getSession()?.refresh_token) {
        const next = await refreshTokens();
        res = await send(next.access_token);
    }

    if (!res.ok) throw await toError(res);
    if (res.status === 204) return null;
    return parseBody(res);
}

export const get = (path, opts) => request(path, { ...opts, method: 'GET' });
export const post = (path, body, opts) => request(path, { ...opts, method: 'POST', body });
export const patch = (path, body, opts) => request(path, { ...opts, method: 'PATCH', body });
export const del = (path, opts) => request(path, { ...opts, method: 'DELETE' });
