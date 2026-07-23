// Admin seam.
//
// Reads are open to any signed-in profile: they expose status, not controls.
// Mutations are allowed only from a local connection (a browser on the server's
// own network, or the CLI); a remote app calling them gets a 403. There is no
// elevation token any more -- admin is a property of the request's transport.

import { get, post, patch, del } from '../api/client.js';

/* ---------- reads ---------- */

export const getConfig = () => get('/api/admin/config');
export const getHardware = () => get('/api/admin/hardware');
export const getScan = () => get('/api/admin/scan');
export const getStreams = () => get('/api/admin/streams');
export const checkUpdate = () => get('/api/admin/update');
export const getUnmatched = () => get('/api/unmatched');
export const getLogs = (n = 500) => get('/api/admin/logs', { query: { n }, text: true });
export const getSessions = () => get('/api/admin/sessions');
export const searchTMDB = (q, kind) =>
    get(`/api/admin/tmdb-search?q=${encodeURIComponent(q)}&kind=${kind}`);

/* ---------- mutations (local connection only) ---------- */

export const patchConfig = (body) => patch('/api/admin/config', body);
export const startScan = () => post('/api/admin/scan', {});
export const applyUpdate = () => post('/api/admin/update', {});
export const manualMatch = (body) => post('/api/admin/match', body);
export const revokeSession = (id) => del(`/api/admin/sessions/${encodeURIComponent(id)}`);
export const rotateConnectionCode = () => post('/api/admin/connection-code/rotate', {});
