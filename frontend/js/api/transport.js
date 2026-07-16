// How a request reaches the server.
//
// Two transports, one interface, because the client runs in two places:
//
//   browser  - served same-origin off the backend. Plain fetch.
//   tunnel   - a desktop/mobile app, which is never same-origin with the box,
//              so requests ride a WebRTC data channel to it (see tunnel.js).
//
// Which one is in play is a property of how this client was launched, not of
// any single call, so it is resolved once here and every caller just uses
// request(). A browser served off the box talks to it directly; an app always
// reaches it peer-to-peer through the coordinator.

import { openTunnel } from './tunnel.js';

/** Same-origin browser fetch. */
const browserTransport = () => (url, init) => fetch(url, init);

let transport = null;
let tunnel = null;
let mode = 'direct';

/**
 * The active transport. The tunnel installs its own via setTransport; this
 * default is the same-origin browser fetch, which is the only case that reaches
 * the server without a tunnel.
 */
export function getTransport() {
    if (!transport) transport = browserTransport();
    return transport;
}

/** Override the transport. Used by the connection bootstrap and by tests. */
export function setTransport(fn) {
    transport = fn;
}

/** 'direct' (same-origin browser) or 'tunnel' (peer-to-peer app). */
export const getMode = () => mode;

/**
 * Send every request over a peer-to-peer tunnel to the box that owns `code`.
 * The base URL becomes irrelevant: the data channel already terminates at the
 * server, so paths are sent as-is.
 */
export async function useTunnel({ coordUrl, code }) {
    tunnel = await openTunnel({ coordUrl, code });
    setBaseUrl('');
    setTransport((url, init) => tunnel.fetch(url, init));
    mode = 'tunnel';
    return tunnel;
}

/** Talk to the box directly, same-origin (a browser served off it). */
export function useDirect(base = '') {
    if (tunnel) {
        tunnel.close();
        tunnel = null;
    }
    setBaseUrl(base);
    transport = null; // re-resolve to the browser fetch
    mode = 'direct';
}

export function closeTunnel() {
    if (!tunnel) return;
    tunnel.close();
    tunnel = null;
    mode = 'direct';
}

/** Where the API lives. Always same-origin now: a browser hits the box it was
 * served from, and the tunnel terminates at the box so paths go as-is. */
let baseUrl = '';

export const getBaseUrl = () => baseUrl;
export const setBaseUrl = (url) => { baseUrl = (url ?? '').replace(/\/$/, ''); };

export const apiUrl = (path) => `${baseUrl}${path}`;
