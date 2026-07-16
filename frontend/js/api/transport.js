// How a request reaches the server.
//
// Three transports, one interface, because the client runs in three places:
//
//   browser  - served same-origin off the backend. Plain fetch.
//   tauri    - desktop/mobile app talking to a box on another machine. That is
//              cross-origin, so it goes through the Rust HTTP plugin rather
//              than the WebView's fetch, which CORS would block.
//   tunnel   - the box is off-LAN and only reachable peer-to-peer, so requests
//              ride the WebRTC data channel (see js/api/tunnel.js).
//
// Which one is in play is a property of how this client was launched and where
// its server is, not of any single call, so it is resolved once here and every
// caller just uses request().

import { openTunnel } from './tunnel.js';

const isTauri = () => typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;

/** Same-origin browser fetch. */
const browserTransport = () => (url, init) => fetch(url, init);

/**
 * Tauri: route through the Rust side so a remote box isn't blocked by CORS.
 * Falls back to fetch if the plugin isn't present, which keeps `tauri dev`
 * against a local server working before the plugin is wired.
 */
function tauriTransport() {
    let pluginFetch = null;
    const load = async () => {
        if (pluginFetch) return pluginFetch;
        try {
            ({ fetch: pluginFetch } = await import('@tauri-apps/plugin-http'));
        } catch {
            pluginFetch = fetch;
        }
        return pluginFetch;
    };
    return async (url, init) => (await load())(url, init);
}

let transport = null;
let tunnel = null;
let mode = 'direct';

/** The active transport. Resolved once, then reused. */
export function getTransport() {
    if (!transport) transport = isTauri() ? tauriTransport() : browserTransport();
    return transport;
}

/** Override the transport. Used by the connection bootstrap and by tests. */
export function setTransport(fn) {
    transport = fn;
}

/** 'direct' (same-origin or LAN) or 'tunnel' (peer-to-peer). */
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

/** Send every request straight to `base` (same-origin or a LAN address). */
export function useDirect(base = '') {
    if (tunnel) {
        tunnel.close();
        tunnel = null;
    }
    setBaseUrl(base);
    transport = null; // re-resolve: browser fetch or the Tauri plugin
    mode = 'direct';
}

export function closeTunnel() {
    if (!tunnel) return;
    tunnel.close();
    tunnel = null;
    mode = 'direct';
}

/** Where the API lives. Same-origin in a browser; an absolute base in an app. */
let baseUrl = '';

export const getBaseUrl = () => baseUrl;
export const setBaseUrl = (url) => { baseUrl = (url ?? '').replace(/\/$/, ''); };

export const apiUrl = (path) => `${baseUrl}${path}`;
