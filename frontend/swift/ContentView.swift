// Main page for northrou.

import SwiftUI

enum Tabs {
    case home, films, shows, search
}

struct ContentView: View {
    @State private var selectedTab: Tabs = .home
    @State private var searchString = ""

    var body: some View {
        TabView(selection: $selectedTab) {
            Tab("Home", systemImage: "house", value: .home) {
                Color.green.ignoresSafeArea() // placeholder
            }
            Tab("Movies", systemImage: "film", value: .films) {
                Color.orange.ignoresSafeArea() // placholder
            }
            Tab("Shows", systemImage: "rectangle.on.rectangle", value: .shows) {
                Color.purple.ignoresSafeArea() // placeholder
            }
            Tab(value: .search, role: .search) {
                NavigationStack {
                    List {}
                        .navigationTitle("Search")
                        .searchable(text: $searchString)
                }
            }
        }
    }
}

#Preview {
    ContentView()
}
