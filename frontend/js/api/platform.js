// Which platform this client is running on, and what that means for chrome.
//
// The shell reports it, rather than sniffing the user agent, which lies. It
// matters because the same web UI ships everywhere but must not draw chrome the
// platform is already drawing natively:
//
//   ios / android : a real tab bar exists over the WebView, so the web nav must
//                   go, or there are two.
//   desktop / web : no native bar to add, so the web nav is the nav.

const isTauri = () => typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;

let cached = null;

/** @returns {Promise<'ios'|'android'|'macos'|'windows'|'linux'|'web'>} */
export async function getPlatform() {
    if (cached) return cached;
    if (!isTauri()) {
        cached = 'web';
        return cached;
    }
    try {
        const { invoke } = await import('@tauri-apps/api/core');
        cached = await invoke('platform');
    } catch {
        cached = 'web';
    }
    return cached;
}

/** Mobile shells draw their own tab bar; everything else uses the web nav. */
export async function hasNativeChrome() {
    const p = await getPlatform();
    return p === 'ios' || p === 'android';
}
