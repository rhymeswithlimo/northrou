// Small DOM helpers. Deliberately tiny: the client has no framework and this
// is not the beginning of one.

export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

export const show = (el) => el?.classList.remove('u-hidden');
export const hide = (el) => el?.classList.add('u-hidden');

/** Restart a CSS animation that may already be applied. */
export function replay(el, className) {
    el.classList.remove(className);
    void el.offsetWidth; // force reflow so the animation restarts
    el.classList.add(className);
}

/** Show a message in an element wired as an aria-live region. */
export function setError(el, message) {
    if (!el) return;
    if (!message) {
        el.textContent = '';
        hide(el);
        return;
    }
    el.textContent = message;
    show(el);
}

export const prefersReducedMotion = () =>
    window.matchMedia('(prefers-reduced-motion: reduce)').matches;
