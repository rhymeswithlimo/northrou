// How a request reaches the server.
//
// Three transports, one interface, because the client runs in three places:
//
//   browser  - served same-origin off the backend. Plain fetch.
//   tauri    - desktop/mobile app talking to a box on another machine. That is
//              cross-origin, so it goes through the Rust HTTP plugin rather
//              than the WebView's fetch, which CORS would block.
//   tunnel   - the box is off-LAN and only reachable peer-to-peer, so requests
//              ride the WebRTC data channel (see internal/remote/tunnel.go).
//
// Picking the transport is a deploy-time fact, not a per-call one, so it is
// resolved once here and every caller just uses request().

const isTauri = () => typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;

/** Same-origin browser fetch. */
function browserTransport() {
    return (url, init) => fetch(url, init);
}

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

/** The active transport. Resolved once, then reused. */
export function getTransport() {
    if (!transport) transport = isTauri() ? tauriTransport() : browserTransport();
    return transport;
}

/**
 * Override the transport. Used by the connection bootstrap to switch to the
 * WebRTC tunnel once a peer is established, and by tests.
 */
export function setTransport(fn) {
    transport = fn;
}

/** Where the API lives. Same-origin in a browser; an absolute base in an app. */
let baseUrl = '';

export const getBaseUrl = () => baseUrl;
export const setBaseUrl = (url) => { baseUrl = url.replace(/\/$/, ''); };

export const apiUrl = (path) => `${baseUrl}${path}`;
