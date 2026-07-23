// Settings.

import { $, show, hide, reveal } from '../lib/dom.js';
import { segmented, toggle } from '../components/controls.js';
import { toast, mountOfflineBanner, statePanel } from '../components/states.js';
import { getMe, signOut, setMyLanguage } from '../data/account.js';
import { listProfiles, createProfile, renameProfile, deleteProfile, MAX_PROFILES } from '../data/profiles.js';
import { getPrefs, setPref, QUALITY_OPTIONS, CELLULAR_OPTIONS } from '../data/prefs.js';
import * as admin from '../data/admin.js';
import { isSignedIn } from '../api/session.js';
import { requireServer } from '../api/connect.js';
import { getServer, isSameOrigin } from '../data/servers.js';

// Admin actions are only available from a local connection (a browser on the
// server's own network, or the CLI); remote apps get reads only. The exact
// wording shown when locked.
const ADMIN_LOCAL_ONLY = 'Server settings can only be changed on your home network or with the northrou CLI.';

const TILES = ['#3c89e0', '#19ad31', '#d4412e', '#da3ce0', '#e0a13c', '#3cd6e0'];
const tileFor = (id) => TILES[(id - 1) % TILES.length];

// Languages offered for audio/subtitle preference. Codes mirror the server's
// internal/language table; English leads because it is the default.
const LANGUAGES = [
    ['en', 'English'], ['es', 'Spanish'], ['fr', 'French'], ['de', 'German'],
    ['it', 'Italian'], ['pt', 'Portuguese'], ['nl', 'Dutch'], ['sv', 'Swedish'],
    ['no', 'Norwegian'], ['da', 'Danish'], ['fi', 'Finnish'], ['pl', 'Polish'],
    ['cs', 'Czech'], ['ru', 'Russian'], ['uk', 'Ukrainian'], ['tr', 'Turkish'],
    ['ar', 'Arabic'], ['he', 'Hebrew'], ['hi', 'Hindi'], ['th', 'Thai'],
    ['ja', 'Japanese'], ['ko', 'Korean'], ['zh', 'Chinese'],
];

/** Populate a <select> with the language list and wire a save-on-change. */
function languageSelect(el, value, onChange) {
    el.replaceChildren();
    for (const [code, name] of LANGUAGES) {
        const opt = document.createElement('option');
        opt.value = code;
        opt.textContent = name;
        el.appendChild(opt);
    }
    el.value = LANGUAGES.some(([c]) => c === value) ? value : 'en';
    el.addEventListener('change', () => onChange(el.value));
}

$('#back').addEventListener('click', () => history.back());
mountOfflineBanner();

if (!(await requireServer())) throw new Error('no server');
if (!isSignedIn()) window.location.replace('welcome.html');

let me = null;
let profiles = [];
let config = null;

/* ==================== profiles ==================== */

function renderProfiles() {
    const list = $('#profiles-list');
    list.replaceChildren(...profiles.map((p) => {
        const node = $('#tpl-profile-row').content.firstElementChild.cloneNode(true);
        const avatar = $('.setting__avatar', node);
        avatar.style.setProperty('--tile', tileFor(p.id));
        avatar.textContent = p.name.charAt(0).toUpperCase();
        $('.setting__name', node).textContent = p.name;
        if (p.id === me.profile.id) show($('.setting__you', node));

        $('[data-edit]', node).addEventListener('click', () => onRename(p));

        const del = $('[data-delete]', node);
        // Never leave zero profiles. The API returns 409 for the last one, so
        // don't offer the button at all.
        if (profiles.length <= 1) del.remove();
        else del.addEventListener('click', () => onDelete(p));

        return node;
    }));

    $('#add-profile').disabled = profiles.length >= MAX_PROFILES;
    $('#profiles-note').textContent =
        'Anyone can add or rename a profile. Each has its own watch history and recommendations. '
        + `Deleting one is only possible on your home network. Maximum of ${MAX_PROFILES} profiles.`;
}

