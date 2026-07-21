// Admin seam.
//
// Reads are open to any signed-in profile: they expose status, not controls.
// Mutations need the elevated token from /api/admin/verify-otp, which lives in
// api/session.js for the ~10 minutes it is valid.

import { get, post, patch } from '../api/client.js';
import * as session from '../api/session.js';

export const isElevated = session.isElevated;

export async function requestOtp() {
    return post('/api/admin/request-otp', {});
}

export async function verifyOtp(otp) {
    const res = await post('/api/admin/verify-otp', { otp });
    session.setElevation(res.access_token, res.expires_at);
    return res;
}

/* ---------- reads ---------- */

export const getConfig = () => get('/api/admin/config');
export const getHardware = () => get('/api/admin/hardware');
export const getScan = () => get('/api/admin/scan');
export const getStreams = () => get('/api/admin/streams');
export const checkUpdate = () => get('/api/admin/update');
export const getUnmatched = () => get('/api/unmatched');
export const searchTMDB = (q, kind) =>
    get(`/api/admin/tmdb-search?q=${encodeURIComponent(q)}&kind=${kind}`);

/* ---------- mutations (elevated) ---------- */

export const patchConfig = (body) => patch('/api/admin/config', body, { elevated: true });
export const startScan = () => post('/api/admin/scan', {}, { elevated: true });
export const applyUpdate = () => post('/api/admin/update', {}, { elevated: true });
export const manualMatch = (body) => post('/api/admin/match', body, { elevated: true });
