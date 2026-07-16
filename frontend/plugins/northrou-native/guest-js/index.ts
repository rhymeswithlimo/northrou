import { invoke, addPluginListener } from '@tauri-apps/api/core';

export interface TabItem {
  key: string;
  title: string;
  systemImage: string;
}

/** Build the native tab bar. A no-op on desktop, which keeps its web nav. */
export const setTabs = (tabs: TabItem[], selected?: string) =>
  invoke('plugin:northrou-native|set_tabs', { args: { tabs, selected } });

/** Move the native selection to follow in-page navigation. */
export const setTab = (key: string) =>
  invoke('plugin:northrou-native|set_tab', { args: { key } });

/** Hide the chrome for immersive content (a detail modal, playback). */
export const showChrome = (visible: boolean) =>
  invoke('plugin:northrou-native|show_chrome', { args: { visible } });

/** Fires when the user taps a native tab. */
export const onTabChanged = (handler: (key: string) => void) =>
  addPluginListener('northrou-native', 'tabChanged', (e: { key: string }) => handler(e.key));