async function onRename(profile) {
    const name = prompt('Profile name', profile.name)?.trim();
    if (!name || name === profile.name) return;
    try {
        await renameProfile(profile.id, name);
        profiles = await listProfiles();
        if (profile.id === me.profile.id) {
            me.profile.name = name;
            $('#current-profile').textContent = name;
        }
        renderProfiles();
        toast(`Renamed to ${name}.`);
    } catch {
        toast('Could not rename the profile.', { variant: 'error' });
    }
}

async function onDelete(profile) {
    // Deleting takes that viewer's whole history with it, so it is an admin
    // action: only allowed on a local connection. Say so before the confirm.
    if (!me.admin) {
        toast(ADMIN_LOCAL_ONLY, { variant: 'error' });
        return;
    }
    if (!confirm(`Delete ${profile.name}? Their watch history and recommendations go too.`)) return;

    try {
        await deleteProfile(profile.id);
        profiles = await listProfiles();
        renderProfiles();
        toast(`Deleted ${profile.name}.`);
    } catch (err) {
        if (err.isConflict) toast('That is the last profile; there must always be one.', { variant: 'error' });
        else if (err.isForbidden) toast(ADMIN_LOCAL_ONLY, { variant: 'error' });
        else toast('Could not delete the profile.', { variant: 'error' });
    }
}

/* ==================== playback ==================== */

function renderPlayback() {
    const prefs = getPrefs();

    segmented($('#quality-max'), QUALITY_OPTIONS, prefs.maxQuality, (v) => {
        setPref('maxQuality', v);
        toast('Playback quality saved.');
    });
    segmented($('#quality-cellular'), CELLULAR_OPTIONS, prefs.cellularQuality, (v) => {
        setPref('cellularQuality', v);
        toast('Cellular quality saved.');
    });
    toggle($('#direct-play'), prefs.directPlay, (v) => {
        setPref('directPlay', v);
        toast(v ? 'Direct play on.' : 'Direct play off.');
    });
}

/* ==================== language (per profile) ==================== */

function renderLanguage() {
    const prof = me?.profile ?? {};
    const save = async () => {
        try {
            await setMyLanguage({
                audio: $('#audio-lang').value,
                subtitle: $('#subtitle-lang').value,
            });
            toast('Language saved.');
        } catch (err) {
            toast(err.body?.error ?? 'Could not save language.', { variant: 'error' });
        }
    };
    languageSelect($('#audio-lang'), prof.preferred_audio_lang ?? 'en', save);
    languageSelect($('#subtitle-lang'), prof.preferred_subtitle_lang ?? 'en', save);
}

/* ==================== about ==================== */

async function renderAbout() {
    const btn = $('#check-update');
    try {
        const info = await admin.checkUpdate();
        $('#version').textContent = info.current ? `v${info.current}` : 'Version unavailable';
        if (info.update_available) {
            btn.textContent = `Update to v${info.latest}`;
            btn.classList.replace('btn--quiet', 'btn--primary');
            btn.onclick = onApplyUpdate;
        } else {
            btn.onclick = async () => {
                btn.disabled = true;
                await renderAbout();
                btn.disabled = false;
                toast('You are on the latest version.');
            };
        }
    } catch {
        $('#version').textContent = 'Version unavailable';
    }
}

async function onApplyUpdate() {
    if (!me.admin) {
        toast(ADMIN_LOCAL_ONLY, { variant: 'error' });
        return;
    }
    if (!confirm('Install the update and restart the server?')) return;
    try {
        await admin.applyUpdate();
        toast('Update started. The server will restart.');
    } catch {
        toast('Could not install the update.', { variant: 'error' });
    }
}

/* ==================== server admin ==================== */

