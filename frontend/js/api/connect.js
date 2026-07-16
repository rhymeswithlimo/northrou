// Resolving which server to talk to, and how.
//
// Order matters: LAN before tunnel, always. At home the direct route is faster,
// needs no broker, and works with the internet down -- and home is where most
// viewing happens. The tunnel is the fallback for being out of the house, not
// the default.

import { useDirect, useTunnel } from './transport.js';
import { getServer, setServer, probeLan, isSameOrigin, DEFAULT_COORD_URL } from '../data/servers.js';

/**
 * Point the client at its server.
 * @returns {Promise<{ok: boolean, mode?: 'same-origin'|'lan'|'tunnel', reason?: string}>}
 */
export async function connect() {
    // Served off the box: it is right there. Nothing to resolve.
    if (isSameOrigin()) {
        useDirect('');
        return { ok: true, mode: 'same-origin' };
    }

    const server = getServer();
    if (!server) return { ok: false, reason: 'no-server' };

    // 1. Is it on this network?
    if (server.lan && await probeLan(server.lan)) {
        useDirect(server.lan);
        setServer({ ...server, mode: 'lan' });
        return { ok: true, mode: 'lan' };
    }

    // 2. Not here. Go through the broker and hole-punch to the house.
    if (!server.code) return { ok: false, reason: 'unreachable' };
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
