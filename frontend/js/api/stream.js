// The streaming seam: turn a media file's stream_url into bytes a <video>
// element can actually play.
//
// The server runs a decision cascade (direct play -> remux -> audio transcode
// -> full HLS transcode). We preflight it with /plan, then attach the right
// player: a native progressive <video src> for the copy paths, HLS.js for a
// full transcode. Most of a 4K HEVC / Atmos library will hit the transcode
// path for a browser advertising H.264/AAC, so the HLS path is the common one.
//
// Auth: /stream and /hls are loaded directly by the <video>/HLS machinery,
// which can't send an Authorization header, so they carry a file-bound,
// media-only "stream token" instead - in the URL for progressive playback, and
// as a bearer header injected per-request for HLS.js. See docs/api.md.

import { get } from './client.js';
import { apiUrl } from './transport.js';
import { getPrefs } from '../data/prefs.js';

// mediaBase turns "/api/media/42/stream" into "/api/media/42", so the plan,
// token and hls URLs all derive from the one field the library DTOs ship.
function mediaBase(streamUrl) {
    return streamUrl.replace(/\/stream$/, '');
}

// --- client capability probing -------------------------------------------
//
// What we advertise decides what the server sends. Over-claiming a codec the
// browser can't actually decode means the server direct-plays it and the
// viewer gets a black screen - the exact failure we're fixing. So a codec is
// only offered when the browser confirms it can both play it as a file and
// decode it through Media Source Extensions (the HLS transcode target).

const probe = document.createElement('video');
const canPlay = (type) => probe.canPlayType(type) !== '';
const mseOK = (type) =>
    typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported?.(type);
const decodes = (type) => canPlay(type) && mseOK(type);

const HEVC = 'video/mp4; codecs="hvc1.1.6.L93.B0"';
const AV1 = 'video/mp4; codecs="av01.0.08M.08"';
const AC3 = 'audio/mp4; codecs="ac-3"';
const EAC3 = 'audio/mp4; codecs="ec-3"';

/** The capability profile for THIS browser, as query params for /plan and /stream. */
export function capabilityQuery() {
    const prefs = getPrefs();
    const video = ['h264']; // baseline every target supports
    if (decodes(HEVC)) video.push('hevc');
    if (decodes(AV1)) video.push('av1');

    const audio = ['aac'];
    if (canPlay(AC3)) audio.push('ac3');
    if (canPlay(EAC3)) audio.push('eac3');

    const q = {
        video: video.join(','),
        audio: audio.join(','),
        containers: 'mp4',
    };
    // "auto" means no cap; a specific quality caps the ladder height.
    if (prefs.maxQuality && prefs.maxQuality !== 'auto') {
        q.max_resolution = Number(prefs.maxQuality) || 0;
    }
    return q;
}

function withQuery(path, query) {
    const qs = new URLSearchParams(query).toString();
    return apiUrl(qs ? `${path}?${qs}` : path);
}

/**
 * Prepare `video` to play the file behind `streamUrl` and return a controller
 * with `destroy()`. Rejects if playback can't be set up at all (the caller
 * shows the message). `onFatal` is called if a fatal error happens later, once
 * playback is already running.
 */
export async function attachStream(video, streamUrl, { onFatal } = {}) {
    const base = mediaBase(streamUrl);
    const query = capabilityQuery();

    // Preflight the decision, and mint the media-only token the URLs carry.
    const [plan, ticket] = await Promise.all([
        get(`${base}/plan`, { query }),
        get(`${base}/stream-token`),
    ]);
    const token = ticket?.token;
    if (!token) throw new Error('Could not authorize playback.');

    if (plan?.mode === 'video') {
        return attachHLS(video, base, query, token, onFatal);
    }
    return attachProgressive(video, base, query, token);
}

// Direct play / remux / audio transcode: raw bytes with HTTP range support, so
// native seeking just works. The token rides the URL since <video> can't send
// a header.
function attachProgressive(video, base, query, token) {
    video.src = withQuery(`${base}/stream`, { ...query, access_token: token });
    video.load();
    return {
        destroy() {
            video.removeAttribute('src');
            video.load();
        },
    };
}

// Full transcode: the server hands back an HLS playlist once the session is
// started. HLS.js fetches the playlist and segments itself, so we inject the
// stream token as a bearer header on each of its requests.
async function attachHLS(video, base, query, token, onFatal) {
    // hls.js is ~400KB; load it only when a transcode actually needs it, so the
    // home screen isn't paying for the player it hasn't opened yet.
    const { default: Hls } = await import('hls.js');

    const info = await get(`${base}/stream`, { query }); // {mode, playlist, decision}
    if (!info?.playlist) throw new Error('The server did not return a playlist.');
    const playlist = apiUrl(info.playlist);

    if (Hls.isSupported()) {
        const hls = new Hls({
            xhrSetup: (xhr) => xhr.setRequestHeader('Authorization', `Bearer ${token}`),
        });
        hls.on(Hls.Events.ERROR, (_evt, data) => {
            if (!data.fatal) return;
            // Network and media errors are often transient; try once to recover
            // before giving up, the way HLS.js recommends.
            if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                hls.startLoad();
            } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
                hls.recoverMediaError();
            } else {
                hls.destroy();
                onFatal?.('Playback failed.');
            }
        });
        hls.loadSource(playlist);
        hls.attachMedia(video);
        return { destroy() { hls.destroy(); } };
    }

    // Safari/iOS play HLS natively, but the token would have to ride every
    // segment URL (they're relative in the playlist, so they won't inherit it),
    // which means rewriting the manifest. This browser-first build doesn't do
    // that yet; every MSE-capable browser (Chrome, Firefox, Edge, desktop
    // Safari) takes the path above.
    throw new Error("This browser can't play this transcoded title yet.");
}
