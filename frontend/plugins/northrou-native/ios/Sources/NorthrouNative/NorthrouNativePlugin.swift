// Northrou's native iOS chrome.
//
// Tauri is built for "web UI + native capabilities", not native navigation
// wrapping web screens, so the split is deliberate: every screen is web, and
// the things a WebView genuinely cannot do well are native. On iOS that is the
// tab bar (a web imitation never feels right: no blur, no haptics, wrong
// safe-area behaviour, wrong swipe) and, shortly, the player.
//
// The design here is not invented: it mirrors frontend/swift/ContentView.swift
// item for item -- same titles, same SF Symbols, same order, same search role.
// That file stays the design reference. A SwiftUI TabView renders a UITabBar
// underneath anyway, so building the UITabBar directly is the same chrome
// without hosting a SwiftUI tree that owns no content.
//
// This lives in the plugin's own package on purpose. src-tauri/gen/apple is
// regenerated, and anything edited there is lost; a plugin subview survives.

import UIKit
import WebKit
import Tauri

/// A tab, mirroring the `Tabs` enum in ContentView.swift.
struct TabItem: Decodable {
    let key: String      // "home" | "films" | "shows" | "search"
    let title: String
    let systemImage: String
}

struct SetTabsArgs: Decodable {
    let tabs: [TabItem]
    let selected: String?
}

struct SetTabArgs: Decodable {
    let key: String
}

struct ShowChromeArgs: Decodable {
    let visible: Bool
}

class NorthrouNativePlugin: Plugin, UITabBarDelegate {
    private var tabBar: UITabBar?
    private var keys: [String] = []
    private weak var webView: WKWebView?

    override func load(webview: WKWebView) {
        self.webView = webview
    }

    /// Build (or rebuild) the tab bar and pin it to the bottom.
    @objc public func set_tabs(_ invoke: Invoke) throws {
        let args = try invoke.parseArgs(SetTabsArgs.self)

        DispatchQueue.main.async {
            guard let host = self.manager.viewController?.view else {
                invoke.reject("no host view controller")
                return
            }

            self.tabBar?.removeFromSuperview()
            self.keys = args.tabs.map(\.key)

            let bar = UITabBar()
            bar.delegate = self
            bar.translatesAutoresizingMaskIntoConstraints = false
            bar.items = args.tabs.enumerated().map { i, t in
                let item = UITabBarItem(
                    title: t.title,
                    image: UIImage(systemName: t.systemImage),
                    tag: i
                )
                return item
            }

            if let selected = args.selected, let i = self.keys.firstIndex(of: selected) {
                bar.selectedItem = bar.items?[i]
            } else {
                bar.selectedItem = bar.items?.first
            }

            host.addSubview(bar)
            NSLayoutConstraint.activate([
                bar.leadingAnchor.constraint(equalTo: host.leadingAnchor),
                bar.trailingAnchor.constraint(equalTo: host.trailingAnchor),
                // Pin to the view, not the safe area: a UITabBar draws its own
                // background down through the home indicator and lays its items
                // out above it. Pinning to the safe area leaves a strip of page
                // showing underneath.
                bar.bottomAnchor.constraint(equalTo: host.bottomAnchor),
            ])
            self.tabBar = bar

            // The web page must not draw its last row underneath the bar.
            self.applyInset(height: bar.intrinsicContentSize.height)
            invoke.resolve()
        }
    }

    /// Select a tab from the web side, so the bar follows in-page navigation
    /// (tapping a card, going back) instead of drifting out of sync.
    @objc public func set_tab(_ invoke: Invoke) throws {
        let args = try invoke.parseArgs(SetTabArgs.self)
        DispatchQueue.main.async {
            guard let bar = self.tabBar,
                  let i = self.keys.firstIndex(of: args.key),
                  let items = bar.items, i < items.count else {
                invoke.resolve()
                return
            }
            bar.selectedItem = items[i]
            invoke.resolve()
        }
    }

    /// Hide the chrome for immersive content (a detail modal, playback).
    @objc public func show_chrome(_ invoke: Invoke) throws {
        let args = try invoke.parseArgs(ShowChromeArgs.self)
        DispatchQueue.main.async {
            guard let bar = self.tabBar else {
                invoke.resolve()
                return
            }
            UIView.animate(withDuration: 0.2) {
                bar.alpha = args.visible ? 1 : 0
            } completion: { _ in
                bar.isHidden = !args.visible
            }
            self.applyInset(height: args.visible ? bar.intrinsicContentSize.height : 0)
            invoke.resolve()
        }
    }

    func tabBar(_ tabBar: UITabBar, didSelect item: UITabBarItem) {
        guard item.tag < keys.count else { return }
        // The web side listens for this and swaps route. The native bar owns
        // the selection; the web owns the content.
        trigger("tabChanged", data: ["key": keys[item.tag]])
    }

    /// Tell the page how much room the bar takes, as a CSS variable the layout
    /// can add to its own padding.
    private func applyInset(height: CGFloat) {
        let js = "document.documentElement.style.setProperty('--native-tabbar-height', '\(Int(height))px')"
        webView?.evaluateJavaScript(js)
    }
}

@_cdecl("init_plugin_northrou_native")
func initPlugin() -> Plugin {
    return NorthrouNativePlugin()
}
