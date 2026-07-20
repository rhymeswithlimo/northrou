// Resolving which server to talk to, and how.
//
// A browser served off the box talks to it directly, same-origin. An app is
// never same-origin, so it reaches the box peer-to-peer: through the coordinator
// and a WebRTC tunnel to the house.

import { useDirect, useTunnel } from './transport.js';
import { isSignedIn } from './session.js';
import { getServer, setServer, isSameOrigin, DEFAULT_COORD_URL } from '../data/servers.js';

/**
 * Point the client at its server.
 * @returns {Promise<{ok: boolean, mode?: 'same-origin'|'tunnel', reason?: string}>}
 */
export async function connect() {
    // Served off the box: it is right there. Nothing to resolve.
    if (isSameOrigin()) {
        useDirect('');
        return { ok: true, mode: 'same-origin' };
    }

    const server = getServer();
    if (!server) return { ok: false, reason: 'no-server' };
    if (!server.code) return { ok: false, reason: 'unreachable' };

    // Hole-punch to the house through the broker.
    try {
        await useTunnel({ coordUrl: server.coordUrl ?? DEFAULT_COORD_URL, code: server.code });
        setServer({ ...server, mode: 'tunnel' });
        return { ok: true, mode: 'tunnel' };
    } catch (e) {
        return { ok: false, reason: 'unreachable', error: e.message };
    }
}

/**
 * Boot guard for every screen behind the connection.
 * Sends the client to the bootstrap page when there is no reachable server,
 * rather than letting the screen render and fail one request at a time.
 */
export async function requireServer() {
    const res = await connect();
    if (!res.ok) {
        window.location.replace('connect.html');
        return false;
    }
    return true;
}

/**
 * First-run and sign-in gate for the app's entry page. Call after
 * requireServer has resolved transport, before rendering anything.
 *
 * A fresh box has no account, so the library would 401 on every request and
 * read as "server unreachable". Instead:
 *   - On the box (same-origin), a box that still needs setup belongs to the
 *     setup wizard. setup.html talks to the box with a same-origin fetch, so
 *     this only applies here; an app reaching the box over the tunnel skips it.
 *   - A set-up box this device is not signed into goes to the sign-in page.
 *     This is the common case when opening the LAN address from a second
 *     device (e.g. a phone) that has no session of its own yet.
 *
 * Returns true only when the caller may proceed to render.
 * @returns {Promise<boolean>}
 */
export async function requireReady() {
    if (isSameOrigin()) {
        try {
            const res = await fetch('/api/setup/status');
            if (res.ok) {
                const { needs_setup } = await res.json();
                if (needs_setup) {
                    window.location.replace('setup.html');
                    return false;
                }
            }
        } catch {
            // Status unreachable: don't guess. Let the caller's own render path
            // report the unreachable server.
        }
    }
    if (!isSignedIn()) {
        window.location.replace('login.html');
        return false;
    }
    return true;
}
