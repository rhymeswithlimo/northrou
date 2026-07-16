// Settings.

import { $, $$, show, hide, setError } from '../lib/dom.js';
import { segmented, toggle } from '../components/controls.js';
import { toast, mountOfflineBanner, statePanel } from '../components/states.js';
import { getMe, signOut } from '../data/account.js';
import { MAX_PROFILES } from '../data/profiles.js';
import { getPrefs, setPref, QUALITY_OPTIONS, CELLULAR_OPTIONS } from '../data/prefs.js';
import * as admin from '../data/admin.js';

const TILES = ['#3c89e0', '#19ad31', '#d4412e', '#da3ce0', '#e0a13c', '#3cd6e0'];
const tileFor = (id) => TILES[(id - 1) % TILES.length];

$('#back').addEventListener('click', () => history.back());
mountOfflineBanner();

let me = null;

/* ==================== account + profiles ==================== */

function renderProfiles() {
    const list = $('#profiles-list');
    list.replaceChildren(...me.profiles.map((p) => {
        const node = $('#tpl-profile-row').content.firstElementChild.cloneNode(true);
        const avatar = $('.setting__avatar', node);
        avatar.style.setProperty('--tile', tileFor(p.id));
        avatar.textContent = p.name.charAt(0).toUpperCase();
        $('.setting__name', node).textContent = p.name;
        if (p.id === me.profile.id) show($('.setting__you', node));

        $('[data-edit]', node).addEventListener('click', () => renameProfile(p));
        const del = $('[data-delete]', node);
        // Never leave zero profiles; the API returns 409 for the last one, so
        // don't offer the button in the first place.
        if (me.profiles.length <= 1) del.remove();
        else del.addEventListener('click', () => deleteProfile(p));

        return node;
    }));

    $('#add-profile').disabled = me.profiles.length >= MAX_PROFILES;
    $('#profiles-note').textContent =
        `Anyone can add or rename a profile. Each has its own watch history and recommendations. `
        + `Deleting one needs an admin code. Maximum of ${MAX_PROFILES} profiles.`;
}

function renameProfile(profile) {
    const name = prompt('Profile name', profile.name)?.trim();
    if (!name || name === profile.name) return;
    profile.name = name;
    renderProfiles();
    toast(`Renamed to ${name}.`);
}

function deleteProfile(profile) {
    // Deleting is destructive (it takes that viewer's whole history with it)
    // and the API gates it behind elevation.
    if (!admin.isElevated()) {
        toast('Unlock Server admin to delete a profile.', { variant: 'error' });
        $('#admin-otp').focus();
        return;
    }
    if (!confirm(`Delete ${profile.name}? Their watch history and recommendations go too.`)) return;
    me.profiles = me.profiles.filter((p) => p.id !== profile.id);
    renderProfiles();
    toast(`Deleted ${profile.name}.`);
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

/* ==================== about ==================== */

async function renderAbout() {
    try {
        const info = await admin.checkUpdate();
        $('#version').textContent = info.current ? `v${info.current}` : 'Version unavailable';
        if (info.update_available) {
            $('#check-update').textContent = `Update to v${info.latest}`;
            $('#check-update').classList.replace('btn--quiet', 'btn--primary');
        }
    } catch {
        $('#version').textContent = 'Version unavailable';
    }
}

/* ==================== server admin ==================== */

const otpInput = $('#admin-otp');
const unlockBtn = $('#unlock');
const unlockError = $('#unlock-error');

otpInput.addEventListener('input', () => {
    otpInput.value = otpInput.value.replace(/\D/g, '');
    unlockBtn.disabled = otpInput.value.length !== 6;
    setError(unlockError, '');
});

$('#request-otp').addEventListener('click', async () => {
    try {
        await admin.requestOtp();
        toast(`Code sent to ${me.account.email}.`);
        otpInput.focus();
    } catch {
        toast('Could not send the code.', { variant: 'error' });
    }
});

$('#unlock-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    unlockBtn.disabled = true;
    try {
        await admin.verifyOtp(otpInput.value);
        setError(unlockError, '');
        await renderAdmin();
        toast('Server admin unlocked for 10 minutes.');
    } catch (err) {
        setError(unlockError, err.status === 401 ? 'Invalid or expired code.' : 'Could not verify the code.');
        otpInput.value = '';
        otpInput.focus();
    } finally {
        unlockBtn.disabled = otpInput.value.length !== 6;
    }
});

