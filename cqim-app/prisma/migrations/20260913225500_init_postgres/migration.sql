-- Initial PostgreSQL schema matching cqim-app/prisma/schema.prisma
-- Generated with: prisma migrate diff --from-empty --to-schema-datamodel prisma/schema.prisma --script
-- Apply on an empty Tencent Cloud PG: npx prisma migrate deploy
-- Never point DATABASE_URL at mongodb:// or file:*.db

-- CreateTable
CREATE TABLE "User" (
    "id" TEXT NOT NULL,
    "dialogId" TEXT,
    "username" TEXT NOT NULL,
    "email" TEXT,
    "phone" TEXT,
    "password" TEXT NOT NULL,
    "nickname" TEXT,
    "avatar" TEXT,
    "backgroundUrl" TEXT,
    "bio" TEXT,
    "gender" TEXT,
    "region" TEXT,
    "birthday" TEXT,
    "isBot" BOOLEAN NOT NULL DEFAULT false,
    "isBanned" BOOLEAN NOT NULL DEFAULT false,
    "banReason" TEXT,
    "role" TEXT NOT NULL DEFAULT 'user',
    "phoneVerified" BOOLEAN NOT NULL DEFAULT false,
    "emailVerified" BOOLEAN NOT NULL DEFAULT false,
    "lastLoginAt" TIMESTAMP(3),
    "lastLoginIp" TEXT,
    "fcmToken" TEXT,
    "voipToken" TEXT,
    "webPushSubscription" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "User_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "UserSession" (
    "id" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "token" TEXT NOT NULL,
    "device" TEXT,
    "ip" TEXT,
    "location" TEXT,
    "expiresAt" TIMESTAMP(3) NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "UserSession_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "VerifyCode" (
    "id" TEXT NOT NULL,
    "target" TEXT NOT NULL,
    "code" TEXT NOT NULL,
    "type" TEXT NOT NULL,
    "channel" TEXT NOT NULL,
    "used" BOOLEAN NOT NULL DEFAULT false,
    "attempts" INTEGER NOT NULL DEFAULT 0,
    "expiresAt" TIMESTAMP(3) NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "VerifyCode_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Moment" (
    "id" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "content" TEXT NOT NULL,
    "visibility" TEXT NOT NULL DEFAULT 'public',
    "location" TEXT,
    "topics" TEXT,
    "isPinned" BOOLEAN NOT NULL DEFAULT false,
    "pinnedAt" TIMESTAMP(3),
    "viewCount" INTEGER NOT NULL DEFAULT 0,
    "sortOrder" INTEGER NOT NULL DEFAULT 0,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "Moment_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "MomentMedia" (
    "id" TEXT NOT NULL,
    "momentId" TEXT NOT NULL,
    "type" TEXT NOT NULL,
    "url" TEXT NOT NULL,
    "width" INTEGER,
    "height" INTEGER,
    "duration" INTEGER,
    "sortOrder" INTEGER NOT NULL DEFAULT 0,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "MomentMedia_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "MomentComment" (
    "id" TEXT NOT NULL,
    "momentId" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "content" TEXT NOT NULL,
    "replyToId" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "MomentComment_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "MomentLike" (
    "id" TEXT NOT NULL,
    "momentId" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "MomentLike_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "MediaFile" (
    "id" TEXT NOT NULL,
    "userId" TEXT,
    "type" TEXT NOT NULL,
    "url" TEXT NOT NULL,
    "filename" TEXT,
    "size" INTEGER,
    "width" INTEGER,
    "height" INTEGER,
    "cosKey" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "MediaFile_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "AdminAccount" (
    "id" TEXT NOT NULL,
    "username" TEXT NOT NULL,
    "password" TEXT NOT NULL,
    "role" TEXT NOT NULL DEFAULT 'admin',
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "AdminAccount_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "AdminSession" (
    "id" TEXT NOT NULL,
    "adminId" TEXT NOT NULL,
    "token" TEXT NOT NULL,
    "expiresAt" TIMESTAMP(3) NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "AdminSession_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "AdminLog" (
    "id" TEXT NOT NULL,
    "adminId" TEXT,
    "adminName" TEXT,
    "action" TEXT NOT NULL,
    "target" TEXT,
    "detail" TEXT,
    "ip" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "AdminLog_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "LoginLog" (
    "id" TEXT NOT NULL,
    "userId" TEXT,
    "username" TEXT,
    "email" TEXT,
    "phone" TEXT,
    "ip" TEXT,
    "userAgent" TEXT,
    "country" TEXT,
    "city" TEXT,
    "success" BOOLEAN NOT NULL DEFAULT true,
    "failReason" TEXT,
    "loginType" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "LoginLog_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "IpBlacklist" (
    "id" TEXT NOT NULL,
    "ip" TEXT NOT NULL,
    "reason" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "IpBlacklist_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "IllegalRequest" (
    "id" TEXT NOT NULL,
    "ip" TEXT,
    "path" TEXT,
    "method" TEXT,
    "userAgent" TEXT,
    "reason" TEXT,
    "statusCode" INTEGER,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "IllegalRequest_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SystemConfig" (
    "key" TEXT NOT NULL,
    "value" TEXT NOT NULL,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "SystemConfig_pkey" PRIMARY KEY ("key")
);

-- CreateTable
CREATE TABLE "Announcement" (
    "id" TEXT NOT NULL,
    "title" TEXT NOT NULL,
    "content" TEXT NOT NULL,
    "type" TEXT NOT NULL DEFAULT 'info',
    "isActive" BOOLEAN NOT NULL DEFAULT true,
    "createdBy" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "Announcement_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SensitiveWord" (
    "id" TEXT NOT NULL,
    "word" TEXT NOT NULL,
    "category" TEXT,
    "isActive" BOOLEAN NOT NULL DEFAULT true,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "SensitiveWord_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Report" (
    "id" TEXT NOT NULL,
    "reporterId" TEXT,
    "targetType" TEXT NOT NULL,
    "targetId" TEXT NOT NULL,
    "reason" TEXT NOT NULL,
    "status" TEXT NOT NULL DEFAULT 'pending',
    "resolvedBy" TEXT,
    "resolvedAt" TIMESTAMP(3),
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "Report_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "CustomPage" (
    "id" TEXT NOT NULL,
    "title" TEXT NOT NULL,
    "slug" TEXT NOT NULL,
    "content" TEXT NOT NULL,
    "published" BOOLEAN NOT NULL DEFAULT false,
    "sortOrder" INTEGER NOT NULL DEFAULT 0,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "CustomPage_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "FriendRequest" (
    "id" TEXT NOT NULL,
    "fromId" TEXT NOT NULL,
    "toId" TEXT NOT NULL,
    "message" TEXT DEFAULT '',
    "status" TEXT NOT NULL DEFAULT 'pending',
    "searchMethod" TEXT NOT NULL DEFAULT 'id',
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "FriendRequest_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Friendship" (
    "id" TEXT NOT NULL,
    "userA" TEXT NOT NULL,
    "userB" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "Friendship_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Chat" (
    "id" TEXT NOT NULL,
    "participantA" TEXT NOT NULL,
    "participantB" TEXT NOT NULL,
    "lastMessage" TEXT,
    "lastMessageAt" TIMESTAMP(3),
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "Chat_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "ChatHidden" (
    "id" TEXT NOT NULL,
    "chatId" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "hiddenAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "ChatHidden_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "PrivateMessage" (
    "id" TEXT NOT NULL,
    "chatId" TEXT NOT NULL,
    "senderId" TEXT NOT NULL,
    "msgType" TEXT NOT NULL DEFAULT 'text',
    "content" TEXT NOT NULL,
    "replyToId" TEXT,
    "isRevoked" BOOLEAN NOT NULL DEFAULT false,
    "status" TEXT NOT NULL DEFAULT 'sent',
    "extra" TEXT,
    "hmac" TEXT,
    "burnAfterRead" INTEGER,
    "burnReadAt" TIMESTAMP(3),
    "burnExpireAt" TIMESTAMP(3),
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "PrivateMessage_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Group" (
    "id" TEXT NOT NULL,
    "dialogId" TEXT,
    "name" TEXT NOT NULL,
    "username" TEXT,
    "avatar" TEXT,
    "ownerId" TEXT NOT NULL,
    "type" TEXT NOT NULL DEFAULT 'normal',
    "isPublic" BOOLEAN NOT NULL DEFAULT false,
    "maxMembers" INTEGER NOT NULL DEFAULT 500,
    "memberCount" INTEGER NOT NULL DEFAULT 0,
    "lastMsgSeq" BIGINT NOT NULL DEFAULT 0,
    "lastMsgTime" TIMESTAMP(3),
    "announcement" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "Group_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "GroupMember" (
    "id" TEXT NOT NULL,
    "groupId" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "role" TEXT NOT NULL DEFAULT 'member',
    "nickname" TEXT,
    "lastAckSeq" BIGINT NOT NULL DEFAULT 0,
    "joinTime" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "muteUntil" TIMESTAMP(3),
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "GroupMember_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "GroupMessage" (
    "id" TEXT NOT NULL,
    "groupId" TEXT NOT NULL,
    "seq" BIGINT NOT NULL,
    "senderId" TEXT NOT NULL,
    "senderName" TEXT,
    "msgType" TEXT NOT NULL DEFAULT 'text',
    "content" TEXT NOT NULL,
    "replyToId" TEXT,
    "isRevoked" BOOLEAN NOT NULL DEFAULT false,
    "extra" TEXT,
    "mlsEncrypted" BOOLEAN NOT NULL DEFAULT false,
    "mlsEpoch" INTEGER DEFAULT 0,
    "burnAfterRead" INTEGER,
    "burnExpireAt" TIMESTAMP(3),
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "GroupMessage_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "GroupInvite" (
    "id" TEXT NOT NULL,
    "groupId" TEXT NOT NULL,
    "inviterId" TEXT NOT NULL,
    "inviteeId" TEXT NOT NULL,
    "message" TEXT DEFAULT '',
    "status" TEXT NOT NULL DEFAULT 'pending',
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "GroupInvite_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "InviteLink" (
    "id" TEXT NOT NULL,
    "hash" TEXT NOT NULL,
    "groupId" TEXT NOT NULL,
    "creatorId" TEXT NOT NULL,
    "name" TEXT,
    "expireAt" TIMESTAMP(3),
    "maxUses" INTEGER NOT NULL DEFAULT 0,
    "usedCount" INTEGER NOT NULL DEFAULT 0,
    "isRevoked" BOOLEAN NOT NULL DEFAULT false,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "InviteLink_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "User_dialogId_key" ON "User"("dialogId");

-- CreateIndex
CREATE UNIQUE INDEX "User_username_key" ON "User"("username");

-- CreateIndex
CREATE UNIQUE INDEX "User_email_key" ON "User"("email");

-- CreateIndex
CREATE UNIQUE INDEX "User_phone_key" ON "User"("phone");

-- CreateIndex
CREATE INDEX "User_dialogId_idx" ON "User"("dialogId");

-- CreateIndex
CREATE INDEX "User_username_idx" ON "User"("username");

-- CreateIndex
CREATE INDEX "User_email_idx" ON "User"("email");

-- CreateIndex
CREATE INDEX "User_phone_idx" ON "User"("phone");

-- CreateIndex
CREATE UNIQUE INDEX "UserSession_token_key" ON "UserSession"("token");

-- CreateIndex
CREATE INDEX "UserSession_token_idx" ON "UserSession"("token");

-- CreateIndex
CREATE INDEX "UserSession_userId_idx" ON "UserSession"("userId");

-- CreateIndex
CREATE INDEX "UserSession_token_expiresAt_idx" ON "UserSession"("token", "expiresAt");

-- CreateIndex
CREATE INDEX "VerifyCode_target_type_channel_idx" ON "VerifyCode"("target", "type", "channel");

-- CreateIndex
CREATE INDEX "VerifyCode_expiresAt_idx" ON "VerifyCode"("expiresAt");

-- CreateIndex
CREATE INDEX "VerifyCode_target_type_channel_used_expiresAt_idx" ON "VerifyCode"("target", "type", "channel", "used", "expiresAt");

-- CreateIndex
CREATE INDEX "Moment_userId_idx" ON "Moment"("userId");

-- CreateIndex
CREATE INDEX "Moment_createdAt_idx" ON "Moment"("createdAt");

-- CreateIndex
CREATE INDEX "Moment_isPinned_idx" ON "Moment"("isPinned");

-- CreateIndex
CREATE INDEX "Moment_sortOrder_idx" ON "Moment"("sortOrder");

-- CreateIndex
CREATE INDEX "Moment_userId_createdAt_idx" ON "Moment"("userId", "createdAt");

-- CreateIndex
CREATE INDEX "Moment_visibility_createdAt_idx" ON "Moment"("visibility", "createdAt");

-- CreateIndex
CREATE INDEX "Moment_userId_isPinned_pinnedAt_idx" ON "Moment"("userId", "isPinned", "pinnedAt");

-- CreateIndex
CREATE INDEX "MomentMedia_momentId_idx" ON "MomentMedia"("momentId");

-- CreateIndex
CREATE INDEX "MomentComment_momentId_idx" ON "MomentComment"("momentId");

-- CreateIndex
CREATE INDEX "MomentComment_userId_idx" ON "MomentComment"("userId");

-- CreateIndex
CREATE INDEX "MomentLike_momentId_idx" ON "MomentLike"("momentId");

-- CreateIndex
CREATE INDEX "MomentLike_userId_idx" ON "MomentLike"("userId");

-- CreateIndex
CREATE UNIQUE INDEX "MomentLike_momentId_userId_key" ON "MomentLike"("momentId", "userId");

-- CreateIndex
CREATE INDEX "MediaFile_userId_idx" ON "MediaFile"("userId");

-- CreateIndex
CREATE INDEX "MediaFile_type_idx" ON "MediaFile"("type");

-- CreateIndex
CREATE INDEX "MediaFile_createdAt_idx" ON "MediaFile"("createdAt");

-- CreateIndex
CREATE UNIQUE INDEX "AdminAccount_username_key" ON "AdminAccount"("username");

-- CreateIndex
CREATE UNIQUE INDEX "AdminSession_token_key" ON "AdminSession"("token");

-- CreateIndex
CREATE INDEX "AdminSession_token_idx" ON "AdminSession"("token");

-- CreateIndex
CREATE INDEX "AdminLog_adminId_idx" ON "AdminLog"("adminId");

-- CreateIndex
CREATE INDEX "AdminLog_createdAt_idx" ON "AdminLog"("createdAt");

-- CreateIndex
CREATE INDEX "LoginLog_userId_idx" ON "LoginLog"("userId");

-- CreateIndex
CREATE INDEX "LoginLog_ip_idx" ON "LoginLog"("ip");

-- CreateIndex
CREATE INDEX "LoginLog_createdAt_idx" ON "LoginLog"("createdAt");

-- CreateIndex
CREATE UNIQUE INDEX "IpBlacklist_ip_key" ON "IpBlacklist"("ip");

-- CreateIndex
CREATE INDEX "IpBlacklist_ip_idx" ON "IpBlacklist"("ip");

-- CreateIndex
CREATE INDEX "IllegalRequest_ip_idx" ON "IllegalRequest"("ip");

-- CreateIndex
CREATE INDEX "IllegalRequest_createdAt_idx" ON "IllegalRequest"("createdAt");

-- CreateIndex
CREATE INDEX "Announcement_isActive_idx" ON "Announcement"("isActive");

-- CreateIndex
CREATE INDEX "Announcement_createdAt_idx" ON "Announcement"("createdAt");

-- CreateIndex
CREATE UNIQUE INDEX "SensitiveWord_word_key" ON "SensitiveWord"("word");

-- CreateIndex
CREATE INDEX "SensitiveWord_word_idx" ON "SensitiveWord"("word");

-- CreateIndex
CREATE INDEX "SensitiveWord_isActive_idx" ON "SensitiveWord"("isActive");

-- CreateIndex
CREATE INDEX "Report_status_idx" ON "Report"("status");

-- CreateIndex
CREATE INDEX "Report_targetType_targetId_idx" ON "Report"("targetType", "targetId");

-- CreateIndex
CREATE INDEX "Report_createdAt_idx" ON "Report"("createdAt");

-- CreateIndex
CREATE UNIQUE INDEX "CustomPage_slug_key" ON "CustomPage"("slug");

-- CreateIndex
CREATE INDEX "CustomPage_slug_idx" ON "CustomPage"("slug");

-- CreateIndex
CREATE INDEX "CustomPage_published_idx" ON "CustomPage"("published");

-- CreateIndex
CREATE INDEX "FriendRequest_fromId_idx" ON "FriendRequest"("fromId");

-- CreateIndex
CREATE INDEX "FriendRequest_toId_idx" ON "FriendRequest"("toId");

-- CreateIndex
CREATE INDEX "FriendRequest_status_idx" ON "FriendRequest"("status");

-- CreateIndex
CREATE UNIQUE INDEX "FriendRequest_fromId_toId_key" ON "FriendRequest"("fromId", "toId");

-- CreateIndex
CREATE INDEX "Friendship_userA_idx" ON "Friendship"("userA");

-- CreateIndex
CREATE INDEX "Friendship_userB_idx" ON "Friendship"("userB");

-- CreateIndex
CREATE UNIQUE INDEX "Friendship_userA_userB_key" ON "Friendship"("userA", "userB");

-- CreateIndex
CREATE INDEX "Chat_participantA_idx" ON "Chat"("participantA");

-- CreateIndex
CREATE INDEX "Chat_participantB_idx" ON "Chat"("participantB");

-- CreateIndex
CREATE INDEX "Chat_lastMessageAt_idx" ON "Chat"("lastMessageAt");

-- CreateIndex
CREATE UNIQUE INDEX "Chat_participantA_participantB_key" ON "Chat"("participantA", "participantB");

-- CreateIndex
CREATE INDEX "ChatHidden_userId_hiddenAt_idx" ON "ChatHidden"("userId", "hiddenAt");

-- CreateIndex
CREATE INDEX "ChatHidden_chatId_idx" ON "ChatHidden"("chatId");

-- CreateIndex
CREATE UNIQUE INDEX "ChatHidden_chatId_userId_key" ON "ChatHidden"("chatId", "userId");

-- CreateIndex
CREATE INDEX "PrivateMessage_chatId_createdAt_idx" ON "PrivateMessage"("chatId", "createdAt");

-- CreateIndex
CREATE INDEX "PrivateMessage_senderId_idx" ON "PrivateMessage"("senderId");

-- CreateIndex
CREATE INDEX "PrivateMessage_chatId_isRevoked_idx" ON "PrivateMessage"("chatId", "isRevoked");

-- CreateIndex
CREATE INDEX "PrivateMessage_burnExpireAt_idx" ON "PrivateMessage"("burnExpireAt");

-- CreateIndex
CREATE UNIQUE INDEX "Group_dialogId_key" ON "Group"("dialogId");

-- CreateIndex
CREATE UNIQUE INDEX "Group_username_key" ON "Group"("username");

-- CreateIndex
CREATE INDEX "Group_dialogId_idx" ON "Group"("dialogId");

-- CreateIndex
CREATE INDEX "Group_username_idx" ON "Group"("username");

-- CreateIndex
CREATE INDEX "Group_ownerId_idx" ON "Group"("ownerId");

-- CreateIndex
CREATE INDEX "Group_type_idx" ON "Group"("type");

-- CreateIndex
CREATE INDEX "Group_isPublic_idx" ON "Group"("isPublic");

-- CreateIndex
CREATE INDEX "Group_id_lastMsgSeq_idx" ON "Group"("id", "lastMsgSeq");

-- CreateIndex
CREATE INDEX "GroupMember_groupId_idx" ON "GroupMember"("groupId");

-- CreateIndex
CREATE INDEX "GroupMember_userId_idx" ON "GroupMember"("userId");

-- CreateIndex
CREATE INDEX "GroupMember_groupId_userId_joinTime_lastAckSeq_idx" ON "GroupMember"("groupId", "userId", "joinTime", "lastAckSeq");

-- CreateIndex
CREATE INDEX "GroupMember_userId_groupId_lastAckSeq_idx" ON "GroupMember"("userId", "groupId", "lastAckSeq");

-- CreateIndex
CREATE UNIQUE INDEX "GroupMember_groupId_userId_key" ON "GroupMember"("groupId", "userId");

-- CreateIndex
CREATE INDEX "GroupMessage_groupId_seq_createdAt_idx" ON "GroupMessage"("groupId", "seq", "createdAt");

-- CreateIndex
CREATE INDEX "GroupMessage_senderId_idx" ON "GroupMessage"("senderId");

-- CreateIndex
CREATE INDEX "GroupMessage_groupId_isRevoked_idx" ON "GroupMessage"("groupId", "isRevoked");

-- CreateIndex
CREATE INDEX "GroupMessage_burnExpireAt_idx" ON "GroupMessage"("burnExpireAt");

-- CreateIndex
CREATE INDEX "GroupMessage_groupId_mlsEncrypted_idx" ON "GroupMessage"("groupId", "mlsEncrypted");

-- CreateIndex
CREATE UNIQUE INDEX "GroupMessage_groupId_seq_key" ON "GroupMessage"("groupId", "seq");

-- CreateIndex
CREATE INDEX "GroupInvite_inviterId_idx" ON "GroupInvite"("inviterId");

-- CreateIndex
CREATE INDEX "GroupInvite_inviteeId_idx" ON "GroupInvite"("inviteeId");

-- CreateIndex
CREATE INDEX "GroupInvite_groupId_idx" ON "GroupInvite"("groupId");

-- CreateIndex
CREATE INDEX "GroupInvite_status_idx" ON "GroupInvite"("status");

-- CreateIndex
CREATE UNIQUE INDEX "GroupInvite_groupId_inviteeId_status_key" ON "GroupInvite"("groupId", "inviteeId", "status");

-- CreateIndex
CREATE UNIQUE INDEX "InviteLink_hash_key" ON "InviteLink"("hash");

-- CreateIndex
CREATE INDEX "InviteLink_hash_idx" ON "InviteLink"("hash");

-- CreateIndex
CREATE INDEX "InviteLink_groupId_idx" ON "InviteLink"("groupId");

-- CreateIndex
CREATE INDEX "InviteLink_creatorId_idx" ON "InviteLink"("creatorId");

-- CreateIndex
CREATE INDEX "InviteLink_expireAt_idx" ON "InviteLink"("expireAt");

-- AddForeignKey
ALTER TABLE "UserSession" ADD CONSTRAINT "UserSession_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Moment" ADD CONSTRAINT "Moment_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "MomentMedia" ADD CONSTRAINT "MomentMedia_momentId_fkey" FOREIGN KEY ("momentId") REFERENCES "Moment"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "MomentComment" ADD CONSTRAINT "MomentComment_momentId_fkey" FOREIGN KEY ("momentId") REFERENCES "Moment"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "MomentComment" ADD CONSTRAINT "MomentComment_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "MomentComment" ADD CONSTRAINT "MomentComment_replyToId_fkey" FOREIGN KEY ("replyToId") REFERENCES "MomentComment"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "MomentLike" ADD CONSTRAINT "MomentLike_momentId_fkey" FOREIGN KEY ("momentId") REFERENCES "Moment"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "MomentLike" ADD CONSTRAINT "MomentLike_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "MediaFile" ADD CONSTRAINT "MediaFile_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "AdminSession" ADD CONSTRAINT "AdminSession_adminId_fkey" FOREIGN KEY ("adminId") REFERENCES "AdminAccount"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "AdminLog" ADD CONSTRAINT "AdminLog_adminId_fkey" FOREIGN KEY ("adminId") REFERENCES "AdminAccount"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "LoginLog" ADD CONSTRAINT "LoginLog_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ChatHidden" ADD CONSTRAINT "ChatHidden_chatId_fkey" FOREIGN KEY ("chatId") REFERENCES "Chat"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "ChatHidden" ADD CONSTRAINT "ChatHidden_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "PrivateMessage" ADD CONSTRAINT "PrivateMessage_chatId_fkey" FOREIGN KEY ("chatId") REFERENCES "Chat"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "GroupMember" ADD CONSTRAINT "GroupMember_groupId_fkey" FOREIGN KEY ("groupId") REFERENCES "Group"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "GroupMessage" ADD CONSTRAINT "GroupMessage_groupId_fkey" FOREIGN KEY ("groupId") REFERENCES "Group"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "GroupInvite" ADD CONSTRAINT "GroupInvite_groupId_fkey" FOREIGN KEY ("groupId") REFERENCES "Group"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "InviteLink" ADD CONSTRAINT "InviteLink_groupId_fkey" FOREIGN KEY ("groupId") REFERENCES "Group"("id") ON DELETE CASCADE ON UPDATE CASCADE;

