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

export const DEFAULT_COORD_URL = 'wss://coord.northrou.sh/ws';

// Where the official hosted web client is served (Cloudflare Pages). A browser
// on this host is the standalone client: it has no box API of its own and must
// reach a box over the tunnel, unlike a browser a box serves at its own address.
// The coordinator lives on a different host (DEFAULT_COORD_URL) since the client
// is hosted separately, so this is its own constant, not derived from that URL.
const HOSTED_CLIENT_HOST = 'app.northrou.sh';

/**
 * Served from the box itself: same-origin, so talk to it directly with no
 * bootstrap. True for a browser a box serves (its LAN/local address); false for
 * the desktop app and for the hosted web client, both of which reach the box
 * over the tunnel.
 */
export const isSameOrigin = () =>
    typeof window !== 'undefined'
    && !('__TAURI_INTERNALS__' in window)
    && window.location.host !== HOSTED_CLIENT_HOST;

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
 * Connection codes are shown grouped (NR-XXXXX-XXXXX) but accepted typed any
 * which way. Current codes have a 10-character body; older servers issued 8, so
 * accept a range. The server compares dash/case-insensitively, so the canonical
 * form here just needs the NR prefix and the raw body.
 */
export function normalizeCode(input) {
    const clean = (input ?? '').toUpperCase().replace(/[^A-Z0-9]/g, '');
    const body = clean.startsWith('NR') ? clean.slice(2) : clean;
    if (body.length < 8 || body.length > 16) return null;
    return `NR-${body}`;
}
