// Connection bootstrap: pair this device with a Northrou server.
//
// This runs before auth, because there is nothing to sign in to until the
// client knows which box it is talking to.

import { $, show, hide, setError, reveal } from '../lib/dom.js';
import { useTunnel } from '../api/transport.js';
import {
    setServer, normalizeCode, isSameOrigin, DEFAULT_COORD_URL,
} from '../data/servers.js';

const stepCode = $('#step-code');
const stepConnecting = $('#step-connecting');
const codeInput = $('#code');
const coordInput = $('#coord');
const connectBtn = $('#connect');
const codeError = $('#code-error');
const status = $('#connect-status');

// Served off the box: it is already right there, and pairing is meaningless.
// Redirect on a blank screen; otherwise this is the right page, so reveal it.
if (isSameOrigin()) window.location.replace('index.html');
else reveal();

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

    // A self-hosted coordinator overrides the default hosted broker.
    const coordUrl = coordInput.value.trim() || DEFAULT_COORD_URL;
    show(stepConnecting);
    hide(stepCode);
    status.textContent = 'Connecting peer to peer...';

    try {
        await useTunnel({ coordUrl, code });
        setServer({ code, coordUrl, mode: 'tunnel' });
        window.location.replace('login.html');
    } catch (err) {
        // Back to the form with the real reason, rather than a dead spinner.
        hide(stepConnecting);
        show(stepCode);
        setError(codeError, err.message || 'Could not reach that server.');
        codeInput.focus();
    }
});

codeInput.focus();
