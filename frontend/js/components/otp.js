// Six single-character boxes that behave like one field: typing advances,
// backspace retreats, and a pasted or autofilled code distributes across them.

import { $$ } from '../lib/dom.js';

export function createOtpInput(root) {
    const inputs = $$('.otp__input', root);

    /** Write `digits` starting at box `start`, then park focus on the last one filled. */
    function fill(digits, start = 0) {
        const chars = [...digits].slice(0, inputs.length - start);
        if (!chars.length) return;
        chars.forEach((d, i) => { inputs[start + i].value = d; });
        inputs[Math.min(start + chars.length, inputs.length) - 1].focus();
    }

    inputs.forEach((input, index) => {
        const prev = inputs[index - 1];
        const next = inputs[index + 1];

        input.addEventListener('input', () => {
            // Autofill and paste can dump the whole code into one box.
            if (input.value.length > 1) {
                fill(input.value.replace(/\D/g, ''), index);
                return;
            }

            input.value = input.value.replace(/\D/g, '');
            if (input.value !== '' && next) next.focus();
        });

        input.addEventListener('keydown', (e) => {
            if (e.key === 'Backspace' && !input.value && prev) {
                e.preventDefault();
                prev.value = '';
                prev.focus();
            }
            if (e.key === 'ArrowLeft' && prev) { e.preventDefault(); prev.focus(); }
            if (e.key === 'ArrowRight' && next) { e.preventDefault(); next.focus(); }
        });

        // Gboard sends deleteContentBackward rather than a Backspace keydown.
        input.addEventListener('beforeinput', (e) => {
            if (e.inputType === 'deleteContentBackward' && !input.value && prev) {
                e.preventDefault();
                prev.value = '';
                prev.focus();
            }
        });

        input.addEventListener('paste', (e) => {
            e.preventDefault();
            const digits = (e.clipboardData || window.clipboardData).getData('text').replace(/\D/g, '');
            if (!digits) return;
            inputs.forEach((el) => { el.value = ''; });
            fill(digits, 0);
        });
    });

    return {
        get value() { return inputs.map((i) => i.value).join(''); },
        get complete() { return inputs.every((i) => i.value !== ''); },
        get length() { return inputs.length; },
        clear() {
            inputs.forEach((i) => { i.value = ''; });
        },
        focusFirstEmpty() {
            (inputs.find((i) => !i.value) || inputs[0]).focus();
        },
        focus() { inputs[0].focus(); },
    };
}
