package db

// Code generated from prisma/schema.prisma — DO NOT EDIT BY HAND.
// 由 scripts/gen_models.py 从 Prisma schema 自动生成，保证与 Node 端表结构一致。

import (
	"time"
)

// User 对应 PostgreSQL 表 "User"。
type User struct {
	Id                  string     `db:"id" json:"id"`
	DialogId            *string    `db:"dialogId" json:"dialogId"`
	Username            string     `db:"username" json:"username"`
	Email               *string    `db:"email" json:"email"`
	Phone               *string    `db:"phone" json:"phone"`
	Password            string     `db:"password" json:"password"`
	Nickname            *string    `db:"nickname" json:"nickname"`
	Avatar              *string    `db:"avatar" json:"avatar"`
	BackgroundUrl       *string    `db:"backgroundUrl" json:"backgroundUrl"`
	Bio                 *string    `db:"bio" json:"bio"`
	Gender              *string    `db:"gender" json:"gender"`
	Region              *string    `db:"region" json:"region"`
	Birthday            *string    `db:"birthday" json:"birthday"`
	IsBot               bool       `db:"isBot" json:"isBot"`
	IsBanned            bool       `db:"isBanned" json:"isBanned"`
	BanReason           *string    `db:"banReason" json:"banReason"`
	Role                string     `db:"role" json:"role"`
	PhoneVerified       bool       `db:"phoneVerified" json:"phoneVerified"`
	EmailVerified       bool       `db:"emailVerified" json:"emailVerified"`
	LastLoginAt         *time.Time `db:"lastLoginAt" json:"lastLoginAt"`
	LastLoginIp         *string    `db:"lastLoginIp" json:"lastLoginIp"`
	WebPushSubscription *string    `db:"webPushSubscription" json:"webPushSubscription"`
	CreatedAt           time.Time  `db:"createdAt" json:"createdAt"`
	UpdatedAt           time.Time  `db:"updatedAt" json:"updatedAt"`
}

// UserColumns 供 SQL 拼接使用。
var UserColumns = []string{"id", "dialogId", "username", "email", "phone", "password", "nickname", "avatar", "backgroundUrl", "bio", "gender", "region", "birthday", "isBot", "isBanned", "banReason", "role", "phoneVerified", "emailVerified", "lastLoginAt", "lastLoginIp", "webPushSubscription", "createdAt", "updatedAt"}

// PushDeviceToken 对应 PostgreSQL 表 "PushDeviceToken"。
type PushDeviceToken struct {
	Id          string    `db:"id" json:"id"`
	UserId      string    `db:"userId" json:"userId"`
	Token       string    `db:"token" json:"token"`
	Platform    string    `db:"platform" json:"platform"`
	Environment string    `db:"environment" json:"environment"`
	Kind        string    `db:"kind" json:"kind"`
	AppVersion  *string   `db:"appVersion" json:"appVersion"`
	CreatedAt   time.Time `db:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time `db:"updatedAt" json:"updatedAt"`
}

// PushDeviceTokenColumns 供 SQL 拼接使用。
var PushDeviceTokenColumns = []string{"id", "userId", "token", "platform", "environment", "kind", "appVersion", "createdAt", "updatedAt"}

// UserSession 对应 PostgreSQL 表 "UserSession"。
type UserSession struct {
	Id        string    `db:"id" json:"id"`
	UserId    string    `db:"userId" json:"userId"`
	Token     string    `db:"token" json:"token"`
	Device    *string   `db:"device" json:"device"`
	Ip        *string   `db:"ip" json:"ip"`
	Location  *string   `db:"location" json:"location"`
	ExpiresAt time.Time `db:"expiresAt" json:"expiresAt"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
}

// UserSessionColumns 供 SQL 拼接使用。
var UserSessionColumns = []string{"id", "userId", "token", "device", "ip", "location", "expiresAt", "createdAt"}

