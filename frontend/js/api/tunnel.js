// The client half of the peer-to-peer tunnel.
//
// When the box isn't on this network, requests can't just be fetched: there is
// no route to it. Instead the coordination server brokers a WebRTC handshake
// (signalling only, never media), the two peers hole-punch a direct connection,
// and ordinary HTTP rides a data channel straight to the house. See
// internal/remote/tunnel.go for the server half.
//
// This lives in JS rather than Rust on purpose. The WebView already has a
// WebRTC stack on every platform Tauri targets, `transport.js` already has a
// seam shaped exactly like this, and a JS client keeps working in a plain
// browser, which a Rust one would not.
//
// Wire format (must match internal/remote/tunnel.go exactly):
//   one data channel per request
//   frame = uint32 big-endian length + payload; length 0 terminates
//   -> frame 1: JSON {method, url, header, body}   (body is base64)
//   <- frame 1: JSON {status, header}
//   <- frame 2..n: body chunks
//   <- zero-length frame: EOF

const ICE_SERVERS = [{ urls: 'stun:stun.l.google.com:19302' }];
const SIGNAL_TIMEOUT_MS = 20000;
const REQUEST_TIMEOUT_MS = 30000;

/** Reassembles length-prefixed frames out of a message-oriented channel.
 *  Go writes the header and the payload separately, so a frame can arrive split
 *  across messages, and two frames can arrive in one. Buffer and parse. */
class FrameReader {
    constructor() {
        this.buf = new Uint8Array(0);
        this.queue = [];   // completed frames (Uint8Array | null for EOF)
        this.waiters = [];
    }

    push(chunk) {
        const next = new Uint8Array(this.buf.length + chunk.length);
        next.set(this.buf);
        next.set(chunk, this.buf.length);
        this.buf = next;
        this.#drain();
    }

    #drain() {
        for (;;) {
            if (this.buf.length < 4) return;
            const view = new DataView(this.buf.buffer, this.buf.byteOffset, 4);
            const len = view.getUint32(0, false); // big-endian

            if (len === 0) {
                this.buf = this.buf.subarray(4);
                this.#emit(null); // EOF frame
                continue;
            }
            if (this.buf.length < 4 + len) return; // frame still incomplete

            this.#emit(this.buf.subarray(4, 4 + len));
            this.buf = this.buf.subarray(4 + len);
        }
    }

    #emit(frame) {
        const waiter = this.waiters.shift();
        if (waiter) waiter.resolve(frame);
        else this.queue.push(frame);
    }

    fail(err) {
        for (const w of this.waiters) w.reject(err);
        this.waiters = [];
    }

    /** @returns {Promise<Uint8Array|null>} next frame, or null at EOF */
    next() {
        if (this.queue.length) return Promise.resolve(this.queue.shift());
        return new Promise((resolve, reject) => this.waiters.push({ resolve, reject }));
    }
}

/**
 * Write one frame as TWO messages: the 4-byte header, then the payload.
 *
 * This looks wasteful and isn't optional. A data channel is SCTP, which is
 * message-oriented, and the Go side reads it as a stream via
 * `io.ReadFull(r, hdr[:4])` -- a 4-byte read. pion returns io.ErrShortBuffer
 * when the read buffer is smaller than the message, so a single 4+n byte
 * message makes that read fail and the server drops the channel. Go's own
 * writeFrame emits a header write and a payload write, so mirroring it byte for
 * byte is what keeps both ends in step.
 */
function sendFrame(dc, payload) {
    const hdr = new Uint8Array(4);
    new DataView(hdr.buffer).setUint32(0, payload.length, false);
    dc.send(hdr);
    if (payload.length) dc.send(payload);
}

const toBase64 = (bytes) => {
    let s = '';
    for (const b of bytes) s += String.fromCharCode(b);
    return btoa(s);
};

/** Signalling: swap SDP and ICE with the box through the coordination server. */
function signal(coordUrl, code) {
    return new Promise((resolve, reject) => {
        let ws;
        try {
            ws = new WebSocket(coordUrl);
        } catch (e) {
            reject(new Error(`Could not reach the coordination server: ${e.message}`));
            return;
        }

        const timer = setTimeout(() => {
            ws.close();
            reject(new Error('The server did not answer. It may be offline.'));
        }, SIGNAL_TIMEOUT_MS);

        ws.addEventListener('open', () => {
            ws.send(JSON.stringify({ type: 'connect', role: 'client', code }));
        });
        ws.addEventListener('error', () => {
            clearTimeout(timer);
            reject(new Error('Could not reach the coordination server.'));
        });
        ws.addEventListener('message', (ev) => {
            const msg = JSON.parse(ev.data);
            if (msg.type === 'error') {
                clearTimeout(timer);
                ws.close();
                reject(new Error(msg.error || 'The coordination server rejected the code.'));
                return;
            }
            if (msg.type === 'paired') {
                clearTimeout(timer);
                resolve({ ws, session: msg.session });
            }
        });
    });
}

