// Settings.
//
// Server admin (scanning, streaming, ffmpeg, TMDB key, remote access, devices,
// connection code) is not editable here: it lives on the server, via
// `northrou admin` / `northrou setup` / config.toml. This page is the viewer's
// own settings: profiles, playback, language, and an About panel.

import { $, show, reveal } from '../lib/dom.js';
import { segmented, toggle } from '../components/controls.js';
import { toast, mountOfflineBanner, statePanel } from '../components/states.js';
import { getMe, signOut, setMyLanguage, getUpdateInfo } from '../data/account.js';
import { listProfiles, createProfile, renameProfile, deleteProfile, MAX_PROFILES } from '../data/profiles.js';
import { getPrefs, setPref, QUALITY_OPTIONS, CELLULAR_OPTIONS } from '../data/prefs.js';
import { isSignedIn } from '../api/session.js';
import { requireServer } from '../api/connect.js';
import { getServer, isSameOrigin } from '../data/servers.js';

// Deleting a profile wipes that viewer's watch history, so the server gates it to
// a local request (a browser on the server's own network, or the CLI). `me.admin`
// reflects that. The wording shown when a remote session tries.
const DELETE_LOCAL_ONLY = 'Deleting a profile is only possible on your home network or with the northrou CLI.';

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
    // Deleting takes that viewer's whole history with it, so the server only
    // allows it from a local connection. Say so before the confirm.
    if (!me.admin) {
        toast(DELETE_LOCAL_ONLY, { variant: 'error' });
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
        else if (err.isForbidden) toast(DELETE_LOCAL_ONLY, { variant: 'error' });
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

// Version is a read, open to any session. Installing an update is done on the
// server (`northrou update`), not from here, so this only reports.
async function renderAbout() {
    const paint = async (announce) => {
        try {
            const info = await getUpdateInfo();
            if (!info.current) {
                $('#version').textContent = 'Version unavailable';
            } else if (info.update_available) {
                $('#version').textContent =
                    `v${info.current} — v${info.latest} is available. Update on the server with northrou update.`;
                if (announce) toast(`Version ${info.latest} is available. Install it on the server.`);
            } else {
                $('#version').textContent = `v${info.current}`;
                if (announce) toast('You are on the latest version.');
            }
        } catch {
            $('#version').textContent = 'Version unavailable';
            if (announce) toast('Could not check for updates.', { variant: 'error' });
        }
    };

    await paint(false);
    $('#check-update').addEventListener('click', async (e) => {
        e.target.disabled = true;
        await paint(true);
        e.target.disabled = false;
    });
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
}

init();
