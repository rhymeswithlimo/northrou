//! Northrou client shell.
//!
//! The web layer owns every screen; this side owns the things a WebView cannot
//! do. Right now that is the HTTP plugin, which exists for a specific reason:
//! the app talks to a server on another machine, so a WebView `fetch` would be
//! a cross-origin request to a box that has no idea what CORS is. Routing
//! through Rust sidesteps that entirely (see js/api/transport.js).
//!
//! It also registers the northrou-native plugin, which owns the mobile tab bar:
//! a real UITabBar on iOS and BottomNavigationView on Android, with the web
//! content underneath. Desktop has no native bar to add, so those calls are
//! no-ops there and the web nav stays.
//!
//! Still to come, per docs/frontend.md: the native player. An <video> tag will
//! not deliver 4K HEVC / TrueHD Atmos with AirPlay/PiP/passthrough.

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_http::init())
        .plugin(tauri_plugin_northrou_native::init())
        .invoke_handler(tauri::generate_handler![platform])
        .run(tauri::generate_context!())
        .expect("error while running northrou");
}

/// Which platform the shell is running on.
///
/// The web layer needs this to decide chrome: on iOS the tab bar and the modal
/// close button are native, so the web equivalents must be hidden rather than
/// drawn twice. Reported by the shell rather than sniffed from the user agent,
/// which lies.
#[tauri::command]
fn platform() -> &'static str {
    if cfg!(target_os = "ios") {
        "ios"
    } else if cfg!(target_os = "android") {
        "android"
    } else if cfg!(target_os = "macos") {
        "macos"
    } else if cfg!(target_os = "windows") {
        "windows"
    } else {
        "linux"
    }
}
