// Login page: email -> one-time code -> signed in.
//
// The two steps are sibling <section>s; only one is ever visible. Both are real
// <form>s, so Enter submits and the browser handles autofill normally.

import { $, show, hide, replay, setError } from '../lib/dom.js';
import { mountAsciiArt } from '../components/ascii-art.js';
import { createOtpInput } from '../components/otp.js';

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

function shake(el) {
    replay(el, 'shake');
}

// ---------- step 1: email ----------

const validateEmail = () => {
    sendBtn.disabled = !EMAIL_RE.test(emailInput.value.trim());
};

emailInput.addEventListener('input', () => {
    validateEmail();
    setError(emailError, '');
});
validateEmail(); // covers browser-restored values on reload

emailForm.addEventListener('submit', (e) => {
    e.preventDefault();
    if (sendBtn.disabled) return;

    goToStep(stepOtp);
    otp.focus();
});

// ---------- step 2: code ----------

otpForm.addEventListener('submit', (e) => {
    e.preventDefault();

    if (!otp.complete) {
        shake(otpSubmit);
        otp.focusFirstEmpty();
        return;
    }

    // Until this talks to a server there is no correct code.
    setError(otpError, 'Invalid or expired OTP.');
    shake(otpSubmit);
    otp.clear();
    otp.focus();
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
