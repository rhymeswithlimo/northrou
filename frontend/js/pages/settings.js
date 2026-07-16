// Settings page.

import { $, $$ } from '../lib/dom.js';

$('#back').addEventListener('click', () => history.back());

/*
 * A settings row lays out as `label ......... control` and wraps to two lines
 * when it runs out of width. A wrapped row needs breathing room that an
 * unwrapped one doesn't, and CSS can't ask "did you wrap?", so measure it.
 *
 * The previous version re-walked every row's whole subtree from a ResizeObserver
 * attached to every row *and* a window resize listener, so one resize ran the
 * whole O(rows x depth) pass once per row. This batches every notification into
 * a single rAF and reads geometry once per frame.
 */

const rows = $$('.settings-row');

/** True if any flex container inside `el` has its children on more than one line. */
function hasWrapped(el) {
    const children = Array.from(el.children);

    if (children.length >= 2) {
        let prevRight = null;
        for (const child of children) {
            const rect = child.getBoundingClientRect();
            if (rect.width > 0 && rect.height > 0) {
                // A child starting left of where the previous one ended means
                // the line broke.
                if (prevRight !== null && rect.left < prevRight - 1) return true;
                prevRight = rect.right;
            }
        }
    }

    // The wrapping container can sit at any depth, so keep looking down.
    return children.some(hasWrapped);
}

let queued = false;
function refresh() {
    queued = false;

    for (const group of $$('.group')) {
        const groupRows = $$(':scope > .settings-row', group);
        const wrapped = groupRows.map(hasWrapped);

        groupRows.forEach((row, i) => {
            // Top padding, unless a <br> or heading already spaces it above.
            const prev = row.previousElementSibling;
            const spacedAbove = Boolean(prev && ['BR', 'H3', 'H2'].includes(prev.tagName));
            row.classList.toggle('wrapped', wrapped[i] && !spacedAbove);

            // Bottom padding only where a wrapped row meets an unwrapped one.
            row.classList.toggle('pad-bottom', wrapped[i] && wrapped[i + 1] === false);
        });
    }
}

function schedule() {
    if (queued) return;
    queued = true;
    requestAnimationFrame(refresh);
}

const observer = new ResizeObserver(schedule);
for (const row of rows) observer.observe(row);

document.fonts.ready.then(schedule);
schedule();
