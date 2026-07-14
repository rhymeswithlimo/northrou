# Frontend

Decisions for the Northrou client. The client lives in `frontend/` and is
developed separately; it binds to the backend's stable HTTP API. This doc exists
so the framework and native-integration questions are settled and don't get
re-litigated.

## Framework: Tauri v2 (decided)

The client is **Tauri v2**: one HTML/CSS/JS codebase targeting desktop
(Win/Mac/Linux), iOS, and Android.

Why not Capacitor (the main alternative): Capacitor is mobile-only. Using it
would mean maintaining a second shell (Electron or Tauri) plus a second plugin
system for desktop. For a small self-hosted project, one codebase across all
three tiers wins. Choose Tauri specifically for the single cross-platform
codebase, not for reusing any existing desktop UI (a touch-first phone UI is
largely a rewrite of the screens regardless).

## Architecture: web UI + native where it's irreplaceable

Tauri (like all WebView shells) is built for "web UI + native *capabilities*,"
not "native navigation chrome wrapping web screens." Split the app accordingly:

- **Web (HTML/CSS/JS), shared across all platforms:** library browse, search,
  details, settings, all the chrome.
- **Native (per platform, via Tauri plugins):** the video player and OS
  integrations. This code is not shared: Swift on iOS, Kotlin on Android,
  Rust/native on desktop.

You do not get "mostly HTML/CSS" *and* "mostly native UIKit." Those pull in
opposite directions. If most of the UI needs to be native, the right tool is
React Native or fully native, not Tauri.

### The player is native on every platform

An `<video>` tag will not deliver 4K HEVC / TrueHD Atmos with
AirPlay/PiP/passthrough. Wire a native player in per platform:

- **iOS:** AVPlayer / AVKit (Swift) - fullscreen over the WebView.
- **Android:** ExoPlayer / Media3 (Kotlin).
- **Desktop:** the weak link. The OS WebView (WebKit / WebView2 /
  **WebKitGTK on Linux**) can't be trusted for HEVC direct play. Embed
  libmpv / libVLC via a Rust plugin for direct play; fall back to the backend
  transcode cascade only when the client genuinely can't decode. Don't transcode
  4K HEVC just to satisfy a WebView when the client hardware can handle it.

The web layer hands a stream URL to the native player on "play."

### Native UI elements (tabs, search) via plugins

You *can* have genuine native chrome (real `UITabBar`, native search, native
screens), not just web styled to look native. The mechanism:

- A Tauri v2 iOS plugin is a Swift class extending `Plugin`. It receives the
  `WKWebView` in `load(webview:)` and reaches the hosting `UIViewController` via
  `self.manager.viewController` (the same handle Tauri's own dialog plugin uses).
- With the view controller you can add native subviews over the WebView or
  present native view controllers. Pattern for a native tab bar: add a
  `UITabBar` as a subview pinned to the bottom; on tab selection call
  `trigger("tabChanged", data: [...])`; the web UI listens via
  `addPluginListener('<plugin>', 'tabChanged', handler)` and swaps routes.
  Native bar, web content underneath, one WebView.
- Same approach for native search (`UISearchBar` / `UISearchController` ->
  `trigger` -> web filters) or fully native screens (present a
  `UIViewController`).

Caveats to keep in mind:

- **Written twice.** Per-platform native code: Swift `UITabBar` on iOS, Kotlin
  `BottomNavigationView` on Android. No sharing.
- **No desktop equivalent.** Desktop WebViews have no native tab bar to add, so
  desktop chrome stays web-styled. You maintain native chrome for mobile *and*
  web chrome for desktop.
- **Manual layout.** Coordinating native overlays with WebView content
  (safe-area insets, keyboard) is fiddly and on you.
- **Keep native code in the plugin's own Swift/Kotlin package**, not in the
  generated `src-tauri/gen/apple` (or `gen/android`) project, which gets
  regenerated. The subview-via-plugin approach survives regens; editing the
  generated root view controller does not.

The SwiftUI in `frontend/swift/` (`ContentView`, `SecondView`) is design
reference for this chrome, not a standalone app: no `@main`, no Xcode project. It
gets adapted into the plugin's native views once Tauri is set up.

## App Store review

Using web content is **not** a rejection risk on its own. Hybrid apps ship
constantly, and the native player + integrations put this well past
Guideline 4.2 (Minimum Functionality). Bundle web assets locally (Tauri's
default); pointing a WebView at a remote website is what raises wrapper
suspicion. The real risks for a self-hosted media client:

1. **Reviewers can't test a self-hosted app** (Guideline 2.1). This is the most
   likely rejection cause. The reviewer needs a working backend with media to
   see anything. Provide a demo server + account, or a built-in demo mode with
   sample content, in the App Review notes.
2. **Media/piracy scrutiny** (Guideline 5.2, Intellectual Property). Frame it
   exactly as Infuse/Jellyfin clients do: a player for the user's own
   personal library on their own hardware. The app does not rip discs,
   circumvent DRM, or provide/index content. Keep that crisp in metadata.
3. **In-App Purchase** (Guideline 3.1.1), only if monetized: any paid tier must
   go through Apple IAP. A free/open-source client is unaffected.
4. **Entitlements / background modes.** Declare `UIBackgroundModes` and
   capabilities for background audio, PiP, and AirPlay correctly.

Precedent: Infuse, VLC, nPlayer, and Swiftfin (Jellyfin) are all on the
App Store connecting to personal/self-hosted media. The path is well-worn.
