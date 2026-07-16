// The library data seam. Maps API responses to what the renderers expect.

import { get, post } from '../api/client.js';

// Home is the page's most expensive call and both the rows and the hero derive
// from it, so it is fetched once per load and shared. Not a cache with a TTL:
// just deduplication within one render, dropped by loadHome() on the next.
let homePromise = null;

const fetchHome = () => {
    if (!homePromise) homePromise = get('/api/home');
    return homePromise;
};

/** Drop the shared home response so the next read re-fetches. */
export const invalidateHome = () => { homePromise = null; };

/** Home rows come back as [{key, title, confidence, items}]. */
export async function getHomeRows() {
    const rows = await fetchHome();
    return (rows ?? []).map((r) => ({
        key: r.key,
        title: r.title,
        items: (r.items ?? []).map(toCard),
    }));
}

/**
 * The hero is the first item of the first home row: the strongest single
 * recommendation the engine has. There is no separate "featured" endpoint, and
 * inventing one would just be /api/home's top pick under another name.
 */
export async function getHero() {
    const rows = await fetchHome();
    const item = rows?.[0]?.items?.[0];
    if (!item) return null;

    // Home items are poster-shaped summaries; the hero needs a backdrop and a
    // meta line, so fetch the detail for the one title we're featuring.
    try {
        const detail = await getDetail(item.kind, item.id);
        return detail && {
            kind: item.kind,
            id: item.id,
            title: detail.title,
            year: detail.year,
            genres: detail.genres,
            runtime: detail.runtime,
            episode_count: detail.seasons?.reduce((n, s) => n + (s.episodes?.length ?? 0), 0),
            backdrop_url: detail.backdrop_url,
        };
    } catch {
        // A hero is decoration; never let it take the page down.
        return null;
    }
}

export async function getContinueWatching() {
    const items = await get('/api/continue-watching');
    return (items ?? []).map((it) => ({
        kind: it.kind,
        id: it.id,
        show_id: it.show_id,
        title: it.title,
        season: it.season,
        number: it.number,
        position_sec: it.position_sec,
        duration_sec: it.duration_sec,
        backdrop_url: it.backdrop_url,
        stream_url: it.stream_url,
    }));
}

function toCard(item) {
    return {
        kind: item.kind,
        id: item.id,
        title: item.title,
        year: item.year,
        // Home rows use poster_path; search and similar use poster_url.
        poster_url: item.poster_url ?? item.poster_path,
    };
}

/** Movie or show detail, shaped for the detail modal. */
export async function getDetail(kind, id) {
    const path = kind === 'show' ? `/api/shows/${id}` : `/api/movies/${id}`;
    const d = await get(path);
    if (!d) return null;

    const [similar, resume] = await Promise.all([
        getSimilar(kind, id),
        resumeFor(kind, id),
    ]);

    return {
        kind,
        id: d.id,
        title: d.title,
        year: d.year,
        rating: d.rating,
        certification: d.certification,
        genres: d.genres,
        tagline: d.tagline,
        overview: d.overview,
        runtime: d.runtime,
        backdrop_url: d.backdrop_url,
        poster_url: d.poster_url,
        stream_url: d.stream_url,
        // Cast and crew render in one row, director first: on a detail screen
        // "who made this" belongs beside "who's in it".
        cast: [...(d.crew ?? []), ...(d.cast ?? [])].map((c) => ({
            name: c.name,
            role: c.role,
            profile_url: c.profile_url,
        })),
        seasons: d.seasons?.map((s) => ({
            number: s.number,
            episodes: (s.episodes ?? []).map((e) => ({
                id: e.id,
                season: e.season,
                number: e.number,
                title: e.title,
                overview: e.overview,
                runtime: e.runtime,
                still_url: e.still_url,
                stream_url: e.stream_url,
                // Per-episode progress isn't on the show payload; the
                // continue-watching row carries it for the one in flight.
                position_sec: 0,
                duration_sec: 0,
            })),
        })),
        similar,
        resume,
    };
}

async function getSimilar(kind, id) {
    const path = kind === 'show' ? `/api/shows/${id}/similar` : `/api/movies/${id}/similar`;
    try {
        return (await get(path) ?? []).map(toCard);
    } catch {
        // "More like this" is a nicety; a failure here must not blank the modal.
        return [];
    }
}

/** The resume state for a title, derived from what's in progress. */
async function resumeFor(kind, id) {
    let items;
    try {
        items = await get('/api/continue-watching');
    } catch {
        return null;
    }

    const match = (items ?? []).find((it) =>
        kind === 'show'
            ? it.kind === 'episode' && it.show_id === Number(id)
            : it.kind === 'movie' && it.id === Number(id));
    if (!match) return null;

    const label = match.kind === 'episode'
        ? `RESUME S${String(match.season).padStart(2, '0')}:E${String(match.number).padStart(2, '0')}`
        : 'RESUME';

    return {
        label,
        position_sec: match.position_sec,
        duration_sec: match.duration_sec,
        stream_url: match.stream_url,
    };
}

/** Search across movies and shows. */
export async function search(q, { signal } = {}) {
    const items = await get('/api/search', { query: { q }, signal });
    return (items ?? []).map(toCard);
}

/** Record playback progress. */
export async function recordWatch({ kind, id, position, duration }) {
    return post('/api/watch', {
        media_kind: kind === 'episode' ? 'episode' : 'movie',
        media_id: Number(id),
        position,
        duration,
    });
}