async function renderAdmin() {
    const panel = $('#admin-panel');
    panel.hidden = false;
    hide($('#unlock-form'));
    $('#admin-state').dataset.state = 'unlocked';
    $('#admin-state').textContent = 'Unlocked';

    const node = $('#tpl-admin').content.firstElementChild.cloneNode(true);
    panel.replaceChildren(node);

    let server, hw, scan, config;
    try {
        [server, hw, scan, config] = await Promise.all([
            admin.getServer(), admin.getHardware(), admin.getScan(), admin.getConfig(),
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

    // ---- server
    $('#server-name', node).textContent = server.name;
    $('#server-address', node).textContent = server.address;
    $('#server-status', node).dataset.state = server.status;
    $('#server-status-text', node).textContent =
        server.status === 'connected' ? 'Connected' : 'Disconnected';

    // ---- libraries
    const libs = [
        ...config.movie_dirs.map((path) => ({ path, kind: 'Movies' })),
        ...config.show_dirs.map((path) => ({ path, kind: 'TV Shows' })),
    ];
    const list = $('#libraries-list', node);
    if (!libs.length) {
        list.replaceChildren(statePanel({
            title: 'No folders yet',
            body: 'Add a folder of movies or shows, then scan to build your library.',
        }));
    } else {
        list.replaceChildren(...libs.map((lib) => {
            const row = $('#tpl-library-row').content.firstElementChild.cloneNode(true);
            $('[data-path]', row).textContent = lib.path;
            $('[data-kind]', row).textContent = lib.kind;
            $('[data-remove]', row).addEventListener('click', () => {
                if (!confirm(`Remove ${lib.path}? The files stay on disk.`)) return;
                row.remove();
                toast('Folder removed.');
            });
            return row;
        }));
    }

    $('#scan-note', node).textContent = scan.last_scan
        ? `Last scan: ${scan.last_scan}, ${scan.items} items.`
        : 'No scan has run yet.';

    $('#scan-now', node).addEventListener('click', async (e) => {
        e.target.disabled = true;
        try {
            await admin.startScan();
            toast('Scan started.');
        } catch {
            toast('Could not start the scan.', { variant: 'error' });
        } finally {
            e.target.disabled = false;
        }
    });

    // ---- streaming
    $('#hwaccel', node).textContent = hw.ffmpeg_ready
        ? (hw.available.length ? hw.available.join(', ') : 'None detected, using software')
        : 'ffmpeg is still downloading';
    $('#hwaccel-state', node).textContent = hw.backend && hw.backend !== 'software' ? 'On' : 'Off';

    $('#capacity-note', node).textContent =
        `Auto uses ${hw.estimated_capacity} on this hardware. Direct play and remux never count.`;

    segmented($('#transcode-cap', node), [
        { value: '0', label: 'Auto' },
        { value: '1', label: '1' },
        { value: '2', label: '2' },
        { value: '4', label: '4' },
    ], String(config.max_transcodes), (v) => saveConfig({ max_transcodes: Number(v) }));

    // ---- advanced
    segmented($('#ffmpeg-mode', node), [
        { value: 'managed', label: 'Managed' },
        { value: 'system', label: 'System' },
    ], config.prefer_system_ffmpeg ? 'system' : 'managed', (v) =>
        saveConfig({ prefer_system_ffmpeg: v === 'system' }));

    segmented($('#mail-mode', node), [
        { value: 'relay', label: 'Hosted relay' },
        { value: 'smtp', label: 'Own SMTP' },
    ], config.mail_mode, (v) => saveConfig({ mail_mode: v }));

    toggle($('#remote-access', node), config.remote_enabled, (v) =>
        saveConfig({ remote_enabled: v }));

    $('#switch-server', node).addEventListener('click', () => {
        window.location.assign('connect.html');
    });
    $('#forget-server', node).addEventListener('click', () => {
        if (!confirm('Forget this server? You will need its connection code to pair again.')) return;
        window.location.assign('connect.html');
    });
}

async function saveConfig(patch) {
    try {
        await admin.patchConfig(patch);
        toast('Saved.');
    } catch (err) {
        toast(err.status === 403 ? 'Admin elevation expired. Unlock again.' : 'Could not save.', { variant: 'error' });
    }
}

/* ==================== boot ==================== */

async function init() {
    try {
        me = await getMe();
    } catch {
        $('.settings').replaceChildren(statePanel({
            variant: 'error',
            title: "Couldn't reach the server",
            body: 'Check that your Northrou server is running and reachable, then try again.',
            action: { label: 'Retry', onClick: () => window.location.reload() },
        }));
        return;
    }

    $('#account-email').textContent = me.account.email;
    $('#current-profile').textContent = me.profile.name;
    $('#unlock-note').textContent =
        `Changing server settings needs a one-time code emailed to ${me.account.email}. `
        + `Anyone signed in can request one.`;

    renderProfiles();
    renderPlayback();
    renderAbout();

    // An already-elevated token means the OTP round-trip can be skipped.
    if (me.admin) await renderAdmin();

    $('#sign-out').addEventListener('click', signOut);
    $('#add-profile').addEventListener('click', () => {
        const name = prompt('New profile name')?.trim();
        if (!name) return;
        const id = Math.max(0, ...me.profiles.map((p) => p.id)) + 1;
        me.profiles.push({ id, name });
        renderProfiles();
        toast(`Added ${name}.`);
    });
    $('#view-logs').addEventListener('click', () => toast('Logs are not wired up yet.', { variant: 'error' }));
}

init();
