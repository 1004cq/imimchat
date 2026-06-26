import SwiftUI

struct LoginView: View {
    @EnvironmentObject var coordinator: AppCoordinator
    @State private var phone = ""
    @State private var password = ""
    @State private var isLoading = false
    @State private var errorMessage: String?

    var body: some View {
        VStack(spacing: 24) {
            Spacer()
            Image(systemName: "message.circle.fill")
                .font(.system(size: 72))
                .foregroundStyle(.blue)
            Text("NeoMsg")
                .font(.largeTitle.bold())
            Text("安全 · 快速 · 私密")
                .foregroundStyle(.secondary)

            VStack(spacing: 12) {
                TextField("手机号", text: $phone)
                    .textFieldStyle(.roundedBorder)
                    .keyboardType(.phonePad)
                SecureField("密码", text: $password)
                    .textFieldStyle(.roundedBorder)
            }
            .padding(.horizontal, 32)

            if let error = errorMessage {
                Text(error).foregroundStyle(.red).font(.caption)
            }

            Button {
                Task { await doLogin() }
            } label: {
                if isLoading {
                    ProgressView().tint(.white)
                } else {
                    Text("登录").fontWeight(.semibold)
                }
            }
            .frame(maxWidth: .infinity)
            .padding()
            .background(.blue)
            .foregroundStyle(.white)
            .clipShape(RoundedRectangle(cornerRadius: 12))
            .padding(.horizontal, 32)
            .disabled(isLoading)

            Spacer()
        }
    }

    private func doLogin() async {
        isLoading = true
        errorMessage = nil
        do {
            try await coordinator.login(phone: phone, password: password)
        } catch {
            errorMessage = "登录失败，请检查账号密码"
        }
        isLoading = false
    }
}

struct ChatsListView: View {
    @EnvironmentObject var coordinator: AppCoordinator

    var body: some View {
        NavigationStack {
            List(coordinator.messageStore.dialogs) { dialog in
                NavigationLink {
                    ChatDetailView(dialog: dialog)
                } label: {
                    DialogRow(dialog: dialog)
                }
            }
            .navigationTitle("消息")
            .overlay {
                if coordinator.messageStore.dialogs.isEmpty {
                    ContentUnavailableView("暂无会话", systemImage: "message")
                }
            }
        }
    }
}

struct DialogRow: View {
    let dialog: Dialog

    var body: some View {
        HStack(spacing: 12) {
            Circle()
                .fill(.blue.opacity(0.2))
                .frame(width: 48, height: 48)
                .overlay {
                    Text(String(dialog.title.prefix(1)))
                        .font(.title3.bold())
                        .foregroundStyle(.blue)
                }
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(dialog.title).font(.body.weight(.medium))
                    if dialog.isSecret {
                        Image(systemName: "lock.fill").font(.caption2).foregroundStyle(.green)
                    }
                    Spacer()
                    if let time = dialog.lastMessageTime {
                        Text(time, style: .time).font(.caption2).foregroundStyle(.secondary)
                    }
                }
                HStack {
                    Text(dialog.lastMessagePreview ?? "")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                    Spacer()
                    if dialog.unreadCount > 0 {
                        Text("\(dialog.unreadCount)")
                            .font(.caption2.bold())
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(.blue)
                            .foregroundStyle(.white)
                            .clipShape(Capsule())
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }
}

struct ChatDetailView: View {
    let dialog: Dialog
    @EnvironmentObject var coordinator: AppCoordinator
    @State private var inputText = ""

    var messages: [Message] {
        coordinator.messageStore.messagesFor(dialogID: dialog.id)
    }

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                LazyVStack(spacing: 8) {
                    ForEach(messages) { msg in
                        MessageBubble(message: msg)
                    }
                }
                .padding()
            }
            HStack(spacing: 8) {
                TextField("输入消息...", text: $inputText, axis: .vertical)
                    .textFieldStyle(.roundedBorder)
                    .lineLimit(1...5)
                Button {
                    sendMessage()
                } label: {
                    Image(systemName: "paperplane.fill")
                        .foregroundStyle(inputText.isEmpty ? .gray : .blue)
                }
                .disabled(inputText.isEmpty)
            }
            .padding()
            .background(.bar)
        }
        .navigationTitle(dialog.title)
        .navigationBarTitleDisplayMode(.inline)
    }

    private func sendMessage() {
        let text = inputText
        inputText = ""
        let msg = Message(
            id: Int64(Date().timeIntervalSince1970 * 1000),
            dialogID: dialog.id,
            senderID: coordinator.currentUser?.id ?? 0,
            type: .text,
            content: text,
            status: .sending,
            clientMsgID: UUID().uuidString,
            createdAt: Date(),
            isOutgoing: true
        )
        coordinator.messageStore.appendMessage(msg)
        // TODO: 通过 ConnectionManager 发送 SendMessageRequest protobuf
    }
}

struct MessageBubble: View {
    let message: Message

    var body: some View {
        HStack {
            if message.isOutgoing { Spacer() }
            VStack(alignment: message.isOutgoing ? .trailing : .leading, spacing: 2) {
                Text(message.content)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .background(message.isOutgoing ? Color.blue : Color(.systemGray5))
                    .foregroundStyle(message.isOutgoing ? .white : .primary)
                    .clipShape(RoundedRectangle(cornerRadius: 16))
                HStack(spacing: 4) {
                    Text(message.createdAt, style: .time)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                    if message.isOutgoing {
                        statusIcon
                    }
                }
            }
            if !message.isOutgoing { Spacer() }
        }
    }

    @ViewBuilder
    private var statusIcon: some View {
        switch message.status {
        case .sending: Image(systemName: "clock").font(.caption2).foregroundStyle(.secondary)
        case .sent: Image(systemName: "checkmark").font(.caption2).foregroundStyle(.secondary)
        case .delivered: Image(systemName: "checkmark.circle").font(.caption2).foregroundStyle(.secondary)
        case .read: Image(systemName: "checkmark.circle.fill").font(.caption2).foregroundStyle(.blue)
        case .failed: Image(systemName: "exclamationmark.circle").font(.caption2).foregroundStyle(.red)
        }
    }
}

struct ContactsView: View {
    var body: some View {
        NavigationStack {
            ContentUnavailableView("通讯录", systemImage: "person.2")
                .navigationTitle("通讯录")
        }
    }
}

struct SettingsView: View {
    @EnvironmentObject var coordinator: AppCoordinator

    var body: some View {
        NavigationStack {
            List {
                Section("账号") {
                    if let user = coordinator.currentUser {
                        LabeledContent("用户", value: user.nickname)
                    }
                    LabeledContent("连接", value: "\(coordinator.connectionState)")
                }
                Section {
                    Button("退出登录", role: .destructive) {
                        coordinator.logout()
                    }
                }
            }
            .navigationTitle("设置")
        }
    }
}
