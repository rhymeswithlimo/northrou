// The full-screen video player.
//
// Built fresh from <template> on open and removed on close, so only one <video>
// is ever live. It uses the native controls (seek bar, volume, fullscreen,
// captions menu) with a thin chrome layer for the back button and title over
// the top - deliberately not a hand-rolled control bar. Playback is set up by
// api/stream.js; this component owns the overlay, resume position, subtitle
// tracks, and progress reporting.

import { $ } from '../lib/dom.js';
import { get, getBlob } from '../api/client.js';
import { attachStream } from '../api/stream.js';
import { recordWatch } from '../data/library.js';

// How often to report playback position while playing. Progress is also flushed
// on pause, end, and close, so this only bounds how much a crash loses.
const PROGRESS_INTERVAL_MS = 10000;

let active = null; // the one open player, so a second open replaces the first

export const isPlayerOpen = () => active !== null;
export const closePlayer = () => active?.close();

/**
 * Open the player for a title.
 * @param {{streamUrl: string, title?: string,
 *          watch?: {kind: string, id: number}, startPosition?: number}} opts
 */
export async function openPlayer({ streamUrl, title, watch, startPosition = 0 } = {}) {
    if (!streamUrl) return;
    if (active) active.close();

    const el = $('#tpl-player').content.firstElementChild.cloneNode(true);
    const video = $('.player__video', el);
    const spinner = $('.player__spinner', el);
    const errBox = $('.player__error', el);
    $('.player__title', el).textContent = title ?? '';

    const restoreFocus = document.activeElement;
    const playerRoot = $('#player-root');
    let controller = null;
    let progressTimer = null;
    let lastReported = -1;
    let closed = false;
    // Everything behind the player (the detail modal is still open underneath)
    // is made inert while it's up, so focus and Tab can't reach it.
    let inerted = [];

    function report() {
        const dur = video.duration;
        if (!dur || Number.isNaN(dur) || !watch?.id) return;
        const pos = Math.floor(video.currentTime);
        if (pos === lastReported) return;
        lastReported = pos;
        recordWatch({ kind: watch.kind, id: watch.id, position: pos, duration: Math.floor(dur) })
            .catch(() => {});
    }

    function showError(msg) {
        spinner.classList.add('u-hidden');
        errBox.textContent = msg;
        errBox.classList.remove('u-hidden');
    }

    function close() {
        if (closed) return;
        closed = true;
        if (active === handle) active = null;
        clearInterval(progressTimer);
        report(); // flush final position
        video.pause();
        controller?.destroy?.();
        for (const t of video.querySelectorAll('track')) {
            if (t.src.startsWith('blob:')) URL.revokeObjectURL(t.src);
        }
        document.removeEventListener('keydown', onKey, true);
        for (const c of inerted) c.removeAttribute('inert');
        el.remove();
        document.body.classList.remove('is-player-open');
        if (typeof restoreFocus?.focus === 'function') restoreFocus.focus();
    }

    // The detail modal listens for keydown on document too, and it's still open
    // underneath. Claim Escape (close only the player, not both) and Tab (its
    // focus trap must not fight ours) in the capture phase so its handler never
    // sees them; every other key falls through to the native video controls.
    function onKey(e) {
        if (e.key === 'Escape') {
            e.stopImmediatePropagation();
            e.preventDefault();
            close();
        } else if (e.key === 'Tab') {
            e.stopImmediatePropagation();
        }
    }

    const handle = { close };
    active = handle;

    document.addEventListener('keydown', onKey, true); // capture: beat the detail modal to it
    el.addEventListener('click', (e) => { if (e.target.closest('[data-close]')) close(); });

    // Wire events before attaching so nothing early is missed.
    video.addEventListener('loadedmetadata', () => {
        // Resume, but not so near the end that it lands past the credits.
        if (startPosition > 5 && startPosition < video.duration - 15) {
            video.currentTime = startPosition;
        }
    }, { once: true });
    video.addEventListener('playing', () => spinner.classList.add('u-hidden'), { once: true });
    video.addEventListener('pause', report);
    video.addEventListener('ended', report);

    playerRoot.append(el);
    inerted = [...document.body.children].filter((c) => c !== playerRoot && !c.hasAttribute('inert'));
    for (const c of inerted) c.setAttribute('inert', '');
    document.body.classList.add('is-player-open');
    requestAnimationFrame(() => el.classList.add('is-open'));
    $('.player__close', el).focus(); // move focus off the (now inert) detail modal

    try {
        controller = await attachStream(video, streamUrl, { onFatal: showError });
    } catch (err) {
        if (!closed) showError(err?.message || 'Could not start playback.');
        return handle;
    }
    if (closed) { controller?.destroy?.(); return handle; }

    // Subtitles load in the background; a failure must not stop playback.
    loadSubtitles(video, streamUrl).catch(() => {});

    try { await video.play(); } catch { /* autoplay may be blocked; controls are shown */ }
    progressTimer = setInterval(report, PROGRESS_INTERVAL_MS);
    return handle;
}

// Attach every ready text track as a <track>. /api/images-style auth applies:
// the .vtt route needs a bearer header, so each track is fetched as a blob and
// handed to <track> as an object URL rather than pointed straight at the API.
async function loadSubtitles(video, streamUrl) {
    const base = streamUrl.replace(/\/stream$/, '');
    let tracks;
    try { tracks = await get(`${base}/subtitles`); } catch { return; }
    for (const t of tracks ?? []) {
        if (t.status !== 'ready' || !t.url) continue;
        let blob;
        try { blob = await getBlob(t.url); } catch { continue; }
        const track = document.createElement('track');
        track.kind = 'subtitles';
        track.label = t.label || t.language || 'Subtitles';
        if (t.language) track.srclang = t.language;
        track.src = URL.createObjectURL(blob);
        if (t.default) track.default = true;
        video.append(track);
    }
}