/**
 * Open a tunnel to the box that owns `code`.
 * @returns {Promise<{fetch: Function, close: Function, state: string}>}
 */
export async function openTunnel({ coordUrl, code }) {
    const { ws, session } = await signal(coordUrl, code);

    const pc = new RTCPeerConnection({ iceServers: ICE_SERVERS });

    pc.addEventListener('icecandidate', (e) => {
        // A null candidate means gathering finished; the server ignores it.
        if (!e.candidate) return;
        ws.send(JSON.stringify({
            type: 'candidate',
            session,
            candidate: JSON.stringify(e.candidate.toJSON()),
        }));
    });

    const connected = new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('Could not establish a direct connection.')), SIGNAL_TIMEOUT_MS);
        pc.addEventListener('connectionstatechange', () => {
            if (pc.connectionState === 'connected') {
                clearTimeout(timer);
                resolve();
            }
            if (pc.connectionState === 'failed') {
                clearTimeout(timer);
                reject(new Error('Could not establish a direct connection. The network may block peer-to-peer.'));
            }
        });
    });

    ws.addEventListener('message', async (ev) => {
        const msg = JSON.parse(ev.data);
        if (msg.type === 'answer') {
            await pc.setRemoteDescription({ type: 'answer', sdp: msg.sdp });
        } else if (msg.type === 'candidate' && msg.candidate) {
            try {
                await pc.addIceCandidate(JSON.parse(msg.candidate));
            } catch {
                // A candidate that arrives before the answer is set is normal;
                // ICE recovers from it.
            }
        }
    });

    // The offer needs an SCTP m-line or no data channel can ever be opened, and
    // that only appears if a channel exists when the offer is created. Per-
    // request channels are added afterwards and negotiate in-band, needing no
    // renegotiation.
    pc.createDataChannel('bootstrap');

    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    ws.send(JSON.stringify({ type: 'offer', session, sdp: offer.sdp }));

    await connected;

    /** One request over one data channel, matching internal/remote/tunnel.go. */
    async function tunnelFetch(url, init = {}) {
        const dc = pc.createDataChannel(`req-${Math.random().toString(36).slice(2)}`);
        dc.binaryType = 'arraybuffer';

        const reader = new FrameReader();
        let done = false;
        dc.addEventListener('message', (e) => reader.push(new Uint8Array(e.data)));
        dc.addEventListener('error', () => reader.fail(new Error('The connection to your server dropped.')));
        // ServeConn closes the channel the moment it has finished replying, so
        // a close after the EOF frame is success, not an error. Only a close
        // with a read still outstanding is a real failure.
        dc.addEventListener('close', () => {
            if (!done) reader.fail(new Error('The server closed the connection early.'));
        });

        await new Promise((resolve, reject) => {
            const timer = setTimeout(() => reject(new Error('Timed out opening a channel.')), REQUEST_TIMEOUT_MS);
            dc.addEventListener('open', () => { clearTimeout(timer); resolve(); });
        });

        try {
            // Go decodes `body` as []byte, which JSON-encodes as base64.
            const bodyBytes = init.body ? new TextEncoder().encode(init.body) : null;
            const header = {};
            for (const [k, v] of Object.entries(init.headers ?? {})) header[k] = [v];

            const path = url.startsWith('http') ? new URL(url).pathname + new URL(url).search : url;
            const env = {
                method: init.method ?? 'GET',
                url: path,
                header,
                ...(bodyBytes ? { body: toBase64(bodyBytes) } : {}),
            };
            sendFrame(dc, new TextEncoder().encode(JSON.stringify(env)));

            const head = await reader.next();
            if (!head) throw new Error('The server closed the connection without responding.');
            const { status, header: respHeader } = JSON.parse(new TextDecoder().decode(head));

            // Body frames until the zero-length terminator.
            const chunks = [];
            for (;;) {
                const chunk = await reader.next();
                if (chunk === null) break;
                chunks.push(chunk);
            }
            const total = chunks.reduce((n, c) => n + c.length, 0);
            const body = new Uint8Array(total);
            let at = 0;
            for (const c of chunks) { body.set(c, at); at += c.length; }

            // Rebuild a real Response so callers can't tell the difference
            // between this and fetch().
            const headers = new Headers();
            for (const [k, vs] of Object.entries(respHeader ?? {})) {
                for (const v of vs) headers.append(k, v);
            }
            done = true;
            return new Response(status === 204 || status === 304 ? null : body, { status, headers });
        } finally {
            done = true;
            dc.close();
        }
    }

    return {
        fetch: tunnelFetch,
        get state() { return pc.connectionState; },
        close() {
            pc.close();
            ws.close();
        },
    };
}
