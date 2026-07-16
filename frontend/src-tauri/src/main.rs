// Prevent a console window from opening alongside the app on Windows release
// builds. Debug builds keep it: that is where the logs go.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    northrou_lib::run()
}
