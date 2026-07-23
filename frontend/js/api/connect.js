// Resolving which server to talk to, and how.
//
// A browser served off the box talks to it directly, same-origin. An app is
// never same-origin, so it reaches the box peer-to-peer: through the coordinator
// and a WebRTC tunnel to the house.

import { useDirect, useTunnel } from './transport.js';
import { isSignedIn } from './session.js';
import { pair } from '../data/account.js';
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

    // Hole-punch to the house through the official coordinator.
    try {
        await useTunnel({ coordUrl: DEFAULT_COORD_URL, code: server.code });
        setServer({ ...server, coordUrl: DEFAULT_COORD_URL, mode: 'tunnel' });
        return { ok: true, mode: 'tunnel' };
    } catch (e) {
        return { ok: false, reason: 'unreachable', error: e.message };
    }
}

/**
 * Boot guard for every screen behind the connection.
 * Sends the client to the welcome page when there is no reachable server,
 * rather than letting the screen render and fail one request at a time.
 */
export async function requireServer() {
    const res = await connect();
    if (!res.ok) {
        window.location.replace('welcome.html');
        return false;
    }
    return true;
}

/**
 * Reports whether the server still needs first-run setup. Setup happens on the
 * box itself (`northrou setup`), so the client can only say so, not do it.
 * Errors read as "no": an unreachable server is its own problem, reported by
 * the caller's render path.
 * @returns {Promise<boolean>}
 */
export async function needsSetup() {
    try {
        const res = await fetch('/api/setup/status');
        if (res.ok) {
            const { needs_setup } = await res.json();
            return Boolean(needs_setup);
        }
    } catch { /* unreachable; the caller's own render reports it */ }
    return false;
}

/**
 * First-run and sign-in gate for the app's entry page. Call after
 * requireServer has resolved transport, before rendering anything.
 *
 * Authentication is the connection code:
 *   - On the box (same-origin) the request is local and trusted: an already-
 *     set-up box pairs automatically with no code, and a multi-profile
 *     household lands on the picker. A box that still needs setup renders
 *     anyway - the home page checks needsSetup() and says to run
 *     `northrou setup` on the server, since setup happens there, not here.
 *     (Only home has that panel; on a needs-setup box any other page bounces
 *     settings → welcome → index and ends on it. That chain terminates - it
 *     is not a redirect loop.)
 *   - An app/hosted client reaching the box over the tunnel needs the
 *     connection code, which is entered on welcome.html; without a session,
 *     go there.
 *
 * Returns true only when the caller may proceed to render.
 * @returns {Promise<boolean>}
 */
export async function requireReady() {
    if (isSameOrigin()) {
        // Local access is trusted: pair without a code to get a session.
        if (!isSignedIn()) {
            try {
                const res = await pair();
                if (res?.profiles?.length > 1) {
                    window.location.replace('profiles.html');
                    return false;
                }
            } catch {
                // Couldn't pair (server hiccup / no profiles yet): fall through
                // and let the caller's render report it.
            }
        }
        return true;
    }
    // App / hosted client: reached the box over the tunnel, which needs a paired
    // session from welcome.html.
    if (!isSignedIn()) {
        window.location.replace('welcome.html');
        return false;
    }
    return true;
}
