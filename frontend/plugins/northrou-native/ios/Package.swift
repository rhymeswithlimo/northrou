// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "tauri-plugin-northrou-native",
    platforms: [.iOS(.v13)],
    products: [
        .library(name: "tauri-plugin-northrou-native", type: .static, targets: ["NorthrouNative"]),
    ],
    dependencies: [
        .package(name: "Tauri", path: "../.tauri/tauri-api"),
    ],
    targets: [
        .target(
            name: "NorthrouNative",
            dependencies: [.byName(name: "Tauri")],
            path: "Sources/NorthrouNative"
        ),
    ]
)
