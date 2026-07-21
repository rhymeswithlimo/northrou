// Small DOM helpers. Deliberately tiny: the client has no framework and this
// is not the beginning of one.

export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

export const show = (el) => el?.classList.remove('u-hidden');
export const hide = (el) => el?.classList.add('u-hidden');

// reveal() takes a page out of its initial hidden ("booting") state. Every page
// starts hidden (an inline <head> script adds `booting`, hidden via base.css) so
// a boot-time redirect - e.g. index -> login - happens on a blank screen instead
// of flashing the wrong page. Call it on every path that STAYS on the page
// (rendered, empty, or error), and never before a redirect. A timeout backstop
// in the inline script reveals anyway if a path forgets, so the worst case is
// today's flash, not a permanently blank screen.
export const reveal = () => document.documentElement.classList.remove('booting');

/** Restart a CSS animation that may already be applied. */
export function replay(el, className) {
    el.classList.remove(className);
    void el.offsetWidth; // force reflow so the animation restarts
    el.classList.add(className);
}

/**
 * Normalize a message for display as an error: capitalize the first letter and
 * end with a full stop. Idempotent, and leaves an empty or already-punctuated
 * message alone. Applied by every error-display path (setError, error toasts,
 * error state panels) so displayed errors read consistently whatever their
 * source - including raw backend/coordinator strings, which are lowercase and
 * unpunctuated by Go convention.
 */
export function formatError(message) {
    const s = String(message ?? '').trim();
    if (!s) return s;
    const out = s.charAt(0).toUpperCase() + s.slice(1);
    return /[.!?]$/.test(out) ? out : out + '.';
}

/** Show a message in an element wired as an aria-live region. */
export function setError(el, message) {
    if (!el) return;
    if (!message) {
        el.textContent = '';
        hide(el);
        return;
    }
    el.textContent = formatError(message);
    show(el);
}

export const prefersReducedMotion = () =>
    window.matchMedia('(prefers-reduced-motion: reduce)').matches;
