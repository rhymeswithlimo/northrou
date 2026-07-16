// Profiles seam.
//
// Any signed-in profile may list, add or rename. Deleting is destructive (it
// takes that viewer's whole watch history with it) so it needs an elevated
// token, the same admin OTP that gates server mutations.

import { get, post, patch, del } from '../api/client.js';

export const MAX_PROFILES = 6;

export async function listProfiles() {
    const res = await get('/api/profiles');
    return res?.profiles ?? [];
}

export async function createProfile(name, avatar) {
    return post('/api/profiles', { name, avatar });
}

export async function renameProfile(id, name, avatar) {
    return patch(`/api/profiles/${id}`, { name, avatar });
}

/** Requires elevation. 409 when it is the last profile: never leave zero. */
export async function deleteProfile(id) {
    return del(`/api/profiles/${id}`, { elevated: true });
}
