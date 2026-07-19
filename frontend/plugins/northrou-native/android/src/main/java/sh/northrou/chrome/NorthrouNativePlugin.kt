// Northrou's native Android chrome.
//
// The mirror of the iOS plugin, and deliberately not shared with it: per
// docs/frontend.md, native chrome is written twice. That is the price of a real
// UITabBar on iOS and a real BottomNavigationView here, instead of a web
// imitation that is subtly wrong on both.
//
// Kept in the plugin's own package, not in the generated gen/android project,
// which is regenerated and would lose it.

package sh.northrou.chrome

import android.app.Activity
import android.view.Gravity
import android.view.ViewGroup
import android.webkit.WebView
import android.widget.FrameLayout
import app.tauri.annotation.Command
import app.tauri.annotation.InvokeArg
import app.tauri.annotation.TauriPlugin
import app.tauri.plugin.Invoke
import app.tauri.plugin.JSObject
import app.tauri.plugin.Plugin
import com.google.android.material.bottomnavigation.BottomNavigationView

@InvokeArg
class TabItem {
    lateinit var key: String
    lateinit var title: String

    // The iOS side names SF Symbols here. Android resolves its own drawable by
    // tab key rather than trying to map Apple's symbol names onto material
    // icons, which never lines up.
    var systemImage: String = ""
}

@InvokeArg
class SetTabsArgs {
    var tabs: List<TabItem> = emptyList()
    var selected: String? = null
}

@InvokeArg
class SetTabArgs {
    lateinit var key: String
}

@InvokeArg
class ShowChromeArgs {
    var visible: Boolean = true
}

@TauriPlugin
class NorthrouNativePlugin(private val activity: Activity) : Plugin(activity) {
    private var nav: BottomNavigationView? = null
    private var keys: List<String> = emptyList()
    private var webView: WebView? = null

    override fun load(webView: WebView) {
        this.webView = webView
    }

    private fun iconFor(key: String): Int = when (key) {
        "home" -> android.R.drawable.ic_menu_view
        "films" -> android.R.drawable.ic_menu_gallery
        "shows" -> android.R.drawable.ic_menu_sort_by_size
        "search" -> android.R.drawable.ic_menu_search
        else -> android.R.drawable.ic_menu_help
    }

    @Command
    fun set_tabs(invoke: Invoke) {
        val args = invoke.parseArgs(SetTabsArgs::class.java)

        activity.runOnUiThread {
            val root = activity.window.decorView.findViewById<ViewGroup>(android.R.id.content)
            nav?.let { root.removeView(it) }
            keys = args.tabs.map { it.key }

            val bar = BottomNavigationView(activity)
            args.tabs.forEachIndexed { i, t ->
                bar.menu.add(0, i, i, t.title).setIcon(iconFor(t.key))
            }
            bar.setOnItemSelectedListener { item ->
                // The native bar owns the selection; the web owns the content.
                trigger("tabChanged", JSObject().put("key", keys[item.itemId]))
                true
            }
            args.selected?.let { sel ->
                keys.indexOf(sel).takeIf { it >= 0 }?.let { bar.selectedItemId = it }
            }

            val lp = FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply { gravity = Gravity.BOTTOM }

            root.addView(bar, lp)
            nav = bar

            bar.post { applyInset(bar.height) }
            invoke.resolve()
        }
    }

    @Command
    fun set_tab(invoke: Invoke) {
        val args = invoke.parseArgs(SetTabArgs::class.java)
        activity.runOnUiThread {
            keys.indexOf(args.key).takeIf { it >= 0 }?.let { nav?.selectedItemId = it }
            invoke.resolve()
        }
    }

    @Command
    fun show_chrome(invoke: Invoke) {
        val args = invoke.parseArgs(ShowChromeArgs::class.java)
        activity.runOnUiThread {
            val bar = nav
            if (bar == null) {
                invoke.resolve()
                return@runOnUiThread
            }
            bar.animate().alpha(if (args.visible) 1f else 0f).setDuration(200).withEndAction {
                bar.visibility = if (args.visible) android.view.View.VISIBLE else android.view.View.GONE
            }
            applyInset(if (args.visible) bar.height else 0)
            invoke.resolve()
        }
    }

    /// Tell the page how much room the bar takes, as a CSS variable its layout
    /// can add to its own padding.
    private fun applyInset(heightPx: Int) {
        val density = activity.resources.displayMetrics.density
        val cssPx = (heightPx / density).toInt()
        webView?.evaluateJavascript(
            "document.documentElement.style.setProperty('--native-tabbar-height', '${cssPx}px')",
            null,
        )
    }
}
