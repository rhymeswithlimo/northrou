// The server this client talks to.
//
// A browser served off the box already knows: it is same-origin, and there is
// nothing to configure. An app is different -- it starts knowing nothing, and
// the box could be on this network or across the internet. That is what the
// connection code is for, and it is why "current server / switch server /
// forget this server" exists in settings.

const KEY = 'northrou.server';

/**
 * @typedef {{code: string, lan?: string, coordUrl: string, name?: string,
 *            mode?: 'lan'|'tunnel'}} Server
 */

export const DEFAULT_COORD_URL = 'wss://coord.northrou.app/ws';

/** Served from the box itself: same-origin, no bootstrap needed. */
export const isSameOrigin = () =>
    typeof window !== 'undefined' && !('__TAURI_INTERNALS__' in window);

/** @returns {Server|null} */
export function getServer() {
    try {
        return JSON.parse(localStorage.getItem(KEY) ?? 'null');
    } catch {
        return null;
    }
}

export function setServer(server) {
    try {
        localStorage.setItem(KEY, JSON.stringify(server));
    } catch { /* non-fatal: this session still works */ }
    return server;
}

export function forgetServer() {
    try {
        localStorage.removeItem(KEY);
    } catch { /* nothing to do */ }
}

/**
 * Is a box reachable directly at `base`?
 *
 * Always try this before the tunnel. On the home network the direct route is
 * faster, needs no broker, and works with the internet down -- and that is the
 * common case, since most viewing happens at home.
 */
export async function probeLan(base, { timeoutMs = 2500 } = {}) {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), timeoutMs);
    try {
        const res = await fetch(`${base.replace(/\/$/, '')}/api/health`, { signal: ctrl.signal });
        if (!res.ok) return false;
        const body = await res.json();
        return body?.status === 'ok';
    } catch {
        // Wrong network, box asleep, or nothing listening: all "not here".
        return false;
    } finally {
        clearTimeout(timer);
    }
}

/** Connection codes are shown as NR-XXXX-XXXX; accept them typed any which way. */
export function normalizeCode(input) {
    const clean = (input ?? '').toUpperCase().replace(/[^A-Z0-9]/g, '');
    const body = clean.startsWith('NR') ? clean.slice(2) : clean;
    if (body.length !== 8) return null;
    return `NR-${body.slice(0, 4)}-${body.slice(4)}`;
}