// Shown to a remote (non-local) session in place of the editable controls.
function renderAdminLocked() {
    const note = $('#admin-locked-note');
    note.hidden = false;
    note.textContent = ADMIN_LOCAL_ONLY;
    $('#admin-state').dataset.state = 'locked';
    $('#admin-state').textContent = 'Local-only';
}

async function renderAdmin() {
    const panel = $('#admin-panel');
    panel.hidden = false;
    hide($('#admin-locked-note'));
    $('#admin-state').dataset.state = 'unlocked';
    $('#admin-state').textContent = 'On this network';

    let hw, scan;
    try {
        [config, hw, scan] = await Promise.all([
            admin.getConfig(), admin.getHardware(), admin.getScan(),
        ]);
    } catch {
        panel.replaceChildren(statePanel({
            variant: 'error',
            title: "Couldn't load server status",
            body: 'The server did not respond. It may be restarting.',
            action: { label: 'Try again', onClick: renderAdmin },
        }));
        return;
    }

    const node = $('#tpl-admin').content.firstElementChild.cloneNode(true);
    panel.replaceChildren(node);

    // ---- server
    $('#server-name', node).textContent = config.server_name || 'This server';
    $('#server-address', node).textContent = window.location.host || 'local';
    $('#server-status', node).dataset.state = 'connected';
    $('#server-status-text', node).textContent = 'Connected';

    // ---- library
    $('#scan-note', node).textContent = scan?.running
        ? 'Scanning now...'
        : (scan?.last_scan ? `Last scan: ${scan.last_scan}.` : 'No scan has run yet.');

    renderUnmatched(node);

    $('#scan-now', node).addEventListener('click', async (e) => {
        e.target.disabled = true;
        try {
            await admin.startScan();
            toast('Scan started.');
        } catch (err) {
            if (err.isForbidden) toast(ADMIN_LOCAL_ONLY, { variant: 'error' });
            // The server has no folders configured yet. That is fixed on the
            // server, not here, so pass its instruction through verbatim.
            else if (err.isBadRequest) toast(err.body?.error ?? 'Could not start the scan.', { variant: 'error' });
            else toast('Could not start the scan.', { variant: 'error' });
        } finally {
            e.target.disabled = false;
        }
    });

    // ---- streaming
    $('#hwaccel', node).textContent = hw.ffmpeg_ready
        ? (hw.available?.length ? hw.available.join(', ') : 'None detected, using software')
        : 'ffmpeg is still downloading';
    $('#hwaccel-state', node).textContent = hw.backend && hw.backend !== 'software' ? 'On' : 'Off';
    $('#capacity-note', node).textContent =
        `Auto uses ${hw.estimated_capacity} on this hardware. Direct play and remux never count.`;

    segmented($('#transcode-cap', node), [
        { value: '0', label: 'Auto' },
        { value: '1', label: '1' },
        { value: '2', label: '2' },
        { value: '4', label: '4' },
    ], String(config.max_transcodes ?? 0), (v) => saveConfig({ max_transcodes: Number(v) }));

    // ---- advanced
    segmented($('#ffmpeg-mode', node), [
        { value: 'managed', label: 'Managed' },
        { value: 'system', label: 'System' },
    ], config.prefer_system_ffmpeg ? 'system' : 'managed',
    (v) => saveConfig({ prefer_system_ffmpeg: v === 'system' }));

    toggle($('#remote-access', node), config.remote_enabled, (v) => saveConfig({ remote_enabled: v }));

    renderTmdbKey(node);

    // ---- devices & access
    $('#connection-code-value', node).textContent = config.connection_code || 'None yet';
    $('#rotate-code', node).addEventListener('click', onRotateCode);
    renderDevices(node);

    $('#switch-server', node).addEventListener('click', () => window.location.assign('welcome.html'));
    $('#forget-server', node).addEventListener('click', () => {
        if (!confirm('Forget this server? You will need its connection code to pair again.')) return;
        signOut();
    });
}

/* ==================== TMDB key ==================== */

