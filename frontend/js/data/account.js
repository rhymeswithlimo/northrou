// Account/session seam.
//
// Note on `admin`: it means THE CURRENT TOKEN IS ALREADY ELEVATED, not "this
// profile may administer". Every profile may administer, because admin is gated
// on an emailed OTP rather than identity. So the Server admin section is shown
// to everyone; `admin` only decides whether the OTP prompt can be skipped.

import { get, post } from '../api/client.js';
import * as session from '../api/session.js';

export async function getMe() {
    return get('/api/me');
}

/** Set the current profile's preferred audio/subtitle languages. */
export async function setMyLanguage({ audio, subtitle }) {
    return post('/api/me/language', { audio, subtitle });
}

export async function requestPin(email) {
    // Always 200, even for a wrong address: the server refuses to confirm which
    // email an install belongs to.
    return post('/api/auth/request-pin', { email }, { auth: false });
}

export async function verifyPin(email, pin) {
    const res = await post('/api/auth/verify-pin', { email, pin }, { auth: false });
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
    session.clearElevation();
    window.location.assign('login.html');
}
