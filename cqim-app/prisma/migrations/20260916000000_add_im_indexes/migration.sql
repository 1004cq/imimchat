-- IM query indexes. Existing indexes and unique constraints already cover:
--   PrivateMessage(chatId, createdAt), GroupMessage(groupId, seq),
--   GroupMember(groupId, userId), ChatHidden(chatId, userId).

-- Private chat list: either participant, ordered by most recent activity.
CREATE INDEX "Chat_participantA_lastMessageAt_idx" ON "Chat"("participantA", "lastMessageAt");
CREATE INDEX "Chat_participantB_lastMessageAt_idx" ON "Chat"("participantB", "lastMessageAt");

-- Hidden-chat lookup and private unread counts.
CREATE INDEX "ChatHidden_userId_chatId_idx" ON "ChatHidden"("userId", "chatId");
CREATE INDEX "PrivateMessage_chatId_status_isRevoked_idx" ON "PrivateMessage"("chatId", "status", "isRevoked");

-- User group list/unread reads and paginated group member list.
CREATE INDEX "GroupMember_userId_updatedAt_idx" ON "GroupMember"("userId", "updatedAt");
CREATE INDEX "GroupMember_groupId_role_joinTime_idx" ON "GroupMember"("groupId", "role", "joinTime");

-- Friend-request inbox/outbox lists.
CREATE INDEX "FriendRequest_fromId_status_createdAt_idx" ON "FriendRequest"("fromId", "status", "createdAt");
CREATE INDEX "FriendRequest_toId_status_createdAt_idx" ON "FriendRequest"("toId", "status", "createdAt");

-- APNs alert-token lookup by recipient and delivery kind.
CREATE INDEX "PushDeviceToken_userId_kind_idx" ON "PushDeviceToken"("userId", "kind");
