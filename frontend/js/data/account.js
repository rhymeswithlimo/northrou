// Account/session seam.
//
// Note on `admin`: it is a property of the connection, not the profile. `me.admin`
// is true only when the request reached the box directly (a browser on the
// server's own network, or the CLI); a remote app through the tunnel is never
// admin. So the Server admin section shows controls only to local sessions.

import { get, post } from '../api/client.js';
import * as session from '../api/session.js';

export async function getMe() {
    return get('/api/me');
}

/** Set the current profile's preferred audio/subtitle languages. */
export async function setMyLanguage({ audio, subtitle }) {
    return post('/api/me/language', { audio, subtitle });
}

/**
 * Server version and whether an update is available. A read, open to any signed-in
 * session (not an admin mutation). Installing the update is done on the server
 * itself, not from here.
 */
export async function getUpdateInfo() {
    return get('/api/admin/update');
}

/**
 * Pair this device and sign in. The server connection code is the sole
 * credential: a remote client (through the tunnel) must pass it; a local request
 * needs none, so `code` is optional. Returns the session, including the profile
 * list for the picker.
 */
export async function pair(code) {
    const res = await post('/api/auth/pair', code ? { code } : {}, { auth: false });
    session.setSession(res);
    return res;
}

/** Switch profile. Rotates the refresh token and rescopes both tokens. */
export async function selectProfile(profileId) {
    const current = session.getSession();
    const res = await post('/api/auth/select-profile', {
        refresh_token: current?.refresh_token,
        profile_id: Number(profileId),
    }, { auth: false });
    session.setSession({ ...current, ...res });
    return res;
}

export async function signOut() {
    const current = session.getSession();
    try {
        if (current?.refresh_token) {
            await post('/api/auth/logout', { refresh_token: current.refresh_token });
        }
    } catch {
        // Revoking server-side is best effort. Dropping the local tokens is
        // what actually signs this device out, so it happens either way.
    }
    session.clearSession();
    window.location.assign('welcome.html');
}
