// Native chrome integration.
//
// On iOS and Android a real tab bar sits over the WebView. This is the web half
// of that arrangement: hide the web nav so the app isn't wearing two, follow
// taps on the native bar, and keep the native selection in step with in-page
// navigation.
//
// The tabs mirror frontend/swift/ContentView.swift exactly -- same keys, same
// titles, same SF Symbols, same order, search last. That file is the design
// reference; this is the wiring.

import { hasNativeChrome, getPlatform } from '../api/platform.js';

const TABS = [
    { key: 'home', title: 'Home', systemImage: 'house' },
    { key: 'films', title: 'Movies', systemImage: 'film' },
    { key: 'shows', title: 'Shows', systemImage: 'rectangle.on.rectangle' },
    { key: 'search', title: 'Search', systemImage: 'magnifyingglass' },
];

const ROUTES = {
    home: 'index.html',
    films: 'index.html#films',
    shows: 'index.html#shows',
    search: 'index.html#search',
};

let plugin = null;

async function load() {
    if (plugin) return plugin;
    plugin = await import('../../plugins/northrou-native/guest-js/index.js');
    return plugin;
}

/**
 * Mount native chrome if the platform has it.
 * @param {{current?: string, onTab?: (key: string) => void}} opts
 * @returns {Promise<boolean>} whether native chrome is in play
 */
export async function mountNativeChrome({ current = 'home', onTab } = {}) {
    if (!(await hasNativeChrome())) return false;

    // Two tab bars is worse than either. The native one wins: it is the one
    // that gets the blur, the haptics and the safe area right.
    document.documentElement.dataset.nativeChrome = 'true';

    try {
        const api = await load();
        await api.setTabs(TABS, current);
        await api.onTabChanged((key) => {
            if (onTab) onTab(key);
            else if (ROUTES[key]) window.location.assign(ROUTES[key]);
        });
        return true;
    } catch {
        // The plugin is missing or failed. Fall back to the web nav rather than
        // leaving the app with no navigation at all.
        delete document.documentElement.dataset.nativeChrome;
        return false;
    }
}

/** Move the native selection to follow in-page navigation. */
export async function selectNativeTab(key) {
    if (!(await hasNativeChrome())) return;
    try {
        (await load()).setTab(key);
    } catch { /* chrome is decoration; never break navigation over it */ }
}

/** Hide the tab bar for immersive content (a detail modal, playback). */
export async function setNativeChromeVisible(visible) {
    if (!(await hasNativeChrome())) return;
    try {
        (await load()).showChrome(visible);
    } catch { /* as above */ }
}

export { getPlatform };
