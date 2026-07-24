// The full-screen video player.
//
// Built fresh from <template> on open and removed on close, so only one <video>
// is ever live. The transport UI (seek bar, play/pause, volume, subtitles +
// their settings, playback speed, fullscreen) is hand-built rather than the
// browser's native `controls`, so it looks and behaves identically on the web,
// the desktop shell (WKWebView on macOS, WebView2 on Windows), and mobile - and
// so subtitles can be sized/styled the way the viewer wants. Playback itself is
// set up by api/stream.js; this component owns the overlay, the controls, resume
// position, subtitle tracks, and progress reporting.

import { $, $$ } from '../lib/dom.js';
import { get, getBlob } from '../api/client.js';
import { attachStream } from '../api/stream.js';
import { recordWatch } from '../data/library.js';
import { getPrefs, setPref } from '../data/prefs.js';

// How often to report playback position while playing. Progress is also flushed
// on pause, end, and close, so this only bounds how much a crash loses.
const PROGRESS_INTERVAL_MS = 10000;
// Controls fade this long after the last pointer/key activity while playing.
const CONTROLS_HIDE_MS = 3200;
// Arrow/J/L jump distance, in seconds.
const SEEK_STEP = 10;

let active = null; // the one open player, so a second open replaces the first

export const isPlayerOpen = () => active !== null;
export const closePlayer = () => active?.close();

/** Format seconds as m:ss, or h:mm:ss past an hour. */
function fmtTime(sec) {
    if (!Number.isFinite(sec) || sec < 0) sec = 0;
    sec = Math.floor(sec);
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const s = sec % 60;
    const mm = h ? String(m).padStart(2, '0') : String(m);
    return (h ? `${h}:` : '') + `${mm}:${String(s).padStart(2, '0')}`;
}

