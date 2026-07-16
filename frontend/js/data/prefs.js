// Playback preferences.
//
// These are per-device, not per-account: they describe what THIS screen and
// connection can handle, and they feed the client-capability query parameters
// on /api/media/{id}/stream. So they stay local rather than going to the server.

const KEY = 'northrou.prefs';

const DEFAULTS = {
    maxQuality: 'auto',      // auto | 2160 | 1080 | 720
    cellularQuality: '1080', // 2160 | 1080 | 720 | 480
    directPlay: true,
};

export const QUALITY_OPTIONS = [
    { value: 'auto', label: 'Auto' },
    { value: '2160', label: '4K' },
    { value: '1080', label: '1080p' },
    { value: '720', label: '720p' },
];

export const CELLULAR_OPTIONS = [
    { value: '2160', label: '4K' },
    { value: '1080', label: '1080p' },
    { value: '720', label: '720p' },
    { value: '480', label: '480p' },
];

export function getPrefs() {
    try {
        return { ...DEFAULTS, ...JSON.parse(localStorage.getItem(KEY) ?? '{}') };
    } catch {
        // Corrupt or unavailable storage (private mode, cleared mid-write)
        // should fall back to defaults, not break the settings page.
        return { ...DEFAULTS };
    }
}

export function setPref(key, value) {
    const next = { ...getPrefs(), [key]: value };
    try {
        localStorage.setItem(KEY, JSON.stringify(next));
    } catch {
        // Storage full or blocked: the setting still applies for this session.
    }
    return next;
}
