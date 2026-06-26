// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "NeoMsg",
    platforms: [.iOS(.v17)],
    products: [
        .library(name: "NeoMsgCore", targets: ["NeoMsgCore"]),
    ],
    dependencies: [
        // .package(url: "https://github.com/apple/swift-protobuf.git", from: "1.25.0"),
        // .package(url: "https://github.com/groue/GRDB.swift.git", from: "7.0.0"),
    ],
    targets: [
        .target(
            name: "NeoMsgCore",
            path: "NeoMsg/Core"
        ),
    ]
)
