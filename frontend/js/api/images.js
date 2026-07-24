// /api/images/* requires the same Bearer auth as the rest of the API, but a
// plain <img src="..."> can't attach an Authorization header - the browser
// just issues an unauthenticated request and it 401s. So images are fetched
// here (through the authenticated client) and handed to the DOM as blob:
// object URLs instead of pointing <img> straight at the API path.

import { getBlob } from './client.js';

// path -> blob: URL. A poster is reused across rows/cards, so this both
// dedupes concurrent fetches and avoids re-fetching on every render. Not
// evicted: a household's library is small enough that this is a non-issue
// for the life of a tab.
const cache = new Map();

/** Resolves an /api/images/... path to a blob: URL, fetching at most once per path. */
export function resolveImageURL(path) {
    if (!path) return Promise.resolve('');
    let entry = cache.get(path);
    if (!entry) {
        entry = getBlob(path).then((b) => URL.createObjectURL(b));
        cache.set(path, entry);
    }
    return entry;
}

/**
 * Sets img.src once the authenticated fetch resolves. Fire-and-forget so
 * synchronous render code (building a card, a row of cards) doesn't need to
 * become async just to show a poster; a failed fetch just leaves the <img>
 * without a src rather than breaking the render.
 */
export function setImageSrc(img, path) {
    if (!path) return;
    resolveImageURL(path)
        .then((url) => { img.src = url; })
        .catch(() => {});
}
