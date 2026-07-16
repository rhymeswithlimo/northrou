// Watch-info
//
// Design reference for the detail sheet's native chrome. The content is web --
// the same detail modal every platform shows -- and the only native part is the
// dismiss button, because a web close button inside a presented sheet is wrong:
// it doesn't follow the platform's placement, and it leaves the sheet's own
// swipe-to-dismiss doing something the page knows nothing about.
//
// So on iOS the web close button hides (js/components/native-chrome.js) and this
// one drives it instead: dismissing here tells the web layer to close its modal,
// keeping both halves in step whether the user taps the button or swipes down.

import SwiftUI

struct WatchView: View {
    @Environment(\.dismiss) private var dismiss

    /// Tells the web layer its modal is going away, so the page can restore the
    /// tab bar and the scroll position it had underneath. Called for both the
    /// button and the interactive swipe.
    var onDismiss: () -> Void = {}

    var body: some View {
        NavigationStack {
            WebContent(route: "detail")
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            close()
                        } label: {
                            Image(systemName: "xmark")
                        }
                        // The web button is hidden on iOS, so this is the only
                        // affordance: it needs a name for VoiceOver.
                        .accessibilityLabel("Close")
                    }
                }
        }
        // A swipe-down dismiss never touches the button, so hook the sheet's own
        // lifecycle too or the web modal would be left open underneath.
        .onDisappear(perform: onDismiss)
    }

    private func close() {
        onDismiss()
        dismiss()
    }
}

#Preview {
    WatchView()
}
