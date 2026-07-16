// Loading, empty, error and toast surfaces.
//
// Every screen that fetches uses these, so "no media yet", "the server went
// away" and "saved" look the same everywhere instead of each page inventing it.

import { $ } from '../lib/dom.js';

/**
 * An empty / error / loading panel.
 * @param {{title: string, body?: string, action?: {label: string, onClick: Function}, variant?: 'empty'|'error'}} opts
 */
export function statePanel({ title, body, action, variant = 'empty' }) {
    const el = document.createElement('div');
    el.className = `state state--${variant}`;
    // Errors announce themselves; an empty library is not an alert.
    if (variant === 'error') el.setAttribute('role', 'alert');

    const h = document.createElement('p');
    h.className = 'state__title';
    h.textContent = title;
    el.append(h);

    if (body) {
        const p = document.createElement('p');
        p.className = 'state__body';
        p.textContent = body;
        el.append(p);
    }

    if (action) {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'btn btn--quiet state__action';
        btn.textContent = action.label;
        btn.addEventListener('click', action.onClick);
        el.append(btn);
    }

    return el;
}

/** Row of poster-shaped skeletons, matching the real grid so nothing jumps. */
export function skeletonRow({ count = 6, ratio = '2 / 3' } = {}) {
    const section = document.createElement('section');
    section.className = 'row';
    section.setAttribute('aria-busy', 'true');

    const title = document.createElement('div');
    title.className = 'skeleton skeleton--text';
    title.style.width = '160px';
    section.append(title);

    const items = document.createElement('div');
    items.className = 'row__items';
    for (let i = 0; i < count; i++) {
        const card = document.createElement('div');
        card.className = 'skeleton';
        card.style.aspectRatio = ratio;
        items.append(card);
    }
    section.append(items);

    return section;
}

/* ---------- toasts ---------- */

let host = null;

function toastHost() {
    if (host) return host;
    host = document.createElement('div');
    host.className = 'toasts';
    // Announce politely: a toast is confirmation, not an interruption.
    host.setAttribute('aria-live', 'polite');
    document.body.append(host);
    return host;
}

/**
 * @param {string} message
 * @param {{variant?: 'success'|'error', duration?: number}} opts
 */
export function toast(message, { variant = 'success', duration = 3200 } = {}) {
    const el = document.createElement('div');
    el.className = `toast${variant === 'error' ? ' toast--error' : ''}`;

    const dot = document.createElement('span');
    dot.className = 'toast__dot';
    dot.setAttribute('aria-hidden', 'true');
    el.append(dot, document.createTextNode(message));

    toastHost().append(el);

    const remove = () => {
        el.classList.add('is-leaving');
        el.addEventListener('animationend', () => el.remove(), { once: true });
        // Belt and braces: reduced-motion kills the animation, so animationend
        // never fires and the node would linger.
        setTimeout(() => el.remove(), 400);
    };
    setTimeout(remove, duration);

    return remove;
}

/* ---------- offline ---------- */

/** Shows a banner whenever the browser reports no connectivity. */
export function mountOfflineBanner() {
    const el = document.createElement('div');
    el.className = 'offline u-hidden';
    el.setAttribute('role', 'status');
    el.textContent = 'Offline. Reconnecting when your network returns.';
    document.body.append(el);

    const sync = () => el.classList.toggle('u-hidden', navigator.onLine);
    window.addEventListener('online', sync);
    window.addEventListener('offline', sync);
    sync();
}