// VerifyCode 对应 PostgreSQL 表 "VerifyCode"。
type VerifyCode struct {
	Id        string    `db:"id" json:"id"`
	Target    string    `db:"target" json:"target"`
	Code      string    `db:"code" json:"code"`
	Type      string    `db:"type" json:"type"`
	Channel   string    `db:"channel" json:"channel"`
	Used      bool      `db:"used" json:"used"`
	Attempts  int32     `db:"attempts" json:"attempts"`
	ExpiresAt time.Time `db:"expiresAt" json:"expiresAt"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
}

// VerifyCodeColumns 供 SQL 拼接使用。
var VerifyCodeColumns = []string{"id", "target", "code", "type", "channel", "used", "attempts", "expiresAt", "createdAt"}

// Moment 对应 PostgreSQL 表 "Moment"。
type Moment struct {
	Id         string     `db:"id" json:"id"`
	UserId     string     `db:"userId" json:"userId"`
	Content    string     `db:"content" json:"content"`
	Visibility string     `db:"visibility" json:"visibility"`
	Location   *string    `db:"location" json:"location"`
	Topics     *string    `db:"topics" json:"topics"`
	IsPinned   bool       `db:"isPinned" json:"isPinned"`
	PinnedAt   *time.Time `db:"pinnedAt" json:"pinnedAt"`
	ViewCount  int32      `db:"viewCount" json:"viewCount"`
	SortOrder  int32      `db:"sortOrder" json:"sortOrder"`
	CreatedAt  time.Time  `db:"createdAt" json:"createdAt"`
	UpdatedAt  time.Time  `db:"updatedAt" json:"updatedAt"`
}

// MomentColumns 供 SQL 拼接使用。
var MomentColumns = []string{"id", "userId", "content", "visibility", "location", "topics", "isPinned", "pinnedAt", "viewCount", "sortOrder", "createdAt", "updatedAt"}

// MomentMedia 对应 PostgreSQL 表 "MomentMedia"。
type MomentMedia struct {
	Id        string    `db:"id" json:"id"`
	MomentId  string    `db:"momentId" json:"momentId"`
	Type      string    `db:"type" json:"type"`
	Url       string    `db:"url" json:"url"`
	Width     *int32    `db:"width" json:"width"`
	Height    *int32    `db:"height" json:"height"`
	Duration  *int32    `db:"duration" json:"duration"`
	SortOrder int32     `db:"sortOrder" json:"sortOrder"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
}

// MomentMediaColumns 供 SQL 拼接使用。
var MomentMediaColumns = []string{"id", "momentId", "type", "url", "width", "height", "duration", "sortOrder", "createdAt"}

// MomentComment 对应 PostgreSQL 表 "MomentComment"。
type MomentComment struct {
	Id        string    `db:"id" json:"id"`
	MomentId  string    `db:"momentId" json:"momentId"`
	UserId    string    `db:"userId" json:"userId"`
	Content   string    `db:"content" json:"content"`
	ReplyToId *string   `db:"replyToId" json:"replyToId"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `db:"updatedAt" json:"updatedAt"`
}

// MomentCommentColumns 供 SQL 拼接使用。
var MomentCommentColumns = []string{"id", "momentId", "userId", "content", "replyToId", "createdAt", "updatedAt"}

// MomentLike 对应 PostgreSQL 表 "MomentLike"。
type MomentLike struct {
	Id        string    `db:"id" json:"id"`
	MomentId  string    `db:"momentId" json:"momentId"`
	UserId    string    `db:"userId" json:"userId"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
}

// MomentLikeColumns 供 SQL 拼接使用。
var MomentLikeColumns = []string{"id", "momentId", "userId", "createdAt"}

// MediaFile 对应 PostgreSQL 表 "MediaFile"。
type MediaFile struct {
	Id            string    `db:"id" json:"id"`
	UserId        *string   `db:"userId" json:"userId"`
	Type          string    `db:"type" json:"type"`
	Kind          string    `db:"kind" json:"kind"`
	Url           string    `db:"url" json:"url"`
	Filename      *string   `db:"filename" json:"filename"`
	Mime          *string   `db:"mime" json:"mime"`
	Size          *int32    `db:"size" json:"size"`
	Width         *int32    `db:"width" json:"width"`
	Height        *int32    `db:"height" json:"height"`
	DurationMs    *int32    `db:"durationMs" json:"durationMs"`
	PosterMediaId *string   `db:"posterMediaId" json:"posterMediaId"`
	Sha256        *string   `db:"sha256" json:"sha256"`
	DiskPath      *string   `db:"diskPath" json:"diskPath"`
	PublicPath    *string   `db:"publicPath" json:"publicPath"`
	CosKey        *string   `db:"cosKey" json:"cosKey"`
	CreatedAt     time.Time `db:"createdAt" json:"createdAt"`
}

