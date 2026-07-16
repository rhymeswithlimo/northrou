// Resolving which server to talk to, and how.
//
// A browser served off the box talks to it directly, same-origin. An app is
// never same-origin, so it reaches the box peer-to-peer: through the coordinator
// and a WebRTC tunnel to the house.

import { useDirect, useTunnel } from './transport.js';
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
