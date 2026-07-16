// Social sign-in, client side.
//
// The box holds no OAuth secrets and the client holds none either. The flow is:
// ask the box which providers it offers, open the broker in a real browser, and
// hand the assertion it returns back to the box, which verifies the signature.
//
// The nonce is generated here and never leaves this device except inside the
// assertion, which is what stops one captured elsewhere being replayed into
// this session.

import { get, post } from '../api/client.js';

const NONCE_KEY = 'northrou.oauthNonce';

/** Which providers this server offers, if any. */
export async function getOAuthConfig() {
    try {
        return await get('/api/auth/oauth/config', { auth: false });
    } catch {
        // An older server, or one with social sign-in off. The pin always works.
        return { providers: [] };
    }
}

function newNonce() {
    const bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    const nonce = [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
    sessionStorage.setItem(NONCE_KEY, nonce);
    return nonce;
}

export const takeNonce = () => {
    const n = sessionStorage.getItem(NONCE_KEY);
    sessionStorage.removeItem(NONCE_KEY); // one flow, one nonce
    return n;
};

/** Where to send the user to authenticate. */
export function authUrl({ startUrl, provider, redirect }) {
    const q = new URLSearchParams({ nonce: newNonce(), redirect });
    return `${startUrl}/${provider}/start?${q}`;
}

/** Exchange the broker's assertion for a session on the box. */
export async function signInWithAssertion(assertion) {
    const nonce = takeNonce();
    if (!nonce) throw new Error('This sign-in did not start on this device.');
    return post('/api/auth/oauth/signin', { assertion, nonce }, { auth: false });
}

/** Reads the assertion the broker put in the URL fragment. */
export function readCallback() {
    const frag = window.location.hash.slice(1);
    if (!frag) return null;
    const vals = new URLSearchParams(frag);
    const assertion = vals.get('assertion');
    const error = vals.get('error');
    if (!assertion && !error) return null;

    // Clear it immediately: an assertion is a bearer credential and has no
    // business sitting in the address bar or in history.
    history.replaceState(null, '', window.location.pathname + window.location.search);
    return { assertion, error };
}