// MediaFileColumns 供 SQL 拼接使用。
var MediaFileColumns = []string{"id", "userId", "type", "kind", "url", "filename", "mime", "size", "width", "height", "durationMs", "posterMediaId", "sha256", "diskPath", "publicPath", "cosKey", "createdAt"}

// AdminAccount 对应 PostgreSQL 表 "AdminAccount"。
type AdminAccount struct {
	Id        string    `db:"id" json:"id"`
	Username  string    `db:"username" json:"username"`
	Password  string    `db:"password" json:"password"`
	Role      string    `db:"role" json:"role"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `db:"updatedAt" json:"updatedAt"`
}

// AdminAccountColumns 供 SQL 拼接使用。
var AdminAccountColumns = []string{"id", "username", "password", "role", "createdAt", "updatedAt"}

// AdminSession 对应 PostgreSQL 表 "AdminSession"。
type AdminSession struct {
	Id        string    `db:"id" json:"id"`
	AdminId   string    `db:"adminId" json:"adminId"`
	Token     string    `db:"token" json:"token"`
	ExpiresAt time.Time `db:"expiresAt" json:"expiresAt"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
}

// AdminSessionColumns 供 SQL 拼接使用。
var AdminSessionColumns = []string{"id", "adminId", "token", "expiresAt", "createdAt"}

// AdminLog 对应 PostgreSQL 表 "AdminLog"。
type AdminLog struct {
	Id        string    `db:"id" json:"id"`
	AdminId   *string   `db:"adminId" json:"adminId"`
	AdminName *string   `db:"adminName" json:"adminName"`
	Action    string    `db:"action" json:"action"`
	Target    *string   `db:"target" json:"target"`
	Detail    *string   `db:"detail" json:"detail"`
	Ip        *string   `db:"ip" json:"ip"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
}

// AdminLogColumns 供 SQL 拼接使用。
var AdminLogColumns = []string{"id", "adminId", "adminName", "action", "target", "detail", "ip", "createdAt"}

// LoginLog 对应 PostgreSQL 表 "LoginLog"。
type LoginLog struct {
	Id         string    `db:"id" json:"id"`
	UserId     *string   `db:"userId" json:"userId"`
	Username   *string   `db:"username" json:"username"`
	Email      *string   `db:"email" json:"email"`
	Phone      *string   `db:"phone" json:"phone"`
	Ip         *string   `db:"ip" json:"ip"`
	UserAgent  *string   `db:"userAgent" json:"userAgent"`
	Country    *string   `db:"country" json:"country"`
	City       *string   `db:"city" json:"city"`
	Success    bool      `db:"success" json:"success"`
	FailReason *string   `db:"failReason" json:"failReason"`
	LoginType  *string   `db:"loginType" json:"loginType"`
	CreatedAt  time.Time `db:"createdAt" json:"createdAt"`
}

// LoginLogColumns 供 SQL 拼接使用。
var LoginLogColumns = []string{"id", "userId", "username", "email", "phone", "ip", "userAgent", "country", "city", "success", "failReason", "loginType", "createdAt"}

// IpBlacklist 对应 PostgreSQL 表 "IpBlacklist"。
type IpBlacklist struct {
	Id        string    `db:"id" json:"id"`
	Ip        string    `db:"ip" json:"ip"`
	Reason    *string   `db:"reason" json:"reason"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `db:"updatedAt" json:"updatedAt"`
}

// IpBlacklistColumns 供 SQL 拼接使用。
var IpBlacklistColumns = []string{"id", "ip", "reason", "createdAt", "updatedAt"}

// IllegalRequest 对应 PostgreSQL 表 "IllegalRequest"。
type IllegalRequest struct {
	Id         string    `db:"id" json:"id"`
	Ip         *string   `db:"ip" json:"ip"`
	Path       *string   `db:"path" json:"path"`
	Method     *string   `db:"method" json:"method"`
	UserAgent  *string   `db:"userAgent" json:"userAgent"`
	Reason     *string   `db:"reason" json:"reason"`
	StatusCode *int32    `db:"statusCode" json:"statusCode"`
	CreatedAt  time.Time `db:"createdAt" json:"createdAt"`
}

// IllegalRequestColumns 供 SQL 拼接使用。
var IllegalRequestColumns = []string{"id", "ip", "path", "method", "userAgent", "reason", "statusCode", "createdAt"}

