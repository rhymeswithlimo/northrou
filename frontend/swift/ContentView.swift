// Main page for northrou.
//
// This is the design reference for the iOS chrome, not a standalone app: there
// is no @main and no Xcode project, exactly as docs/frontend.md describes. The
// tabs below are the source of truth for what the real chrome looks like, and
// they are mirrored item for item -- same titles, same SF Symbols, same order,
// search last -- by:
//
//   plugins/northrou-native/ios/.../NorthrouNativePlugin.swift  (the real UITabBar)
//   js/components/native-chrome.js                              (the web half)
//
// The placeholder colours are gone because the content is not native: every
// screen is web, rendered by the WebView underneath, with this bar over it.
// Preview keeps a stand-in so the layout is still viewable in Xcode.

import SwiftUI

enum Tabs: String {
    case home, films, shows, search
}

/// The tab bar's shape, shared with the plugin that builds the real one.
struct TabSpec {
    let tab: Tabs
    let title: String
    let systemImage: String

    static let all: [TabSpec] = [
        .init(tab: .home, title: "Home", systemImage: "house"),
        .init(tab: .films, title: "Movies", systemImage: "film"),
        .init(tab: .shows, title: "Shows", systemImage: "rectangle.on.rectangle"),
        // .search takes the system's search role, which parks it last and gives
        // it the platform's own search affordance.
        .init(tab: .search, title: "Search", systemImage: "magnifyingglass"),
    ]
}

struct ContentView: View {
    @State private var selectedTab: Tabs = .home
    @State private var searchString = ""

    /// Called when a tab is chosen. In the app this is the plugin's
    /// `trigger("tabChanged")`, which the web layer listens for and swaps route.
    var onTabChange: (Tabs) -> Void = { _ in }

    var body: some View {
        TabView(selection: $selectedTab) {
            Tab("Home", systemImage: "house", value: .home) {
                WebContent(route: "index.html")
            }
            Tab("Movies", systemImage: "film", value: .films) {
                WebContent(route: "index.html#films")
            }
            Tab("Shows", systemImage: "rectangle.on.rectangle", value: .shows) {
                WebContent(route: "index.html#shows")
            }
            Tab(value: .search, role: .search) {
                NavigationStack {
                    WebContent(route: "index.html#search")
                        .navigationTitle("Search")
                        .searchable(text: $searchString)
                }
            }
        }
        .onChange(of: selectedTab) { _, tab in onTabChange(tab) }
        .onChange(of: searchString) { _, q in
            // Native search bar, web results: the query goes to the same
            // /api/search the web UI uses.
            NotificationCenter.default.post(name: .northrouSearch, object: q)
        }
    }
}

/// Stands in for the WebView the shell actually hosts. The real app has one
/// WKWebView under the tab bar, not one per tab: the tabs swap its route.
struct WebContent: View {
    let route: String

    var body: some View {
        ZStack {
            Color(red: 0.02, green: 0.02, blue: 0.02).ignoresSafeArea()
            Text(route)
                .font(.system(.footnote, design: .monospaced))
                .foregroundStyle(.secondary)
        }
    }
}

extension Notification.Name {
    static let northrouSearch = Notification.Name("northrou.search")
}

#Preview {
    ContentView()
}
