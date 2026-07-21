// First-run setup.
//
// This one talks to the real API already: /api/setup/status and
// /api/setup/complete both exist. Setup signs the operator straight in with a
// session elevated for the setup window, so they can scan and administer
// without an email round-trip.
//
// Two steps, and neither collects a path: media folders are added on the box
// with `northrou admin`, where they can be checked against the real filesystem.

import { $, $$, show, hide, setError, reveal } from '../lib/dom.js';
import { toast } from '../components/states.js';
import { confetti } from '../lib/confetti.js';

// Setup never redirects away on boot (an already-set-up box shows a message in
// place), so it always stays: reveal immediately.
reveal();

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

const steps = {
    account: $('#step-account'),
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
    goTo(steps.meta);
});

/* ---------- step 2: finish ---------- */

const finishBtn = $('#finish');
const setupError = $('#setup-error');

$('#meta-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    finishBtn.disabled = true;
    finishBtn.textContent = 'Creating...';
    setError(setupError, '');

    const body = {
        email: emailInput.value.trim(),
        tmdb_api_key: $('#tmdb').value.trim(),
        enable_remote: true,
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
        confetti();
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