// SystemConfig 对应 PostgreSQL 表 "SystemConfig"。
type SystemConfig struct {
	Key       string    `db:"key" json:"key"`
	Value     string    `db:"value" json:"value"`
	UpdatedAt time.Time `db:"updatedAt" json:"updatedAt"`
}

// SystemConfigColumns 供 SQL 拼接使用。
var SystemConfigColumns = []string{"key", "value", "updatedAt"}

// Announcement 对应 PostgreSQL 表 "Announcement"。
type Announcement struct {
	Id        string    `db:"id" json:"id"`
	Title     string    `db:"title" json:"title"`
	Content   string    `db:"content" json:"content"`
	Type      string    `db:"type" json:"type"`
	IsActive  bool      `db:"isActive" json:"isActive"`
	CreatedBy *string   `db:"createdBy" json:"createdBy"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `db:"updatedAt" json:"updatedAt"`
}

// AnnouncementColumns 供 SQL 拼接使用。
var AnnouncementColumns = []string{"id", "title", "content", "type", "isActive", "createdBy", "createdAt", "updatedAt"}

// SensitiveWord 对应 PostgreSQL 表 "SensitiveWord"。
type SensitiveWord struct {
	Id        string    `db:"id" json:"id"`
	Word      string    `db:"word" json:"word"`
	Category  *string   `db:"category" json:"category"`
	IsActive  bool      `db:"isActive" json:"isActive"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
}

// SensitiveWordColumns 供 SQL 拼接使用。
var SensitiveWordColumns = []string{"id", "word", "category", "isActive", "createdAt"}

// Report 对应 PostgreSQL 表 "Report"。
type Report struct {
	Id         string     `db:"id" json:"id"`
	ReporterId *string    `db:"reporterId" json:"reporterId"`
	TargetType string     `db:"targetType" json:"targetType"`
	TargetId   string     `db:"targetId" json:"targetId"`
	Reason     string     `db:"reason" json:"reason"`
	Status     string     `db:"status" json:"status"`
	ResolvedBy *string    `db:"resolvedBy" json:"resolvedBy"`
	ResolvedAt *time.Time `db:"resolvedAt" json:"resolvedAt"`
	CreatedAt  time.Time  `db:"createdAt" json:"createdAt"`
}

// ReportColumns 供 SQL 拼接使用。
var ReportColumns = []string{"id", "reporterId", "targetType", "targetId", "reason", "status", "resolvedBy", "resolvedAt", "createdAt"}

// CustomPage 对应 PostgreSQL 表 "CustomPage"。
type CustomPage struct {
	Id        string    `db:"id" json:"id"`
	Title     string    `db:"title" json:"title"`
	Slug      string    `db:"slug" json:"slug"`
	Content   string    `db:"content" json:"content"`
	Published bool      `db:"published" json:"published"`
	SortOrder int32     `db:"sortOrder" json:"sortOrder"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `db:"updatedAt" json:"updatedAt"`
}

// CustomPageColumns 供 SQL 拼接使用。
var CustomPageColumns = []string{"id", "title", "slug", "content", "published", "sortOrder", "createdAt", "updatedAt"}

// FriendRequest 对应 PostgreSQL 表 "FriendRequest"。
type FriendRequest struct {
	Id           string    `db:"id" json:"id"`
	FromId       string    `db:"fromId" json:"fromId"`
	ToId         string    `db:"toId" json:"toId"`
	Message      *string   `db:"message" json:"message"`
	Status       string    `db:"status" json:"status"`
	SearchMethod string    `db:"searchMethod" json:"searchMethod"`
	CreatedAt    time.Time `db:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time `db:"updatedAt" json:"updatedAt"`
}

// FriendRequestColumns 供 SQL 拼接使用。
var FriendRequestColumns = []string{"id", "fromId", "toId", "message", "status", "searchMethod", "createdAt", "updatedAt"}

