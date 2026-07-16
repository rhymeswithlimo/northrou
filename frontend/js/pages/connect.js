// Connection bootstrap: pair this device with a Northrou server.
//
// This runs before auth, because there is nothing to sign in to until the
// client knows which box it is talking to.

import { $, show, hide, setError } from '../lib/dom.js';
import { useDirect, useTunnel } from '../api/transport.js';
import {
    setServer, probeLan, normalizeCode, isSameOrigin, DEFAULT_COORD_URL,
} from '../data/servers.js';

const stepCode = $('#step-code');
const stepConnecting = $('#step-connecting');
const codeInput = $('#code');
const lanInput = $('#lan');
const coordInput = $('#coord');
const connectBtn = $('#connect');
const codeError = $('#code-error');
const status = $('#connect-status');

// Served off the box: it is already right there, and pairing is meaningless.
if (isSameOrigin()) window.location.replace('index.html');

/** Format as NR-XXXX-XXXX while typing, without fighting the caret at the end. */
codeInput.addEventListener('input', () => {
    const raw = codeInput.value.toUpperCase().replace(/[^A-Z0-9]/g, '');
    const body = raw.startsWith('NR') ? raw.slice(2) : raw;
    const parts = ['NR'];
    if (body.length) parts.push(body.slice(0, 4));
    if (body.length > 4) parts.push(body.slice(4, 8));
    codeInput.value = body.length ? parts.join('-') : '';

    connectBtn.disabled = !normalizeCode(codeInput.value);
    setError(codeError, '');
});

$('#code-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const code = normalizeCode(codeInput.value);
    if (!code) return;

    const lan = lanInput.value.trim();
    // A self-hosted coordinator overrides the default hosted broker.
    const coordUrl = coordInput.value.trim() || DEFAULT_COORD_URL;
    show(stepConnecting);
    hide(stepCode);

    // 1. LAN first. At home it is faster, needs no broker and survives the
    //    internet being down.
    if (lan) {
        status.textContent = 'Looking on this network...';
        if (await probeLan(lan)) {
            useDirect(lan);
            setServer({ code, lan, coordUrl, mode: 'lan' });
            window.location.replace('login.html');
            return;
        }
    }

    // 2. Not here (or no address given): hole-punch to the house.
    $('#s-lan').className = 'is-done';
    $('#s-tunnel').className = 'is-current';
    status.textContent = 'Connecting peer to peer...';

    try {
        await useTunnel({ coordUrl, code });
        setServer({ code, lan: lan || undefined, coordUrl, mode: 'tunnel' });
        window.location.replace('login.html');
    } catch (err) {
        // Back to the form with the real reason, rather than a dead spinner.
        hide(stepConnecting);
        show(stepCode);
        $('#s-lan').className = 'is-current';
        $('#s-tunnel').className = '';
        setError(codeError, err.message || 'Could not reach that server.');
        codeInput.focus();
    }
});

codeInput.focus();
