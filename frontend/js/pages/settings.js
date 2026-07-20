// Settings.

import { $, show, hide, setError, reveal } from '../lib/dom.js';
import { segmented, toggle } from '../components/controls.js';
import { createOtpInput } from '../components/otp.js';
import { toast, mountOfflineBanner, statePanel } from '../components/states.js';
import { getMe, signOut } from '../data/account.js';
import { listProfiles, createProfile, renameProfile, deleteProfile, MAX_PROFILES } from '../data/profiles.js';
import { getPrefs, setPref, QUALITY_OPTIONS, CELLULAR_OPTIONS } from '../data/prefs.js';
import * as admin from '../data/admin.js';
import { isSignedIn } from '../api/session.js';
import { requireServer } from '../api/connect.js';

const TILES = ['#3c89e0', '#19ad31', '#d4412e', '#da3ce0', '#e0a13c', '#3cd6e0'];
const tileFor = (id) => TILES[(id - 1) % TILES.length];

$('#back').addEventListener('click', () => history.back());
mountOfflineBanner();

if (!(await requireServer())) throw new Error('no server');
if (!isSignedIn()) window.location.replace('login.html');

let me = null;
let profiles = [];
let config = null;

/** Send the viewer to the elevation gate, whichever step it is showing. */
function focusUnlock() {
    const verify = $('#unlock-verify');
    const target = verify.classList.contains('u-hidden') ? $('#request-otp') : verify;
    target.scrollIntoView({ block: 'center', behavior: 'smooth' });
    // The six boxes are not focusable as a unit, so aim at the first one.
    (target === verify ? $('.otp__input', verify) : target).focus();
}

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
        + `Deleting one needs an admin code. Maximum of ${MAX_PROFILES} profiles.`;
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
    // Deleting takes that viewer's whole history with it, so the API gates it
    // behind elevation. Say so before the confirm, not after a 403.
    if (!admin.isElevated()) {
        toast('Unlock Server admin to delete a profile.', { variant: 'error' });
        focusUnlock();
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
        else if (err.isForbidden) toast('Admin elevation expired. Unlock again.', { variant: 'error' });
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
    if (!admin.isElevated()) {
        toast('Unlock Server admin to install an update.', { variant: 'error' });
        focusUnlock();
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

const unlockBtn = $('#unlock');
const unlockError = $('#unlock-error');
const otp = createOtpInput($('#unlock-verify'));

// The six boxes are one field, so gate Unlock on all of them being filled.
$('#unlock-verify').addEventListener('input', () => {
    unlockBtn.disabled = !otp.complete;
    setError(unlockError, '');
});

/** Ask for a code and reveal the entry step. Also the resend path. */
async function requestOtp(btn) {
    btn.disabled = true;
    try {
        await admin.requestOtp();
        $('#unlock-sent').textContent = `Enter the 6-digit code sent to ${me.account.email}.`;
        hide($('#unlock-request'));
        show($('#unlock-verify'));
        otp.clear();
        otp.focus();
        setError(unlockError, '');
        unlockBtn.disabled = true;
    } catch {
        toast('Could not send the code.', { variant: 'error' });
    } finally {
        btn.disabled = false;
    }
}

$('#request-otp').addEventListener('click', (e) => requestOtp(e.currentTarget));
$('#resend-otp').addEventListener('click', (e) => requestOtp(e.currentTarget));

$('#unlock-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!otp.complete) return;
    unlockBtn.disabled = true;
    try {
        await admin.verifyOtp(otp.value);
        setError(unlockError, '');
        await renderAdmin();
        toast('Server admin unlocked for 10 minutes.');
    } catch (err) {
        setError(unlockError, err.isAuth ? 'Invalid or expired code.' : 'Could not verify the code.');
        otp.clear();
        otp.focus();
    } finally {
        unlockBtn.disabled = !otp.complete;
    }
});

async function renderAdmin() {
    const panel = $('#admin-panel');
    panel.hidden = false;
    hide($('#unlock-form'));
    $('#admin-state').dataset.state = 'unlocked';
    $('#admin-state').textContent = 'Unlocked';

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
    $('#server-name', node).textContent = 'This server';
    $('#server-address', node).textContent = window.location.host || 'local';
    $('#server-status', node).dataset.state = 'connected';
    $('#server-status-text', node).textContent = 'Connected';

    // ---- library
    $('#scan-note', node).textContent = scan?.running
        ? 'Scanning now...'
        : (scan?.last_scan ? `Last scan: ${scan.last_scan}.` : 'No scan has run yet.');

    $('#scan-now', node).addEventListener('click', async (e) => {
        e.target.disabled = true;
        try {
            await admin.startScan();
            toast('Scan started.');
        } catch (err) {
            if (err.isForbidden) toast('Admin elevation expired. Unlock again.', { variant: 'error' });
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

    $('#switch-server', node).addEventListener('click', () => window.location.assign('connect.html'));
    $('#forget-server', node).addEventListener('click', () => {
        if (!confirm('Forget this server? You will need its connection code to pair again.')) return;
        signOut();
    });
}

async function saveConfig(patch) {
    try {
        config = await admin.patchConfig(patch);
        toast('Saved.');
    } catch (err) {
        toast(err.isForbidden ? 'Admin elevation expired. Unlock again.'
            : (err.body?.error ?? 'Could not save.'), { variant: 'error' });
    }
}

/* ==================== boot ==================== */

async function init() {
    try {
        [me, profiles] = await Promise.all([getMe(), listProfiles()]);
    } catch (err) {
        if (err.isAuth) {
            // Redirecting to sign in: stay blank, don't flash the settings shell.
            window.location.replace('login.html');
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
    $('#account-email').textContent = me.account.email;
    $('#current-profile').textContent = me.profile.name;
    $('#unlock-note').textContent =
        `Changing server settings needs a one-time code emailed to ${me.account.email}. `
        + 'Anyone signed in can request one.';

    renderProfiles();
    renderPlayback();
    renderAbout();

    // `admin: true` means this token is ALREADY elevated, so the OTP round trip
    // can be skipped. It does not mean "may administer" -- everyone may.
    if (me.admin) await renderAdmin();

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
    $('#view-logs').addEventListener('click', () => toast('Logs are not wired up yet.', { variant: 'error' }));
}

init();