// The TMDB key is write-only: the server reports only whether one is set
// (has_tmdb_key), never the key itself. So this offers "set/replace" and, when
// one exists, "remove" - it never shows the current value.
function renderTmdbKey(root) {
    const input = $('#tmdb-key', root);
    const save = $('#tmdb-save', root);
    const remove = $('#tmdb-remove', root);
    const desc = $('#tmdb-desc', root);

    const reflect = () => {
        const has = !!config.has_tmdb_key;
        remove.hidden = !has;
        input.placeholder = has ? 'A key is set — paste a new one to replace it' : 'Paste your TMDB API key';
        desc.textContent = has
            ? 'A key is set. It fetches posters, descriptions, and artwork, and stays on this server.'
            : 'Add a key to fetch posters, descriptions, and artwork. Free from themoviedb.org; it stays on this server.';
    };
    reflect();

    save.addEventListener('click', async () => {
        const key = input.value.trim();
        if (!key) {
            toast('Paste a key first.', { variant: 'error' });
            return;
        }
        save.disabled = true;
        try {
            config = await admin.patchConfig({ tmdb_api_key: key });
            input.value = '';
            reflect();
            toast('TMDB key saved. Run a scan to fill in missing artwork.');
        } catch (err) {
            toast(err.isForbidden ? ADMIN_LOCAL_ONLY : 'Could not save the key.', { variant: 'error' });
        } finally {
            save.disabled = false;
        }
    });

    remove.addEventListener('click', async () => {
        if (!confirm("Remove the TMDB key? New scans won't fetch posters or descriptions until you add one again.")) return;
        try {
            config = await admin.patchConfig({ tmdb_api_key: '' });
            input.value = '';
            reflect();
            toast('TMDB key removed.');
        } catch (err) {
            toast(err.isForbidden ? ADMIN_LOCAL_ONLY : 'Could not remove the key.', { variant: 'error' });
        }
    });
}

/* ==================== devices & access ==================== */

// A rough "how long ago" for the paired-devices list.
function agoText(iso) {
    const t = Date.parse(iso);
    if (Number.isNaN(t)) return 'unknown';
    const mins = Math.floor((Date.now() - t) / 60000);
    if (mins < 1) return 'just now';
    if (mins < 60) return `${mins}m ago`;
    if (mins < 24 * 60) return `${Math.floor(mins / 60)}h ago`;
    return `${Math.floor(mins / (24 * 60))}d ago`;
}

async function renderDevices(root) {
    const list = $('#devices-list', root);
    const note = $('#devices-note', root);
    let devices = [];
    try {
        devices = await admin.getSessions();
    } catch {
        note.textContent = 'Could not load the device list.';
        return;
    }
    if (!devices.length) {
        note.textContent = 'No devices are paired yet.';
        list.replaceChildren();
        return;
    }
    note.textContent = `${devices.length} device${devices.length === 1 ? '' : 's'} can watch this library.`;
    list.replaceChildren(...devices.map((d) => {
        const li = document.createElement('li');
        li.className = 'devices__row';

        const text = document.createElement('div');
        text.className = 'devices__text';
        const name = document.createElement('p');
        name.className = 'setting__name';
        name.textContent = d.device_name || 'Unknown device';
        const desc = document.createElement('p');
        desc.className = 'setting__desc';
        desc.textContent = `${d.profile_name || 'No profile'} · last seen ${agoText(d.last_seen_at)}`;
        text.append(name, desc);

        const revoke = document.createElement('button');
        revoke.type = 'button';
        revoke.className = 'btn btn--quiet';
        revoke.textContent = 'Revoke';
        revoke.addEventListener('click', async () => {
            if (!confirm(`Sign out ${d.device_name || 'this device'}? It will need the connection code to pair again.`)) return;
            try {
                await admin.revokeSession(d.id);
                toast('Device revoked.');
                renderDevices(root);
            } catch (err) {
                toast(err.isForbidden ? ADMIN_LOCAL_ONLY : 'Could not revoke the device.', { variant: 'error' });
            }
        });

        li.append(text, revoke);
        return li;
    }));
}