// Friendship 对应 PostgreSQL 表 "Friendship"。
type Friendship struct {
	Id        string    `db:"id" json:"id"`
	UserA     string    `db:"userA" json:"userA"`
	UserB     string    `db:"userB" json:"userB"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
}

// FriendshipColumns 供 SQL 拼接使用。
var FriendshipColumns = []string{"id", "userA", "userB", "createdAt"}

// Chat 对应 PostgreSQL 表 "Chat"。
type Chat struct {
	Id                 string     `db:"id" json:"id"`
	ParticipantA       string     `db:"participantA" json:"participantA"`
	ParticipantB       string     `db:"participantB" json:"participantB"`
	LastMessage        *string    `db:"lastMessage" json:"lastMessage"`
	LastMessageAt      *time.Time `db:"lastMessageAt" json:"lastMessageAt"`
	VanishMode         bool       `db:"vanishMode" json:"vanishMode"`
	VanishSeconds      *int32     `db:"vanishSeconds" json:"vanishSeconds"`
	RestrictForwarding bool       `db:"restrictForwarding" json:"restrictForwarding"`
	CreatedAt          time.Time  `db:"createdAt" json:"createdAt"`
	UpdatedAt          time.Time  `db:"updatedAt" json:"updatedAt"`
}

// ChatColumns 供 SQL 拼接使用。
var ChatColumns = []string{"id", "participantA", "participantB", "lastMessage", "lastMessageAt", "vanishMode", "vanishSeconds", "restrictForwarding", "createdAt", "updatedAt"}

// ChatHidden 对应 PostgreSQL 表 "ChatHidden"。
type ChatHidden struct {
	Id       string    `db:"id" json:"id"`
	ChatId   string    `db:"chatId" json:"chatId"`
	UserId   string    `db:"userId" json:"userId"`
	HiddenAt time.Time `db:"hiddenAt" json:"hiddenAt"`
}

// ChatHiddenColumns 供 SQL 拼接使用。
var ChatHiddenColumns = []string{"id", "chatId", "userId", "hiddenAt"}

// PrivateMessage 对应 PostgreSQL 表 "PrivateMessage"。
type PrivateMessage struct {
	Id            string     `db:"id" json:"id"`
	ChatId        string     `db:"chatId" json:"chatId"`
	SenderId      string     `db:"senderId" json:"senderId"`
	MsgType       string     `db:"msgType" json:"msgType"`
	Content       string     `db:"content" json:"content"`
	ReplyToId     *string    `db:"replyToId" json:"replyToId"`
	IsRevoked     bool       `db:"isRevoked" json:"isRevoked"`
	Status        string     `db:"status" json:"status"`
	Extra         *string    `db:"extra" json:"extra"`
	Hmac          *string    `db:"hmac" json:"hmac"`
	BurnAfterRead *int32     `db:"burnAfterRead" json:"burnAfterRead"`
	BurnReadAt    *time.Time `db:"burnReadAt" json:"burnReadAt"`
	BurnExpireAt  *time.Time `db:"burnExpireAt" json:"burnExpireAt"`
	CreatedAt     time.Time  `db:"createdAt" json:"createdAt"`
}

// PrivateMessageColumns 供 SQL 拼接使用。
var PrivateMessageColumns = []string{"id", "chatId", "senderId", "msgType", "content", "replyToId", "isRevoked", "status", "extra", "hmac", "burnAfterRead", "burnReadAt", "burnExpireAt", "createdAt"}

// Group 对应 PostgreSQL 表 "Group"。
type Group struct {
	Id           string     `db:"id" json:"id"`
	DialogId     *string    `db:"dialogId" json:"dialogId"`
	Name         string     `db:"name" json:"name"`
	Username     *string    `db:"username" json:"username"`
	Avatar       *string    `db:"avatar" json:"avatar"`
	OwnerId      string     `db:"ownerId" json:"ownerId"`
	Type         string     `db:"type" json:"type"`
	IsPublic     bool       `db:"isPublic" json:"isPublic"`
	MaxMembers   int32      `db:"maxMembers" json:"maxMembers"`
	MemberCount  int32      `db:"memberCount" json:"memberCount"`
	LastMsgSeq   int64      `db:"lastMsgSeq" json:"lastMsgSeq"`
	LastMsgTime  *time.Time `db:"lastMsgTime" json:"lastMsgTime"`
	Announcement *string    `db:"announcement" json:"announcement"`
	CreatedAt    time.Time  `db:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time  `db:"updatedAt" json:"updatedAt"`
}

// GroupColumns 供 SQL 拼接使用。
var GroupColumns = []string{"id", "dialogId", "name", "username", "avatar", "ownerId", "type", "isPublic", "maxMembers", "memberCount", "lastMsgSeq", "lastMsgTime", "announcement", "createdAt", "updatedAt"}

