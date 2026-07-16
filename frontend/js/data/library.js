// The library data seam.
//
// Every screen reads through this module, so integration is a change here and
// nowhere else: each function keeps its signature and return shape and starts
// awaiting the real endpoint instead of the dev fixtures.
//
//   getHero()             -> GET /api/home (first item of the top row)
//   getContinueWatching() -> GET /api/continue-watching
//   getHomeRows()         -> GET /api/home
//   getDetail(kind, id)   -> GET /api/movies/{id} | /api/shows/{id}

import * as fixtures from './fixtures.dev.js';

export async function getHero() {
    return fixtures.hero;
}

export async function getContinueWatching() {
    return fixtures.continueWatching;
}

export async function getHomeRows() {
    return fixtures.homeRows;
}

export async function getDetail(kind, id) {
    return fixtures.detailFor(kind, Number(id));
}