async function onRotateCode() {
    if (!confirm('Rotate the connection code? Every paired device is signed out and must '
        + 'pair again with the new code. Devices at home reconnect on their own.')) return;
    try {
        const res = await admin.rotateConnectionCode();
        const codeEl = document.querySelector('#connection-code-value');
        if (codeEl) codeEl.textContent = res.connection_code;
        toast(`New code: ${res.connection_code}`);
        const panel = document.querySelector('#admin-panel .settings__admin-inner');
        if (panel) renderDevices(panel);
    } catch (err) {
        toast(err.isForbidden ? ADMIN_LOCAL_ONLY : 'Could not rotate the code.', { variant: 'error' });
    }
}

/* ==================== unmatched files (fix match) ==================== */

async function renderUnmatched(root) {
    const section = $('#unmatched-section', root);
    const list = $('#unmatched-list', root);
    let items = [];
    try {
        items = await admin.getUnmatched();
    } catch {
        return; // non-critical; leave the section hidden
    }
    if (!Array.isArray(items) || items.length === 0) {
        section.hidden = true;
        return;
    }
    section.hidden = false;
    $('#unmatched-note', root).textContent =
        `${items.length} file${items.length === 1 ? '' : 's'} the scanner couldn't identify. Search and pick the right title.`;
    list.replaceChildren(...items.map(fixRow));
}

// fixRow builds one unmatched-file row with an inline TMDB search + match form.
function fixRow(item) {
    const li = document.createElement('li');
    li.className = 'unmatched__row';

    const head = document.createElement('div');
    head.className = 'unmatched__head';
    const name = document.createElement('span');
    name.className = 'unmatched__name';
    name.textContent = item.parsed_title || basename(item.path);
    const kind = document.createElement('span');
    kind.className = 'unmatched__kind';
    kind.textContent = item.kind === 'episode' ? 'TV' : 'Movie';
    head.append(name, kind);

    const form = document.createElement('div');
    form.className = 'unmatched__form';

    const search = document.createElement('input');
    search.type = 'text';
    search.className = 'settings__input';
    search.placeholder = 'Search TMDB by title';
    search.value = item.parsed_title || '';

    // Season/episode inputs only for episodes.
    let season, episode;
    if (item.kind === 'episode') {
        season = numberInput('Season');
        episode = numberInput('Ep');
    }

    const go = document.createElement('button');
    go.type = 'button';
    go.className = 'btn btn--quiet';
    go.textContent = 'Search';

    const results = document.createElement('div');
    results.className = 'unmatched__results';

    const runSearch = async () => {
        const q = search.value.trim();
        if (!q) return;
        results.textContent = 'Searching…';
        let found = [];
        try {
            found = await admin.searchTMDB(q, item.kind);
        } catch (err) {
            results.textContent = err.body?.error ?? 'Search failed.';
            return;
        }
        if (!found.length) {
            results.textContent = 'No matches.';
            return;
        }
        results.replaceChildren(...found.slice(0, 6).map((r) =>
            resultButton(r, item, season, episode, li)));
    };
    go.addEventListener('click', runSearch);
    search.addEventListener('keydown', (e) => { if (e.key === 'Enter') runSearch(); });

    form.append(search);
    if (season) form.append(season, episode);
    form.append(go);
    li.append(head, form, results);
    return li;
}

