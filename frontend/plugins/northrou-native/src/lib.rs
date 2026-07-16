//! Native chrome and player integration for Northrou.
//!
//! The web layer owns every screen. This plugin owns the pieces a WebView
//! cannot do convincingly:
//!
//! - **The tab bar on mobile.** A web imitation of a UITabBar is always
//!   slightly wrong: no blur, no haptics, wrong safe-area behaviour, wrong
//!   swipe. So iOS gets a real UITabBar and Android a real
//!   BottomNavigationView, with the web content underneath. Desktop keeps the
//!   web nav, because a desktop WebView has no native bar to add.
//! - **The player** (next): an <video> tag will not deliver 4K HEVC / TrueHD
//!   Atmos with AirPlay/PiP/passthrough.
//!
//! None of this is shared code. Swift on iOS, Kotlin on Android, and that is
//! the deal Tauri makes: one web codebase, native where it is irreplaceable.

use serde::{Deserialize, Serialize};
use tauri::{
    plugin::{Builder, TauriPlugin},
    AppHandle, Runtime,
};

#[cfg(mobile)]
use tauri::plugin::PluginHandle;

#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error(transparent)]
    Tauri(#[from] tauri::Error),
    #[cfg(mobile)]
    #[error(transparent)]
    PluginInvoke(#[from] tauri::plugin::mobile::PluginInvokeError),
    #[error("native chrome is not available on this platform")]
    Unsupported,
}

impl Serialize for Error {
    // std's Result, not the alias below, which fixes the error type.
    fn serialize<S: serde::Serializer>(&self, s: S) -> std::result::Result<S::Ok, S::Error> {
        s.serialize_str(&self.to_string())
    }
}

type Result<T> = std::result::Result<T, Error>;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TabItem {
    pub key: String,
    pub title: String,
    #[serde(rename = "systemImage")]
    pub system_image: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SetTabsArgs {
    pub tabs: Vec<TabItem>,
    pub selected: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SetTabArgs {
    pub key: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ShowChromeArgs {
    pub visible: bool,
}

#[cfg(mobile)]
pub struct NorthrouNative<R: Runtime>(PluginHandle<R>);

#[cfg(not(mobile))]
pub struct NorthrouNative<R: Runtime>(std::marker::PhantomData<fn() -> R>);

impl<R: Runtime> NorthrouNative<R> {
    #[cfg(mobile)]
    pub fn set_tabs(&self, args: SetTabsArgs) -> Result<()> {
        self.0.run_mobile_plugin("set_tabs", args).map_err(Into::into)
    }

    // Desktop has no native tab bar to add, so these are no-ops rather than
    // errors: the web layer calls them unconditionally and keeps its own nav.
    #[cfg(not(mobile))]
    pub fn set_tabs(&self, _args: SetTabsArgs) -> Result<()> {
        Ok(())
    }

    #[cfg(mobile)]
    pub fn set_tab(&self, args: SetTabArgs) -> Result<()> {
        self.0.run_mobile_plugin("set_tab", args).map_err(Into::into)
    }

    #[cfg(not(mobile))]
    pub fn set_tab(&self, _args: SetTabArgs) -> Result<()> {
        Ok(())
    }

    #[cfg(mobile)]
    pub fn show_chrome(&self, args: ShowChromeArgs) -> Result<()> {
        self.0.run_mobile_plugin("show_chrome", args).map_err(Into::into)
    }

    #[cfg(not(mobile))]
    pub fn show_chrome(&self, _args: ShowChromeArgs) -> Result<()> {
        Ok(())
    }
}

#[tauri::command]
async fn set_tabs<R: Runtime>(app: AppHandle<R>, args: SetTabsArgs) -> Result<()> {
    use tauri::Manager;
    app.state::<NorthrouNative<R>>().set_tabs(args)
}

#[tauri::command]
async fn set_tab<R: Runtime>(app: AppHandle<R>, args: SetTabArgs) -> Result<()> {
    use tauri::Manager;
    app.state::<NorthrouNative<R>>().set_tab(args)
}

#[tauri::command]
async fn show_chrome<R: Runtime>(app: AppHandle<R>, args: ShowChromeArgs) -> Result<()> {
    use tauri::Manager;
    app.state::<NorthrouNative<R>>().show_chrome(args)
}

pub fn init<R: Runtime>() -> TauriPlugin<R> {
    Builder::new("northrou-native")
        .invoke_handler(tauri::generate_handler![set_tabs, set_tab, show_chrome])
        .setup(|app, _api| {
            use tauri::Manager;

            #[cfg(target_os = "ios")]
            let handle = _api.register_ios_plugin(init_plugin_northrou_native)?;
            #[cfg(target_os = "android")]
            let handle = _api.register_android_plugin("sh.northrou.native", "NorthrouNativePlugin")?;

            #[cfg(mobile)]
            app.manage(NorthrouNative(handle));
            #[cfg(not(mobile))]
            app.manage(NorthrouNative::<R>(std::marker::PhantomData));
            #[cfg(not(mobile))]
            let _ = &app;

            Ok(())
        })
        .build()
}

#[cfg(target_os = "ios")]
tauri::ios_plugin_binding!(init_plugin_northrou_native);
