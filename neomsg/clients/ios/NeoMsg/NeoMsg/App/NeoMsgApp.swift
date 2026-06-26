import SwiftUI

@main
struct NeoMsgApp: App {
    @StateObject private var coordinator = AppCoordinator()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(coordinator)
        }
    }
}

struct RootView: View {
    @EnvironmentObject var coordinator: AppCoordinator

    var body: some View {
        Group {
            if coordinator.isAuthenticated {
                MainTabView()
            } else {
                LoginView()
            }
        }
        .task {
            await coordinator.bootstrap()
        }
    }
}

struct MainTabView: View {
    var body: some View {
        TabView {
            ChatsListView()
                .tabItem { Label("消息", systemImage: "message.fill") }
            ContactsView()
                .tabItem { Label("通讯录", systemImage: "person.2.fill") }
            SettingsView()
                .tabItem { Label("设置", systemImage: "gearshape.fill") }
        }
    }
}
