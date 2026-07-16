// The profiles data seam. Swaps to GET /api/profiles at integration; the
// shape here already matches ({id, name, avatar?}).

const FIXTURES = [
    { id: 1, name: 'Tomas' },
    { id: 2, name: 'Kira' },
    { id: 3, name: 'Ivan' },
    { id: 4, name: 'Lena' },
];

export async function listProfiles() {
    return FIXTURES;
}

export const MAX_PROFILES = 6;
