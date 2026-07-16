const COMMANDS: &[&str] = &["set_tabs", "set_tab", "show_chrome", "play", "stop"];

fn main() {
    tauri_plugin::Builder::new(COMMANDS)
        .android_path("android")
        .ios_path("ios")
        .build();
}