const clamp = (n, lo, hi) => Math.min(hi, Math.max(lo, n));

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
    const subsEl = $('.player__subs', el);
    const seek = $('.player__seek', el);
    const playedBar = $('.player__seek-played', el);
    const bufferedBar = $('.player__seek-buffered', el);
    const seekKnob = $('.player__seek-knob', el);
    const curEl = $('.player__cur', el);
    const durEl = $('.player__dur', el);
    const volInput = $('.player__vol', el);
    const ccBtn = $('.player__cc', el);
    const capMenu = $('.player__menu--captions', el);
    const setMenu = $('.player__menu--settings', el);
    const shortcutsEl = $('.player__shortcuts', el);
    $('.player__title', el).textContent = title ?? '';

    const restoreFocus = document.activeElement;
    const playerRoot = $('#player-root');
    let controller = null;
    let progressTimer = null;
    let hideTimer = null;
    let lastReported = -1;
    let closed = false;
    let scrubbing = false;      // dragging the seek head
    let cueTrack = null;        // the TextTrack we render manually, or null
    // Everything behind the player (the detail modal is still open underneath)
    // is made inert while it's up, so focus and Tab can't reach it.
    let inerted = [];

    const prefs = getPrefs();

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

    // --- controls visibility ------------------------------------------------
    // Controls show on any activity and hide after a quiet spell while playing;
    // a paused or menu-open player keeps them up so nothing is stranded.
    function menuOpen() {
        return !capMenu.classList.contains('u-hidden')
            || !setMenu.classList.contains('u-hidden')
            || !shortcutsEl.classList.contains('u-hidden');
    }
    function showControls() {
        el.classList.add('is-active');
        clearTimeout(hideTimer);
        if (!video.paused && !menuOpen()) {
            hideTimer = setTimeout(hideControls, CONTROLS_HIDE_MS);
        }
    }
    function hideControls() {
        if (video.paused || menuOpen() || scrubbing) return;
        el.classList.remove('is-active');
        closeMenus();
    }

    function closeMenus() {
        capMenu.classList.add('u-hidden');
        setMenu.classList.add('u-hidden');
        shortcutsEl.classList.add('u-hidden');
        $$('.player__btn[aria-expanded]', el).forEach((b) => b.setAttribute('aria-expanded', 'false'));
    }

    // --- play / pause -------------------------------------------------------
    function togglePlay() {
        if (video.paused) video.play().catch(() => {});
        else video.pause();
    }

    function syncPlayButton() {
        el.classList.toggle('is-paused', video.paused);
        $('[data-action="play"]', el).setAttribute('aria-label', video.paused ? 'Play' : 'Pause');
    }

    // --- seeking ------------------------------------------------------------
    function seekTo(t) {
        if (Number.isFinite(video.duration)) {
            video.currentTime = clamp(t, 0, video.duration);
        }
    }
    function seekBy(delta) { seekTo(video.currentTime + delta); }

    function paintSeek(pct) {
        playedBar.style.width = `${pct}%`;
        seekKnob.style.left = `${pct}%`;
    }

    function updateProgress() {
        const dur = video.duration;
        const pct = Number.isFinite(dur) && dur > 0 ? (video.currentTime / dur) * 100 : 0;
        if (!scrubbing) paintSeek(pct);
        curEl.textContent = fmtTime(video.currentTime);
        seek.setAttribute('aria-valuenow', String(Math.round(pct)));
        seek.setAttribute('aria-valuetext', `${fmtTime(video.currentTime)} of ${fmtTime(dur)}`);
    }
    function updateBuffered() {
        const dur = video.duration;
        if (!Number.isFinite(dur) || dur <= 0 || !video.buffered.length) return;
        // The buffered range covering the playhead is what matters to the viewer.
        let end = 0;
        for (let i = 0; i < video.buffered.length; i++) {
            if (video.buffered.start(i) <= video.currentTime) end = video.buffered.end(i);
        }
        bufferedBar.style.width = `${clamp((end / dur) * 100, 0, 100)}%`;
    }

    // Map a clientX on the seek track to a media time.
    function seekFraction(clientX) {
        const r = seek.getBoundingClientRect();
        return clamp((clientX - r.left) / r.width, 0, 1);
    }
    seek.addEventListener('pointerdown', (e) => {
        if (!Number.isFinite(video.duration)) return;
        scrubbing = true;
        el.classList.add('is-scrubbing');
        seek.setPointerCapture(e.pointerId);
        const f = seekFraction(e.clientX);
        paintSeek(f * 100);
        curEl.textContent = fmtTime(f * video.duration);
        showControls();
    });
    seek.addEventListener('pointermove', (e) => {
        if (!scrubbing) return;
        const f = seekFraction(e.clientX);
        paintSeek(f * 100);
        curEl.textContent = fmtTime(f * video.duration);
    });
    const endScrub = (e) => {
        if (!scrubbing) return;
        scrubbing = false;
        el.classList.remove('is-scrubbing');
        seekTo(seekFraction(e.clientX) * video.duration);
        showControls();
    };
    seek.addEventListener('pointerup', endScrub);
    seek.addEventListener('pointercancel', () => { scrubbing = false; el.classList.remove('is-scrubbing'); });
    // Keyboard access on the focused scrubber (arrows are also handled globally,
    // but Home/End and a focused-slider affordance belong here).
    seek.addEventListener('keydown', (e) => {
        if (e.key === 'Home') { e.preventDefault(); seekTo(0); }
        else if (e.key === 'End') { e.preventDefault(); seekTo(video.duration); }
    });

    // --- volume -------------------------------------------------------------
    function applyVolume(v, { save = true } = {}) {
        video.volume = clamp(v, 0, 1);
        video.muted = video.volume === 0;
        volInput.value = String(video.muted ? 0 : video.volume);
        el.classList.toggle('is-muted', video.muted || video.volume === 0);
        if (save) {
            setPref('volume', video.volume);
            setPref('muted', video.muted);
        }
    }
    function toggleMute() {
        if (video.muted || video.volume === 0) {
            applyVolume(video.volume > 0 ? video.volume : (prefs.volume || 1) || 1);
            video.muted = false;
        } else {
            video.muted = true;
            volInput.value = '0';
            el.classList.add('is-muted');
            setPref('muted', true);
        }
    }
    volInput.addEventListener('input', () => applyVolume(Number(volInput.value)));

    // --- subtitles ----------------------------------------------------------
    // Tracks are added by loadSubtitles(). We render cues ourselves (each active
    // track runs in 'hidden' mode) so size/background follow the viewer's
    // settings rather than the browser default.
    function renderCues() {
        if (!cueTrack || !cueTrack.activeCues) { subsEl.replaceChildren(); return; }
        const frag = document.createDocumentFragment();
        for (const cue of cueTrack.activeCues) {
            const line = document.createElement('span');
            line.className = 'player__sub-line';
            // VTT cue text may carry simple markup; keep it plain and safe.
            line.textContent = cue.text.replace(/<[^>]+>/g, '');
            frag.append(line);
        }
        subsEl.replaceChildren(frag);
    }

    function selectTrack(track) {
        // Turn everything off, then activate the chosen one in hidden mode.
        for (const t of video.textTracks) t.mode = 'disabled';
        if (cueTrack) cueTrack.oncuechange = null;
        cueTrack = track ?? null;
        if (cueTrack) {
            cueTrack.mode = 'hidden';
            cueTrack.oncuechange = renderCues;
        }
        renderCues();
        el.classList.toggle('has-subs', !!cueTrack);
        ccBtn.classList.toggle('is-on', !!cueTrack);
        setPref('subtitleLang', cueTrack ? (cueTrack.language || cueTrack.label || 'on') : '');
        buildCaptionsMenu();
    }

    function trackList() {
        return [...video.textTracks].filter((t) => t.kind === 'subtitles' || t.kind === 'captions');
    }

    function buildCaptionsMenu() {
        const tracks = trackList();
        ccBtn.hidden = tracks.length === 0;
        const rows = [{ label: 'Off', track: null }].concat(
            tracks.map((t) => ({ label: t.label || t.language || 'Subtitles', track: t })));
        capMenu.replaceChildren(...rows.map(({ label, track }) => {
            const b = document.createElement('button');
            b.type = 'button';
            b.className = 'player__menu-item';
            b.textContent = label;
            b.setAttribute('role', 'menuitemradio');
            const on = track === cueTrack;
            b.setAttribute('aria-checked', String(on));
            b.classList.toggle('is-on', on);
            b.addEventListener('click', () => { selectTrack(track); closeMenus(); showControls(); });
            return b;
        }));
    }

    // Restore the viewer's last subtitle choice once tracks exist.
    function applySubtitlePref() {
        const want = prefs.subtitleLang;
        if (!want) { buildCaptionsMenu(); return; }
        const match = trackList().find((t) => (t.language || t.label) === want);
        if (match) selectTrack(match);
        else buildCaptionsMenu();
    }

    function toggleCaptions() {
        const tracks = trackList();
        if (!tracks.length) return;
        selectTrack(cueTrack ? null : tracks[0]);
        showControls();
    }

    // --- settings (subtitle size / background / speed) ----------------------
    function applySubSize(size, { save = true } = {}) {
        el.dataset.subsize = size;
        markSeg(setMenu, 'subsize', size);
        if (save) setPref('subtitleSize', size);
    }
    function applySubBg(on, { save = true } = {}) {
        el.classList.toggle('sub-nobg', !on);
        markSeg(setMenu, 'subbg', on ? 'on' : 'off');
        if (save) setPref('subtitleBackground', on);
    }
    function applySpeed(rate) {
        video.playbackRate = rate;
        markSeg(setMenu, 'speed', String(rate));
    }
    function markSeg(root, group, value) {
        const attr = { subsize: 'data-subsize', subbg: 'data-subbg', speed: 'data-speed' }[group];
        $$(`[data-group="${group}"] button`, root).forEach((b) => {
            b.classList.toggle('is-on', b.getAttribute(attr) === value);
        });
    }

    setMenu.addEventListener('click', (e) => {
        const b = e.target.closest('button');
        if (!b) return;
        if (b.dataset.subsize) applySubSize(b.dataset.subsize);
        else if (b.dataset.subbg) applySubBg(b.dataset.subbg === 'on');
        else if (b.dataset.speed) applySpeed(Number(b.dataset.speed));
    });

    function toggleMenu(which) {
        const menu = which === 'captions' ? capMenu : which === 'settings' ? setMenu : shortcutsEl;
        const wasOpen = !menu.classList.contains('u-hidden');
        closeMenus();
        if (!wasOpen) {
            menu.classList.remove('u-hidden');
            const btn = $(`[data-action="${which}"]`, el);
            btn?.setAttribute('aria-expanded', 'true');
        }
        showControls();
    }

    // --- fullscreen ---------------------------------------------------------
    function toggleFullscreen() {
        if (document.fullscreenElement) {
            document.exitFullscreen?.().catch(() => {});
        } else {
            (el.requestFullscreen?.() ?? Promise.reject()).catch(() => {
                // Safari/WKWebView expose it on the video element instead.
                video.webkitEnterFullscreen?.();
            });
        }
    }
    document.addEventListener('fullscreenchange', onFsChange);
    function onFsChange() {
        el.classList.toggle('is-fullscreen', document.fullscreenElement === el);
    }

    // --- control-bar clicks -------------------------------------------------
    const ACTIONS = {
        play: togglePlay,
        back10: () => seekBy(-SEEK_STEP),
        fwd10: () => seekBy(SEEK_STEP),
        mute: toggleMute,
        captions: () => toggleMenu('captions'),
        settings: () => toggleMenu('settings'),
        fullscreen: toggleFullscreen,
    };
    el.addEventListener('click', (e) => {
        if (e.target.closest('[data-close]')) { close(); return; }
        const btn = e.target.closest('[data-action]');
        if (btn) { ACTIONS[btn.dataset.action]?.(); showControls(); return; }
        // A click on the stage (the video area, not a control) toggles play.
        if (e.target.closest('.player__stage')) {
            if (menuOpen()) { closeMenus(); return; }
            togglePlay();
        }
    });

    // Wake the controls on any pointer/keyboard activity.
    el.addEventListener('pointermove', showControls);
    el.addEventListener('pointerdown', showControls);

    // --- keyboard -----------------------------------------------------------
    // The detail modal listens for keydown on document too, and it's still open
    // underneath. Claim the keys we use in the capture phase so its focus trap
    // and Escape handler never see them; the rest fall through.
    function onKey(e) {
        // Let a focused range/slider handle its own arrows.
        const typing = e.target instanceof HTMLInputElement && e.target.type === 'range';

        switch (e.key) {
            case 'Escape':
                e.stopImmediatePropagation();
                e.preventDefault();
                if (menuOpen()) closeMenus();
                else if (document.fullscreenElement === el) document.exitFullscreen?.();
                else close();
                return;
            case 'Tab':
                // Keep the modal's focus trap from fighting ours.
                e.stopImmediatePropagation();
                return;
            case ' ':
            case 'k': case 'K':
                e.preventDefault(); togglePlay(); showControls(); return;
            case 'ArrowLeft': case 'j': case 'J':
                if (typing && e.key === 'ArrowLeft') return;
                e.preventDefault(); seekBy(-SEEK_STEP); showControls(); return;
            case 'ArrowRight': case 'l': case 'L':
                if (typing && e.key === 'ArrowRight') return;
                e.preventDefault(); seekBy(SEEK_STEP); showControls(); return;
            case 'ArrowUp':
                if (typing) return;
                e.preventDefault(); applyVolume((video.muted ? 0 : video.volume) + 0.1); showControls(); return;
            case 'ArrowDown':
                if (typing) return;
                e.preventDefault(); applyVolume((video.muted ? 0 : video.volume) - 0.1); showControls(); return;
            case 'm': case 'M':
                e.preventDefault(); toggleMute(); showControls(); return;
            case 'f': case 'F':
                e.preventDefault(); toggleFullscreen(); return;
            case 'c': case 'C':
                e.preventDefault(); toggleCaptions(); return;
            case '?':
                e.preventDefault(); toggleMenu('shortcuts'); return;
        }
        // Number keys jump to 0-90% of the runtime.
        if (/^[0-9]$/.test(e.key) && Number.isFinite(video.duration)) {
            e.preventDefault();
            seekTo((Number(e.key) / 10) * video.duration);
            showControls();
        }
    }

    function close() {
        if (closed) return;
        closed = true;
        if (active === handle) active = null;
        clearInterval(progressTimer);
        clearTimeout(hideTimer);
        report(); // flush final position
        video.pause();
        controller?.destroy?.();
        for (const t of video.querySelectorAll('track')) {
            if (t.src.startsWith('blob:')) URL.revokeObjectURL(t.src);
        }
        document.removeEventListener('keydown', onKey, true);
        document.removeEventListener('fullscreenchange', onFsChange);
        if (document.fullscreenElement === el) document.exitFullscreen?.().catch(() => {});
        for (const c of inerted) c.removeAttribute('inert');
        el.remove();
        document.body.classList.remove('is-player-open');
        if (typeof restoreFocus?.focus === 'function') restoreFocus.focus();
    }

    const handle = { close };
    active = handle;

    document.addEventListener('keydown', onKey, true); // capture: beat the detail modal to it

    // Wire media events before attaching so nothing early is missed.
    video.addEventListener('loadedmetadata', () => {
        durEl.textContent = fmtTime(video.duration);
        updateProgress();
        // Resume, but not so near the end that it lands past the credits.
        if (startPosition > 5 && startPosition < video.duration - 15) {
            video.currentTime = startPosition;
        }
    }, { once: true });
    video.addEventListener('timeupdate', updateProgress);
    video.addEventListener('progress', updateBuffered);
    video.addEventListener('durationchange', () => { durEl.textContent = fmtTime(video.duration); });
    video.addEventListener('play', () => { syncPlayButton(); showControls(); });
    video.addEventListener('pause', () => { syncPlayButton(); report(); showControls(); });
    video.addEventListener('ended', report);
    video.addEventListener('waiting', () => spinner.classList.remove('u-hidden'));
    video.addEventListener('playing', () => { spinner.classList.add('u-hidden'); showControls(); });
    video.addEventListener('canplay', () => spinner.classList.add('u-hidden'));

    playerRoot.append(el);
    inerted = [...document.body.children].filter((c) => c !== playerRoot && !c.hasAttribute('inert'));
    for (const c of inerted) c.setAttribute('inert', '');
    document.body.classList.add('is-player-open');

    // Apply persisted settings up front.
    applyVolume(prefs.muted ? 0 : (prefs.volume ?? 1), { save: false });
    if (prefs.muted) { video.muted = true; el.classList.add('is-muted'); }
    applySubSize(prefs.subtitleSize ?? 'md', { save: false });
    applySubBg(prefs.subtitleBackground !== false, { save: false });
    markSeg(setMenu, 'speed', '1');
    syncPlayButton();

    requestAnimationFrame(() => { el.classList.add('is-open'); showControls(); });
    $('.player__close', el).focus(); // move focus off the (now inert) detail modal

    try {
        controller = await attachStream(video, streamUrl, { onFatal: showError });
    } catch (err) {
        if (!closed) showError(err?.message || 'Could not start playback.');
        return handle;
    }
    if (closed) { controller?.destroy?.(); return handle; }

    // Subtitles load in the background; a failure must not stop playback. Build
    // the picker and restore the saved choice once the tracks exist.
    loadSubtitles(video, streamUrl)
        .then(() => { buildCaptionsMenu(); applySubtitlePref(); })
        .catch(() => {});

    try { await video.play(); } catch { /* autoplay may be blocked; controls are shown */ }
    progressTimer = setInterval(report, PROGRESS_INTERVAL_MS);
    return handle;
}

// Attach every ready text track as a <track>. /api/images-style auth applies:
// the .vtt route needs a bearer header, so each track is fetched as a blob and
// handed to <track> as an object URL rather than pointed straight at the API.
// Tracks are added disabled; the player activates one itself (it renders cues
// manually, so it never leaves a track in the browser's own 'showing' mode).
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
        video.append(track);
        track.track.mode = 'disabled';
    }
}
