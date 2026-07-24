// Welcome: pair this device with a Northrou server.
//
// This runs before auth, because there is nothing to sign in to until the
// client knows which box it is talking to. Entering the connection code is the
// whole ceremony, so landing a successful pair gets the confetti.

import { $, show, hide, setError, reveal } from '../lib/dom.js';
import { useTunnel } from '../api/transport.js';
import { pair } from '../data/account.js';
import { confetti } from '../lib/confetti.js';
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

/** Group the code as NR-XXXXX-XXXXX while typing, without fighting the caret. */
codeInput.addEventListener('input', () => {
    const raw = codeInput.value.toUpperCase().replace(/[^A-Z0-9]/g, '');
    const body = raw.startsWith('NR') ? raw.slice(2) : raw;
    const groups = body.match(/.{1,5}/g) ?? [];
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
        const res = await pair(code);
        // Remember the server together with its name so settings and the
        // switcher can say "Living Room NAS" rather than echo the code.
        setServer({ code, coordUrl: DEFAULT_COORD_URL, mode: 'tunnel', name: res?.server_name });

        // You're in: celebrate, let the burst register, then go watch.
        status.textContent = res?.server_name ? `Connected to ${res.server_name}` : 'Connected';
        confetti();
        const dest = res?.profiles?.length > 1 ? 'profiles.html' : 'index.html';
        setTimeout(() => window.location.replace(dest), 1600);
    } catch (err) {
        // Back to the form with the real reason, rather than a dead spinner.
        hide(stepConnecting);
        show(stepCode);
        setError(codeError, err.message || 'Could not reach that server, or the code was wrong.');
        codeInput.focus();
    }
});

codeInput.focus();
