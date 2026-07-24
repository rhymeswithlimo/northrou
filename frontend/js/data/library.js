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

/** Home rows come back as [{key, title, subtitle?, confidence, items}]. */
export async function getHomeRows() {
    const rows = await fetchHome();
    return (rows ?? []).map((r) => ({
        key: r.key,
        title: r.title,
        subtitle: r.subtitle,
        items: (r.items ?? []).map(toCard),
    }));
}

/**
 * The hero pool is every title across the home rows, deduped. The featured
 * hero rotates through these at random, so it draws from the whole set of
 * recommendations rather than just /api/home's single top pick. Returns
 * lightweight {kind, id} refs; getHeroItem hydrates one when it's shown.
 */
export async function getHeroPool() {
    const rows = await fetchHome();
    const seen = new Set();
    const pool = [];
    for (const r of rows ?? []) {
        for (const it of r.items ?? []) {
            if (!it.kind || it.id == null) continue;
            const key = `${it.kind}:${it.id}`;
            if (seen.has(key)) continue;
            seen.add(key);
            pool.push({ kind: it.kind, id: it.id });
        }
    }
    return pool;
}

/**
 * Hero-shaped data for a single title. Lightweight on purpose: it hits only the
 * movie/show endpoint (not getDetail, which also fans out to similar + resume),
 * because the hero just needs a backdrop and a meta line. Returns null when the
 * title is gone or has no backdrop to show.
 */
export async function getHeroItem(kind, id) {
    try {
        const path = kind === 'show' ? `/api/shows/${id}` : `/api/movies/${id}`;
        const d = await get(path);
        // The hero fills the screen, so prefer the dedicated high-res backdrop
        // (>=2560x1440) when a re-scan has cached one; fall back to the regular
        // w1280 backdrop otherwise. Either is fine; no backdrop at all skips it.
        const backdrop = d?.hero_backdrop_url || d?.backdrop_url;
        if (!d || !backdrop) return null;
        return {
            kind,
            id,
            title: d.title,
            year: d.year,
            genres: d.genres,
            runtime: d.runtime,
            episode_count: d.seasons?.reduce((n, s) => n + (s.episodes?.length ?? 0), 0),
            backdrop_url: backdrop,
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

// Home rows ship a bare cache-relative path ("w500/xxx.jpg", see
// docs/architecture.md's home-row shape); every other endpoint already
// returns a ready-to-use "/api/images/..." URL. Normalize both to the latter,
// or the raw path renders as a broken image with the API prefix missing.
function toImageURL(path) {
    if (!path) return undefined;
    return path.startsWith('/api/images/') ? path : `/api/images/${path}`;
}

function toCard(item) {
    return {
        kind: item.kind,
        id: item.id,
        title: item.title,
        year: item.year,
        // Home rows use poster_path; search and similar use poster_url.
        poster_url: toImageURL(item.poster_url ?? item.poster_path),
        // Similar-title results carry a "why this is here" reason; other
        // endpoints omit it and this stays undefined.
        reason: item.reason,
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
        logo_url: d.logo_url,
        stream_url: d.stream_url,
        // Cast and crew stay as SEPARATE arrays, each with its person `id`
        // preserved: detail.js's castCrew() merges them (directors first, then
        // top-billed actors) and dedupes by id. Flattening them into one array
        // here - or dropping the id - makes that dedupe collapse everyone whose
        // id is undefined into a single entry, which is why the row once showed
        // just the director. Keep the id.
        cast: (d.cast ?? []).map((c) => ({
            id: c.id, name: c.name, role: c.role, profile_url: c.profile_url,
        })),
        crew: (d.crew ?? []).map((c) => ({
            id: c.id, name: c.name, role: c.role, profile_url: c.profile_url,
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
        // What playback should record progress against: for a show this is the
        // in-progress episode (match.id is the episode id), not the show.
        watch: { kind: match.kind, id: match.id },
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