function resultButton(r, item, season, episode, row) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'unmatched__result';
    b.textContent = r.year ? `${r.title} (${r.year})` : r.title;
    b.addEventListener('click', async () => {
        const body = { path: item.path, kind: item.kind, tmdb_id: r.tmdb_id };
        if (item.kind === 'episode') {
            body.season = Number(season.value);
            body.episode = Number(episode.value);
            if (!body.season || !body.episode) {
                toast('Enter the season and episode numbers first.', { variant: 'error' });
                return;
            }
        }
        try {
            await admin.manualMatch(body);
            toast('Matched.');
            row.remove();
        } catch (err) {
            if (err.isForbidden) toast(ADMIN_LOCAL_ONLY, { variant: 'error' });
            else toast(err.body?.error ?? 'Match failed.', { variant: 'error' });
        }
    });
    return b;
}

function numberInput(placeholder) {
    const i = document.createElement('input');
    i.type = 'number';
    i.min = '0';
    i.className = 'settings__input settings__input--num';
    i.placeholder = placeholder;
    return i;
}

function basename(p) {
    return p.split(/[\\/]/).pop();
}

/* ==================== logs ==================== */

// The server's recent log lines, in a dialog. Same content as `northrou logs`.
async function showLogs() {
    let text;
    try {
        text = await admin.getLogs();
    } catch {
        toast('Could not load the server log.', { variant: 'error' });
        return;
    }

    const dialog = document.createElement('dialog');
    dialog.className = 'logs-dialog';

    const pre = document.createElement('pre');
    pre.className = 'logs-dialog__text';
    pre.textContent = text || 'The log is empty.';

    const close = document.createElement('button');
    close.type = 'button';
    close.className = 'btn btn--quiet';
    close.textContent = 'Close';
    close.addEventListener('click', () => dialog.close());

    dialog.append(pre, close);
    dialog.addEventListener('close', () => dialog.remove());
    document.body.appendChild(dialog);
    dialog.showModal();
    pre.scrollTop = pre.scrollHeight; // newest lines are the ones being asked about
}

async function saveConfig(patch) {
    try {
        config = await admin.patchConfig(patch);
        toast('Saved.');
    } catch (err) {
        toast(err.isForbidden ? ADMIN_LOCAL_ONLY
            : (err.body?.error ?? 'Could not save.'), { variant: 'error' });
    }
}

/* ==================== boot ==================== */

async function init() {
    try {
        [me, profiles] = await Promise.all([getMe(), listProfiles()]);
    } catch (err) {
        if (err.isAuth) {
            // Not paired: send to the connect screen. Stay blank, don't flash
            // the settings shell.
            window.location.replace('welcome.html');
            return;
        }
        // Staying to show the error: reveal so it isn't hidden behind the boot gate.
        reveal();
        $('.settings').replaceChildren(statePanel({
            variant: 'error',
            title: "Couldn't reach the server",
            body: 'Check that your Northrou server is running and reachable, then try again.',
            action: { label: 'Retry', onClick: () => window.location.reload() },
        }));
        return;
    }

    reveal();
    // "Server" row: the server's own name (set during `northrou setup`), with
    // the LAN address / stored pairing as fallbacks for older servers.
    $('#account-email').textContent = me.server_name
        || (isSameOrigin()
            ? (window.location.host || 'This server')
            : (getServer()?.name ?? getServer()?.code ?? 'Paired server'));
    $('#current-profile').textContent = me.profile.name;

    renderProfiles();
    renderPlayback();
    renderLanguage();
    renderAbout();

    // Admin is a property of the connection: `me.admin` is true only for a local
    // request (a browser on the server's network, or the CLI). Remote apps see a
    // note instead of the editable controls.
    if (me.admin) await renderAdmin();
    else renderAdminLocked();

    $('#sign-out').addEventListener('click', signOut);
    $('#add-profile').addEventListener('click', async () => {
        const name = prompt('New profile name')?.trim();
        if (!name) return;
        try {
            await createProfile(name);
            profiles = await listProfiles();
            renderProfiles();
            toast(`Added ${name}.`);
        } catch {
            toast('Could not add the profile.', { variant: 'error' });
        }
    });
    $('#view-logs').addEventListener('click', showLogs);
}

init();
