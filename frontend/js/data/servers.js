// The server this client talks to.
//
// A browser served off the box already knows: it is same-origin, and there is
// nothing to configure. An app is different -- it starts knowing nothing and
// reaches the box peer-to-peer. That is what the connection code is for, and it
// is why "current server / switch server / forget this server" exists in
// settings.

const KEY = 'northrou.server';

/**
 * @typedef {{code: string, coordUrl: string, name?: string,
 *            mode?: 'tunnel'}} Server
 */

export const DEFAULT_COORD_URL = 'wss://app.northrou.sh/ws';

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

/** Connection codes are shown as NR-XXXX-XXXX; accept them typed any which way. */
export function normalizeCode(input) {
    const clean = (input ?? '').toUpperCase().replace(/[^A-Z0-9]/g, '');
    const body = clean.startsWith('NR') ? clean.slice(2) : clean;
    if (body.length !== 8) return null;
    return `NR-${body.slice(0, 4)}-${body.slice(4)}`;
}
