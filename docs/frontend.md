# Frontend

Decisions for the Northrou client. The client lives in `frontend/` and binds to
the backend's stable HTTP API. This doc exists so the framework and native
integration questions are settled and don't get re-litigated.

## Framework: Tauri v2 (decided)

The client is **Tauri v2**: one HTML/CSS/JS codebase targeting desktop
(Win/Mac/Linux), iOS, and Android, plus the same build served straight from the
backend for a browser.

Why not Capacitor (the main alternative): Capacitor is mobile-only. Using it
would mean maintaining a second shell (Electron or Tauri) plus a second plugin
system for desktop. For a small self-hosted project, one codebase across all
three tiers wins. Choose Tauri specifically for the single cross-platform
codebase, not for reusing any existing desktop UI (a touch-first phone UI is
largely a rewrite of the screens regardless).

## Build

Vite, multi-page. Each screen is its own document with its own entry module,
which is what they already were; there is no SPA bundle and no router yet.

```sh
make frontend       # build + stage into backend/internal/web/assets for go:embed
make frontend-dev   # :5173 with /api proxied to a local server
cd frontend && npx tauri dev     # desktop shell against the dev server
cd frontend && npx tauri build   # desktop bundle
```

`base: './'` so one build works both served from the backend at `/` and loaded
by Tauri's asset protocol. Target is `es2022`/`safari16`: every platform runs a
modern WebView, so transpiled fallbacks would ship for nobody.

The build output is generated and **not committed**. Only `.gitkeep` is tracked,
which keeps `go build` working in a fresh clone; without a build the server says
so at `/` rather than 404ing, and the API still works.

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

### Reaching the server

The client does not start out knowing which box it belongs to; only a browser
served off the backend does. Everything else pairs first, and the transport is
resolved once at boot (`js/api/transport.js`, `js/api/connect.js`):

| Transport | When | How |
|---|---|---|
| same-origin | browser, served by the backend | plain `fetch` |
| tunnel | app | WebRTC data channel |

An app is never same-origin with the box, so it always reaches it peer-to-peer:
there is no direct-to-a-LAN-address path. A single hosted-broker WebRTC tunnel is
one code path to get right, works identically at home and away, and needs no
address discovery or `/api/health` probing of guessed hosts.

The tunnel client is **JS, not Rust**: the WebView already has a WebRTC stack on
every platform Tauri targets, and a JS client keeps working in a plain browser,
which a Rust one would not. Its wire format must match
`internal/remote/tunnel.go` byte for byte. One gotcha, pinned by
`internal/remote/framing_test.go`: a data channel is SCTP (message-oriented) but
the server reads it as a stream with a 4-byte `io.ReadFull`, and pion returns
`ErrShortBuffer` if the message is bigger than the read. Frames must therefore be
sent as **two messages**, header then payload, exactly as Go's `writeFrame` does.

### Page boot: guard, then reveal

Each entry module runs a boot guard before it renders. `requireServer()` resolves
the transport (redirecting to `connect.html` when no server is paired); the app
index additionally checks first-run/sign-in state and routes to `setup.html` or
`login.html` as needed. To avoid flashing the wrong screen before a redirect,
every page starts hidden - an inline `<head>` script sets a `booting` class that
`base.css` hides the body on - and calls `reveal()` (`js/lib/dom.js`) only on the
paths that **stay** (rendered, empty, or error), never before a redirect. A new
page must call `reveal()` on each of its stay paths; a timeout backstop reveals
anyway if one is missed, so the failure mode is a flash, not a blank screen.

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

This is implemented in `frontend/plugins/northrou-native/`:

| Piece | Where |
|---|---|
| iOS `UITabBar` | `ios/Sources/NorthrouNative/NorthrouNativePlugin.swift` |
| Android `BottomNavigationView` | `android/.../NorthrouNativePlugin.kt` |
| Rust binding (no-ops on desktop) | `src/lib.rs` |
| JS API | `guest-js/index.ts`, `js/components/native-chrome.js` |

The web half hides its own chrome when the native chrome is up, keyed on
`<html data-native-chrome>`: the web nav goes on mobile (or the app wears two
tab bars), and the detail modal's close button goes on iOS (the presented
sheet's own button drives it instead, see `frontend/swift/WatchView.swift`). The
native bar reports its height into `--native-tabbar-height` so the page reserves
exactly the right room rather than guessing at a device and safe area.

Caveats to keep in mind:

- **Written twice.** Per-platform native code: Swift `UITabBar` on iOS, Kotlin
  `BottomNavigationView` on Android. No sharing.
- **No desktop equivalent.** Desktop WebViews have no native tab bar to add, so
  desktop chrome stays web-styled. You maintain native chrome for mobile *and*
  web chrome for desktop. The Rust binding no-ops there so the web layer can
  call it unconditionally.
- **Manual layout.** Coordinating native overlays with WebView content
  (safe-area insets, keyboard) is fiddly and on you.
- **Keep native code in the plugin's own Swift/Kotlin package**, not in the
  generated `src-tauri/gen/apple` (or `gen/android`) project, which gets
  regenerated. The subview-via-plugin approach survives regens; editing the
  generated root view controller does not.

The SwiftUI in `frontend/swift/` (`ContentView`, `WatchView`) is design
reference for this chrome, not a standalone app: no `@main`, no Xcode project.
It is the source of truth for what the chrome looks like, and the plugin mirrors
it item for item (same titles, SF Symbols, order, search last). It needs
**iOS 18+** to typecheck: the `Tab(_:systemImage:value:)` initialiser it uses is
iOS 18 API.

## Per-platform status

| Target | Built | Notes |
|---|---|---|
| Browser (served by backend) | yes | verified end to end against a real server |
| macOS desktop | yes | `Northrou.app` bundles and launches |
| Windows / Linux desktop | not built here | pure-web + Rust; no platform-specific code beyond the player |
| iOS | plugin written, not built | needs an Apple developer team for `tauri ios build` |
| Android | plugin written, not built | needs the Android SDK/NDK |

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
4. **Sign in with Apple** (Guideline 4.8), if social sign-in ships: offering
   Google login on iOS requires offering an equivalent privacy-preserving
   option, which Sign in with Apple satisfies. Both are implemented; enable them
   together or neither.
5. **Entitlements / background modes.** Declare `UIBackgroundModes` and
   capabilities for background audio, PiP, and AirPlay correctly.

Precedent: Infuse, VLC, nPlayer, and Swiftfin (Jellyfin) are all on the
App Store connecting to personal/self-hosted media. The path is well-worn.
