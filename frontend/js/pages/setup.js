// First-run setup.
//
// This one talks to the real API already: /api/setup/status and
// /api/setup/complete both exist. Setup signs the operator straight in with a
// session elevated for the setup window, so they can add media and scan without
// an email round-trip.

import { $, $$, show, hide, setError } from '../lib/dom.js';
import { toast } from '../components/states.js';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

const steps = {
    account: $('#step-account'),
    media: $('#step-media'),
    meta: $('#step-meta'),
    done: $('#step-done'),
};

function goTo(step) {
    for (const s of Object.values(steps)) hide(s);
    show(step);
    step.querySelector('input, button, a')?.focus();
}

$$('[data-back]').forEach((btn) => {
    btn.addEventListener('click', () => goTo($(`#${btn.dataset.back}`)));
});

/* ---------- step 1: account ---------- */

const emailInput = $('#email');
const accountNext = $('#account-next');

const validateEmail = () => {
    accountNext.disabled = !EMAIL_RE.test(emailInput.value.trim());
};
emailInput.addEventListener('input', validateEmail);
validateEmail();

$('#account-form').addEventListener('submit', (e) => {
    e.preventDefault();
    if (accountNext.disabled) return;
    goTo(steps.media);
});

/* ---------- step 2: media folders ---------- */

function addPathRow(hostId, value = '') {
    const host = $(`#${hostId}`);
    const node = $('#tpl-path-row').content.firstElementChild.cloneNode(true);
    const input = $('input', node);
    input.value = value;
    input.addEventListener('input', validateMedia);

    $('[data-remove]', node).addEventListener('click', () => {
        node.remove();
        // Always leave one row: an empty list with no input is a dead end.
        if (!$$('.setup__path', host).length) addPathRow(hostId);
        validateMedia();
    });

    host.append(node);
    return input;
}

const paths = (hostId) =>
    $$(`#${hostId} input`).map((i) => i.value.trim()).filter(Boolean);

function validateMedia() {
    // At least one folder somewhere, or there is nothing to scan.
    $('#media-next').disabled = paths('movie-dirs').length + paths('show-dirs').length === 0;
}

$$('[data-add]').forEach((btn) => {
    btn.addEventListener('click', () => addPathRow(btn.dataset.add).focus());
});

addPathRow('movie-dirs');
addPathRow('show-dirs');
validateMedia();

$('#media-form').addEventListener('submit', (e) => {
    e.preventDefault();
    if ($('#media-next').disabled) return;
    goTo(steps.meta);
});

/* ---------- step 3: finish ---------- */

const finishBtn = $('#finish');
const setupError = $('#setup-error');

$('#meta-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    finishBtn.disabled = true;
    finishBtn.textContent = 'Creating...';
    setError(setupError, '');

    const body = {
        email: emailInput.value.trim(),
        movie_dirs: paths('movie-dirs'),
        show_dirs: paths('show-dirs'),
        tmdb_api_key: $('#tmdb').value.trim(),
        enable_remote: true,
        smtp_host: $('#smtp_host').value.trim(),
        smtp_port: parseInt($('#smtp_port').value, 10) || 0,
        smtp_username: $('#smtp_username').value.trim(),
        smtp_password: $('#smtp_password').value,
        from_address: $('#from_address').value.trim(),
    };

    try {
        const res = await fetch('/api/setup/complete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        const data = await res.json().catch(() => ({}));

        if (!res.ok) {
            setError(setupError, data.error || 'Setup failed. Check the values and try again.');
            return;
        }

        $('#connection-code').textContent = data.connection_code ?? '';
        goTo(steps.done);
    } catch {
        setError(setupError, 'Could not reach the server. Is it still running?');
    } finally {
        finishBtn.disabled = false;
        finishBtn.textContent = 'Create account & finish';
    }
});

/* ---------- done ---------- */

$('#copy-code').addEventListener('click', async () => {
    try {
        await navigator.clipboard.writeText($('#connection-code').textContent);
        toast('Connection code copied.');
    } catch {
        toast('Copy failed. Select the code and copy it manually.', { variant: 'error' });
    }
});

/* ---------- boot ---------- */

// Setup is only usable while no account exists; say so rather than letting the
// operator fill the whole form and hit a 409 at the end.
(async function checkStatus() {
    try {
        const res = await fetch('/api/setup/status');
        const { needs_setup } = await res.json();
        if (!needs_setup) {
            steps.account.querySelector('.auth__subtitle').textContent =
                'This server is already set up. Sign in to continue.';
            $('#account-form').replaceChildren(
                Object.assign(document.createElement('a'), {
                    className: 'auth__submit',
                    href: 'login.html',
                    textContent: 'Go to sign in',
                    style: 'display:flex;text-decoration:none',
                }),
            );
            $('.setup__steps', steps.account).remove();
        }
    } catch {
        // The server may still be starting; let them fill the form and find out
        // at submit, which reports properly.
    }
})();