// GroupMember 对应 PostgreSQL 表 "GroupMember"。
type GroupMember struct {
	Id         string     `db:"id" json:"id"`
	GroupId    string     `db:"groupId" json:"groupId"`
	UserId     string     `db:"userId" json:"userId"`
	Role       string     `db:"role" json:"role"`
	Nickname   *string    `db:"nickname" json:"nickname"`
	LastAckSeq int64      `db:"lastAckSeq" json:"lastAckSeq"`
	JoinTime   time.Time  `db:"joinTime" json:"joinTime"`
	MuteUntil  *time.Time `db:"muteUntil" json:"muteUntil"`
	CreatedAt  time.Time  `db:"createdAt" json:"createdAt"`
	UpdatedAt  time.Time  `db:"updatedAt" json:"updatedAt"`
}

// GroupMemberColumns 供 SQL 拼接使用。
var GroupMemberColumns = []string{"id", "groupId", "userId", "role", "nickname", "lastAckSeq", "joinTime", "muteUntil", "createdAt", "updatedAt"}

// GroupMessage 对应 PostgreSQL 表 "GroupMessage"。
type GroupMessage struct {
	Id            string     `db:"id" json:"id"`
	GroupId       string     `db:"groupId" json:"groupId"`
	Seq           int64      `db:"seq" json:"seq"`
	SenderId      string     `db:"senderId" json:"senderId"`
	SenderName    *string    `db:"senderName" json:"senderName"`
	MsgType       string     `db:"msgType" json:"msgType"`
	Content       string     `db:"content" json:"content"`
	ReplyToId     *string    `db:"replyToId" json:"replyToId"`
	IsRevoked     bool       `db:"isRevoked" json:"isRevoked"`
	Extra         *string    `db:"extra" json:"extra"`
	MlsEncrypted  bool       `db:"mlsEncrypted" json:"mlsEncrypted"`
	MlsEpoch      *int32     `db:"mlsEpoch" json:"mlsEpoch"`
	BurnAfterRead *int32     `db:"burnAfterRead" json:"burnAfterRead"`
	BurnExpireAt  *time.Time `db:"burnExpireAt" json:"burnExpireAt"`
	CreatedAt     time.Time  `db:"createdAt" json:"createdAt"`
}

// GroupMessageColumns 供 SQL 拼接使用。
var GroupMessageColumns = []string{"id", "groupId", "seq", "senderId", "senderName", "msgType", "content", "replyToId", "isRevoked", "extra", "mlsEncrypted", "mlsEpoch", "burnAfterRead", "burnExpireAt", "createdAt"}

// GroupInvite 对应 PostgreSQL 表 "GroupInvite"。
type GroupInvite struct {
	Id        string    `db:"id" json:"id"`
	GroupId   string    `db:"groupId" json:"groupId"`
	InviterId string    `db:"inviterId" json:"inviterId"`
	InviteeId string    `db:"inviteeId" json:"inviteeId"`
	Message   *string   `db:"message" json:"message"`
	Status    string    `db:"status" json:"status"`
	CreatedAt time.Time `db:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `db:"updatedAt" json:"updatedAt"`
}

// GroupInviteColumns 供 SQL 拼接使用。
var GroupInviteColumns = []string{"id", "groupId", "inviterId", "inviteeId", "message", "status", "createdAt", "updatedAt"}

// InviteLink 对应 PostgreSQL 表 "InviteLink"。
type InviteLink struct {
	Id        string     `db:"id" json:"id"`
	Hash      string     `db:"hash" json:"hash"`
	GroupId   string     `db:"groupId" json:"groupId"`
	CreatorId string     `db:"creatorId" json:"creatorId"`
	Name      *string    `db:"name" json:"name"`
	ExpireAt  *time.Time `db:"expireAt" json:"expireAt"`
	MaxUses   int32      `db:"maxUses" json:"maxUses"`
	UsedCount int32      `db:"usedCount" json:"usedCount"`
	IsRevoked bool       `db:"isRevoked" json:"isRevoked"`
	CreatedAt time.Time  `db:"createdAt" json:"createdAt"`
}

// InviteLinkColumns 供 SQL 拼接使用。
var InviteLinkColumns = []string{"id", "hash", "groupId", "creatorId", "name", "expireAt", "maxUses", "usedCount", "isRevoked", "createdAt"}
