// Connection bootstrap: pair this device with a Northrou server.
//
// This runs before auth, because there is nothing to sign in to until the
// client knows which box it is talking to.

import { $, show, hide, setError, reveal } from '../lib/dom.js';
import { useTunnel } from '../api/transport.js';
import { pair } from '../data/account.js';
import {
    setServer, normalizeCode, isSameOrigin, DEFAULT_COORD_URL,
} from '../data/servers.js';

const stepCode = $('#step-code');
const stepConnecting = $('#step-connecting');
const codeInput = $('#code');
const connectBtn = $('#connect');
const codeError = $('#code-error');
const status = $('#connect-status');

// Served off the box: it is already right there, and pairing is meaningless.
// Redirect on a blank screen; otherwise this is the right page, so reveal it.
if (isSameOrigin()) window.location.replace('index.html');
else reveal();

/** Group the code as NR-XXXX-XXXX-... while typing, without fighting the caret. */
codeInput.addEventListener('input', () => {
    const raw = codeInput.value.toUpperCase().replace(/[^A-Z0-9]/g, '');
    const body = raw.startsWith('NR') ? raw.slice(2) : raw;
    const groups = body.match(/.{1,4}/g) ?? [];
    codeInput.value = body.length ? ['NR', ...groups].join('-') : '';

    connectBtn.disabled = !normalizeCode(codeInput.value);
    setError(codeError, '');
});

$('#code-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const code = normalizeCode(codeInput.value);
    if (!code) return;

    show(stepConnecting);
    hide(stepCode);
    status.textContent = 'Connecting peer to peer...';

    try {
        // Open the tunnel, then authenticate with the same code: the connection
        // code is the credential, exchanged for this device's own session.
        await useTunnel({ coordUrl: DEFAULT_COORD_URL, code });
        setServer({ code, coordUrl: DEFAULT_COORD_URL, mode: 'tunnel' });
        const res = await pair(code);
        window.location.replace(res?.profiles?.length > 1 ? 'profiles.html' : 'index.html');
    } catch (err) {
        // Back to the form with the real reason, rather than a dead spinner.
        hide(stepConnecting);
        show(stepCode);
        setError(codeError, err.message || 'Could not reach that server, or the code was wrong.');
        codeInput.focus();
    }
});

codeInput.focus();
