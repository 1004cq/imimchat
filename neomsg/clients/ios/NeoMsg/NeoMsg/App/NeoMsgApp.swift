import SwiftUI

final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        PushNotificationHandler.shared.registerForPushNotifications()
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        PushNotificationHandler.shared.updateDeviceToken(deviceToken)
    }

    func application(
        _ application: UIApplication,
        didReceiveRemoteNotification userInfo: [AnyHashable: Any],
        fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
    ) {
        PushNotificationHandler.shared.handleRemoteNotification(
            userInfo: userInfo,
            fetchCompletionHandler: completionHandler
        )
    }
}

@main
struct NeoMsgApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
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
