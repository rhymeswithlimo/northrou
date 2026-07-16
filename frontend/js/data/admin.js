// Admin seam.
//
// Reads are open to any signed-in profile (they expose status, not controls).
// Mutations need an elevated token from POST /api/admin/verify-otp, which is
// held here for the ~10 minutes it lives and passed as the bearer.
//
//   requestOtp()  -> POST /api/admin/request-otp
//   verifyOtp()   -> POST /api/admin/verify-otp   {otp} -> {access_token, expires_at}
//   getHardware() -> GET  /api/admin/hardware
//   getScan()     -> GET  /api/admin/scan
//   startScan()   -> POST /api/admin/scan          (elevated)
//   getConfig()   -> GET  /api/admin/config        (Phase 3)
//   patchConfig() -> PATCH /api/admin/config       (elevated, Phase 3)
//   checkUpdate() -> GET  /api/admin/update

let elevatedToken = null;
let elevatedUntil = 0;

export const isElevated = () => Boolean(elevatedToken) && Date.now() < elevatedUntil;

export async function requestOtp() {
    return { ok: true };
}

export async function verifyOtp(otp) {
    // The real call returns 401 on a bad code; the dev stand-in accepts any
    // six digits so the unlocked panel can be built and reviewed.
    if (!/^\d{6}$/.test(otp)) {
        const err = new Error('Invalid or expired code.');
        err.status = 401;
        throw err;
    }
    elevatedToken = 'dev';
    elevatedUntil = Date.now() + 10 * 60 * 1000;
    return { expires_at: new Date(elevatedUntil).toISOString() };
}

export async function getServer() {
    return { name: 'This server', address: 'localhost:7777', status: 'connected' };
}

export async function getHardware() {
    return {
        backend: 'videotoolbox',
        available: ['videotoolbox'],
        estimated_capacity: 4,
        active_transcodes: 0,
        ffmpeg_ready: true,
    };
}

export async function getScan() {
    return { running: false, last_scan: null, items: 0 };
}

export async function startScan() {
    return { ok: true };
}

export async function getConfig() {
    return {
        movie_dirs: [],
        show_dirs: [],
        prefer_system_ffmpeg: false,
        mail_mode: 'relay',
        remote_enabled: true,
        max_transcodes: 0, // 0 = auto
    };
}

export async function patchConfig(patch) {
    return patch;
}

export async function checkUpdate() {
    return { current: null, latest: null, update_available: false };
}
