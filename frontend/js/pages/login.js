// Login: email -> one-time pin -> signed in.
//
// The two steps are sibling <section>s; only one is ever visible. Both are real
// <form>s, so Enter submits and the browser handles autofill normally.

import { $, show, hide, replay, setError } from '../lib/dom.js';
import { mountAsciiArt } from '../components/ascii-art.js';
import { createOtpInput } from '../components/otp.js';
import { requestPin, verifyPin } from '../data/account.js';
import { NetworkError } from '../api/client.js';
import { isSignedIn } from '../api/session.js';

// Stricter than type="email", which happily accepts "a@b".
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

const stepEmail = $('#step-email');
const stepOtp = $('#step-otp');
const emailForm = $('#email-form');
const emailInput = $('#email');
const sendBtn = $('#send-code');
const emailError = $('#email-error');
const otpForm = $('#otp-form');
const otpSubmit = $('#otp-submit');
const otpError = $('#otp-error');
const backBtn = $('#otp-back');

const otp = createOtpInput(otpForm);

mountAsciiArt($('#ascii'));

// Already signed in: nothing to do here.
if (isSignedIn()) window.location.replace('profiles.html');

const shake = (el) => replay(el, 'shake');

/** Remembers which method was last used, so the badge is real rather than decorative. */
const LAST_USED_KEY = 'northrou.lastUsed';
function markLastUsed(method) {
    try {
        localStorage.setItem(LAST_USED_KEY, method);
    } catch { /* non-essential */ }
}
function showLastUsed() {
    let last;
    try {
        last = localStorage.getItem(LAST_USED_KEY);
    } catch {
        return;
    }
    if (!last) return;
    const badge = document.querySelector(`[data-last-used="${last}"]`);
    if (badge) badge.hidden = false;
}
showLastUsed();

// ---------- step 1: email ----------

const validateEmail = () => {
    sendBtn.disabled = !EMAIL_RE.test(emailInput.value.trim());
};

emailInput.addEventListener('input', () => {
    validateEmail();
    setError(emailError, '');
});
validateEmail(); // covers browser-restored values on reload

emailForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (sendBtn.disabled) return;

    sendBtn.disabled = true;
    sendBtn.textContent = 'Sending...';
    try {
        await requestPin(emailInput.value.trim());
        markLastUsed('email');
        goToStep(stepOtp);
        otp.focus();
    } catch (err) {
        // request-pin is always 200 for a valid request, so an error here means
        // the server is unreachable or broken, never "wrong email".
        setError(emailError, err instanceof NetworkError
            ? 'Could not reach the server. Check your connection.'
            : 'Something went wrong sending your code. Try again.');
        shake(sendBtn);
    } finally {
        sendBtn.textContent = 'Send Code';
        validateEmail();
    }
});

// ---------- step 2: pin ----------

otpForm.addEventListener('submit', async (e) => {
    e.preventDefault();

    if (!otp.complete) {
        shake(otpSubmit);
        otp.focusFirstEmpty();
        return;
    }

    otpSubmit.disabled = true;
    otpSubmit.textContent = 'Signing in...';
    try {
        const res = await verifyPin(emailInput.value.trim(), otp.value);
        setError(otpError, '');
        // More than one profile means someone has to pick; a single-profile
        // household should not be made to choose from a list of one.
        window.location.assign(res.profiles?.length > 1 ? 'profiles.html' : 'index.html');
        return;
    } catch (err) {
        setError(otpError, err instanceof NetworkError
            ? 'Could not reach the server.'
            : 'Invalid or expired code.');
        shake(otpSubmit);
        otp.clear();
        otp.focus();
    } finally {
        otpSubmit.disabled = false;
        otpSubmit.textContent = 'Sign in';
    }
});

otpSubmit.addEventListener('animationend', () => otpSubmit.classList.remove('shake'));

backBtn.addEventListener('click', () => {
    goToStep(stepEmail);

    // A half-typed code belongs to the address we're leaving; the next trip
    // forward may well use a different one.
    otp.clear();
    setError(otpError, '');
    emailInput.focus();
});

function goToStep(step) {
    for (const s of [stepEmail, stepOtp]) {
        if (s === step) show(s); else hide(s);
    }
}
