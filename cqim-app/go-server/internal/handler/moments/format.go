package moments

import (
	"context"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
)

// ============ JSON 响应类型（字段名与 TS 版一致，camelCase） ============

// userLite 精简用户（评论/点赞内嵌）。
type userLite struct {
	ID       string  `json:"id"`
	Username string  `json:"username"`
	Nickname *string `json:"nickname"`
	Avatar   *string `json:"avatar"`
}

// userLiteBio 精简用户（含个人主页字段）。
type userLiteBio struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	Nickname      *string `json:"nickname"`
	Avatar        *string `json:"avatar"`
	BackgroundURL *string `json:"backgroundUrl"`
	Bio           *string `json:"bio"`
}

type countJSON struct {
	Comments int `json:"comments"`
	Likes    int `json:"likes"`
}

// mediaIn 发布/管理接口的媒体输入。
type mediaIn struct {
	Type     *string `json:"type"`
	URL      *string `json:"url"`
	Width    *int32  `json:"width"`
	Height   *int32  `json:"height"`
	Duration *int32  `json:"duration"`
}

// shareMediaIn 分享格式组装用的媒体输入。
type shareMediaIn struct {
	Type     string
	URL      string
	Width    *int32
	Height   *int32
	Duration *int32
}

// shareMediaJSON 分享格式的媒体（含各档签名缩略图）。
type shareMediaJSON struct {
	Type          string  `json:"type"`
	URL           string  `json:"url"`
	Width         *int32  `json:"width"`
	Height        *int32  `json:"height"`
	Duration      *int32  `json:"duration"`
	Cover         *string `json:"cover"`
	ThumbURL      *string `json:"thumbUrl,omitempty"`
	MediumURL     *string `json:"mediumUrl,omitempty"`
	LowQualityURL *string `json:"lowQualityUrl,omitempty"`
	PosterURL     *string `json:"posterUrl,omitempty"`
}

// mediaFullJSON 完整媒体行 + 签名缩略图（列表/详情/发布响应用）。
type mediaFullJSON struct {
	ID            string    `json:"id"`
	MomentID      string    `json:"momentId"`
	Type          string    `json:"type"`
	URL           string    `json:"url"`
	Width         *int32    `json:"width"`
	Height        *int32    `json:"height"`
	Duration      *int32    `json:"duration"`
	SortOrder     int32     `json:"sortOrder"`
	CreatedAt     time.Time `json:"createdAt"`
	ThumbURL      *string   `json:"thumbUrl,omitempty"`
	MediumURL     *string   `json:"mediumUrl,omitempty"`
	LowQualityURL *string   `json:"lowQualityUrl,omitempty"`
	PosterURL     *string   `json:"posterUrl,omitempty"`
}

// myMediaJSON 我的动态列表的媒体（仅 type/url + 缩略图）。
type myMediaJSON struct {
	Type          string  `json:"type"`
	URL           string  `json:"url"`
	ThumbURL      *string `json:"thumbUrl,omitempty"`
	MediumURL     *string `json:"mediumUrl,omitempty"`
	LowQualityURL *string `json:"lowQualityUrl,omitempty"`
	PosterURL     *string `json:"posterUrl,omitempty"`
}

// shareLikeJSON 分享格式的点赞条目。
type shareLikeJSON struct {
	UserID     string `json:"userId"`
	UserName   string `json:"userName"`
	UserAvatar string `json:"userAvatar"`
	CreatedAt  int64  `json:"createdAt"`
}

// shareCommentJSON 分享格式的评论条目（展平，含嵌套回复）。
type shareCommentJSON struct {
	ID              string  `json:"id"`
	UserID          string  `json:"userId"`
	UserName        string  `json:"userName"`
	UserAvatar      string  `json:"userAvatar"`
	Content         string  `json:"content"`
	ParentID        *string `json:"parentId"`
	ReplyToUserID   *string `json:"replyToUserId"`
	ReplyToUserName *string `json:"replyToUserName"`
	CreatedAt       int64   `json:"createdAt"`
	IsDeleted       bool    `json:"isDeleted"`
}

// shareMomentJSON formatMomentForShare 的输出格式。
type shareMomentJSON struct {
	ID           string             `json:"id"`
	AuthorID     string             `json:"authorId"`
	AuthorName   string             `json:"authorName"`
	AuthorAvatar string             `json:"authorAvatar"`
	Content      string             `json:"content"`
	Media        []shareMediaJSON   `json:"media"`
	Images       []string           `json:"images"`
	Videos       []string           `json:"videos"`
	CoverURL     *string            `json:"coverUrl"`
	Topics       []string           `json:"topics"`
	Location     *string            `json:"location"`
	Visibility   string             `json:"visibility"`
	IsPinned     bool               `json:"isPinned"`
	PinnedAt     *int64             `json:"pinnedAt"`
	SortOrder    int32              `json:"sortOrder"`
	CreatedAt    int64              `json:"createdAt"`
	UpdatedAt    *int64             `json:"updatedAt"`
	LikeCount    int                `json:"likeCount"`
	CommentCount int                `json:"commentCount"`
	IsLiked      bool               `json:"isLiked"`
	Likes        []shareLikeJSON    `json:"likes"`
	Comments     []shareCommentJSON `json:"comments"`
}

// feedResult Feed 接口响应。
type feedResult struct {
	Moments    []shareMomentJSON `json:"moments"`
	HasMore    bool              `json:"hasMore"`
	NextCursor *string           `json:"nextCursor"`
	Ts         int64             `json:"_ts"`
}

// shareUserJSON 外链页用户资料。
type shareUserJSON struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	Nickname      *string `json:"nickname"`
	Avatar        *string `json:"avatar"`
	BackgroundURL *string `json:"backgroundUrl"`
	Bio           *string `json:"bio"`
}

// shareListResult 带 userId 的列表（外链/个人主页）响应。
// Bio/BackgroundURL 用双重指针：外层 nil 表示整个字段省略，
// 外层非 nil 内层 nil 表示显式 null（与 TS 的条件展开语义一致）。
type shareListResult struct {
	Moments       []shareMomentJSON `json:"moments"`
	HasMore       bool              `json:"hasMore"`
	NextCursor    *string           `json:"nextCursor"`
	Total         *int64            `json:"total,omitempty"`
	User          *shareUserJSON    `json:"user,omitempty"`
	Bio           **string          `json:"bio,omitempty"`
	BackgroundURL **string          `json:"backgroundUrl,omitempty"`
	Ts            int64             `json:"_ts"`
}

// momentCardJSON 公开广场列表的动态卡片（对应 TS 非分享模式的展开格式）。
type momentCardJSON struct {
	ID           string          `json:"id"`
	UserID       string          `json:"userId"`
	Content      string          `json:"content"`
	Visibility   string          `json:"visibility"`
	Location     *string         `json:"location"`
	Topics       []string        `json:"topics"`
	IsPinned     bool            `json:"isPinned"`
	PinnedAt     *time.Time      `json:"pinnedAt"`
	ViewCount    int32           `json:"viewCount"`
	SortOrder    int32           `json:"sortOrder"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	User         *userLiteBio    `json:"user"`
	Media        []mediaFullJSON `json:"media"`
	Count        countJSON       `json:"_count"`
	LikeCount    int             `json:"likeCount"`
	CommentCount int             `json:"commentCount"`
	IsLiked      bool            `json:"isLiked"`
}

// listResult 公开广场列表响应。
type listResult struct {
	Moments    []momentCardJSON `json:"moments"`
	HasMore    bool             `json:"hasMore"`
	NextCursor *string          `json:"nextCursor"`
	Ts         int64            `json:"_ts"`
}

// publishMomentJSON 发布动态的响应 moment。
type publishMomentJSON struct {
	ID           string          `json:"id"`
	UserID       string          `json:"userId"`
	Content      string          `json:"content"`
	Visibility   string          `json:"visibility"`
	Location     *string         `json:"location"`
	Topics       []string        `json:"topics"`
	IsPinned     bool            `json:"isPinned"`
	PinnedAt     *time.Time      `json:"pinnedAt"`
	ViewCount    int32           `json:"viewCount"`
	SortOrder    int32           `json:"sortOrder"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	User         *userLite       `json:"user"`
	Media        []mediaFullJSON `json:"media"`
	Count        countJSON       `json:"_count"`
	LikeCount    int             `json:"likeCount"`
	CommentCount int             `json:"commentCount"`
	IsLiked      bool            `json:"isLiked"`
}

// myMomentJSON 我的动态列表条目。
type myMomentJSON struct {
	ID           string        `json:"id"`
	Content      string        `json:"content"`
	Visibility   string        `json:"visibility"`
	Location     *string       `json:"location"`
	IsPinned     bool          `json:"isPinned"`
	SortOrder    int32         `json:"sortOrder"`
	CreatedAt    int64         `json:"createdAt"`
	Media        []myMediaJSON `json:"media"`
	LikeCount    int           `json:"likeCount"`
	CommentCount int           `json:"commentCount"`
}

// commentReplyJSON 详情页嵌套回复（仅一层，不再嵌套）。
type commentReplyJSON struct {
	ID        string    `json:"id"`
	MomentID  string    `json:"momentId"`
	UserID    string    `json:"userId"`
	Content   string    `json:"content"`
	ReplyToID *string   `json:"replyToId"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	User      *userLite `json:"user"`
}

// commentDetailJSON 详情页顶层评论（含回复）。
type commentDetailJSON struct {
	ID        string             `json:"id"`
	MomentID  string             `json:"momentId"`
	UserID    string             `json:"userId"`
	Content   string             `json:"content"`
	ReplyToID *string            `json:"replyToId"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt time.Time          `json:"updatedAt"`
	User      *userLite          `json:"user"`
	Replies   []commentReplyJSON `json:"replies"`
}

// likeDetailJSON 详情页点赞条目。
type likeDetailJSON struct {
	ID        string    `json:"id"`
	MomentID  string    `json:"momentId"`
	UserID    string    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
	User      *userLite `json:"user"`
}

// momentDetailJSON 单条动态详情响应。
type momentDetailJSON struct {
	ID           string              `json:"id"`
	UserID       string              `json:"userId"`
	Content      string              `json:"content"`
	Visibility   string              `json:"visibility"`
	Location     *string             `json:"location"`
	Topics       []string            `json:"topics"`
	IsPinned     bool                `json:"isPinned"`
	PinnedAt     *time.Time          `json:"pinnedAt"`
	ViewCount    int32               `json:"viewCount"`
	SortOrder    int32               `json:"sortOrder"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
	User         *userLiteBio        `json:"user"`
	Media        []mediaFullJSON     `json:"media"`
	Comments     []commentDetailJSON `json:"comments"`
	Likes        []likeDetailJSON    `json:"likes"`
	Count        countJSON           `json:"_count"`
	LikeCount    int                 `json:"likeCount"`
	CommentCount int                 `json:"commentCount"`
	IsLiked      bool                `json:"isLiked"`
}

// createdCommentJSON 发表评论的响应 comment（含评论者）。
type createdCommentJSON struct {
	ID        string    `json:"id"`
	MomentID  string    `json:"momentId"`
	UserID    string    `json:"userId"`
	Content   string    `json:"content"`
	ReplyToID *string   `json:"replyToId"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	User      *userLite `json:"user"`
}

type topicItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type hotTopicsResult struct {
	Topics []topicItem `json:"topics"`
}

// ============ 批量查询 helpers ============

// getUserLite 按 id 取精简用户。
func (h *Handler) getUserLite(ctx context.Context, id string) *db.User {
	u, err := db.QueryRowToStruct[db.User](ctx, h.deps.DB,
		`SELECT "id","username","nickname","avatar","backgroundUrl","bio" FROM "User" WHERE "id"=$1`, id)
	if err != nil {
		return nil
	}
	return u
}

// batchUsers 批量取用户并按 id 建索引。
func (h *Handler) batchUsers(ctx context.Context, ids []string) map[string]*db.User {
	m := make(map[string]*db.User)
	if len(ids) == 0 {
		return m
	}
	// 用 UserBrief 精简 struct，避免 pgx RowToStructByName 因缺列报错。
	rows, err := db.QueryToStructs[db.UserBrief](ctx, h.deps.DB,
		`SELECT "id","username","nickname","avatar","backgroundUrl","bio" FROM "User" WHERE "id"=ANY($1)`, ids)
	if err != nil {
		return m
	}
	for i := range rows {
		u := rows[i].ToUser()
		m[u.Id] = &u
	}
	return m
}

// batchMedia 批量取媒体并按 momentId 分组（sortOrder 升序）。
func (h *Handler) batchMedia(ctx context.Context, momentIDs []string) map[string][]db.MomentMedia {
	m := make(map[string][]db.MomentMedia)
	if len(momentIDs) == 0 {
		return m
	}
	rows, err := db.QueryToStructs[db.MomentMedia](ctx, h.deps.DB,
		`SELECT "id","momentId","type","url","width","height","duration","sortOrder","createdAt"
		 FROM "MomentMedia" WHERE "momentId"=ANY($1) ORDER BY "sortOrder" ASC`, momentIDs)
	if err != nil {
		return m
	}
	for _, md := range rows {
		m[md.MomentId] = append(m[md.MomentId], md)
	}
	return m
}

// momentCounts 按 momentId 统计点赞/评论数（对应 Prisma _count）。
func (h *Handler) momentCounts(ctx context.Context, table string, ids []string) map[string]int64 {
	m := make(map[string]int64)
	if len(ids) == 0 {
		return m
	}
	type row struct {
		MomentID string `db:"momentId"`
		N        int64  `db:"n"`
	}
	rows, err := db.QueryToStructs[row](ctx, h.deps.DB,
		`SELECT "momentId", COUNT(*) AS "n" FROM "`+table+`" WHERE "momentId"=ANY($1) GROUP BY "momentId"`, ids)
	if err != nil {
		return m
	}
	for _, r := range rows {
		m[r.MomentID] = r.N
	}
	return m
}

func uniqueIDs(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ============ formatMomentForShare（对应 TS 同名函数） ============

// commentFullRow 评论 + 评论者 + 被回复者（展平查询行）。
type commentFullRow struct {
	ID        string    `db:"id"`
	MomentID  string    `db:"momentId"`
	UserID    string    `db:"userId"`
	Content   string    `db:"content"`
	ReplyToID *string   `db:"replyToId"`
	CreatedAt time.Time `db:"createdAt"`
	UpdatedAt time.Time `db:"updatedAt"`
	UName     string    `db:"uname"`
	UNick     *string   `db:"unick"`
	UAvatar   *string   `db:"uavatar"`
	RUID      *string   `db:"ruid"`
	RUName    *string   `db:"runame"`
	RUNick    *string   `db:"runick"`
}

// likeFullRow 点赞 + 点赞用户。
type likeFullRow struct {
	ID        string    `db:"id"`
	MomentID  string    `db:"momentId"`
	UserID    string    `db:"userId"`
	CreatedAt time.Time `db:"createdAt"`
	UName     string    `db:"uname"`
	UNick     *string   `db:"unick"`
	UAvatar   *string   `db:"uavatar"`
}

const commentUserJoin = `c."id", c."momentId", c."userId", c."content", c."replyToId", c."createdAt", c."updatedAt",
	u."username" AS "uname", u."nickname" AS "unick", u."avatar" AS "uavatar",
	ru."id" AS "ruid", ru."username" AS "runame", ru."nickname" AS "runick"`

const commentUserFrom = `FROM "MomentComment" c
	JOIN "User" u ON u."id" = c."userId"
	LEFT JOIN "MomentComment" pc ON pc."id" = c."replyToId"
	LEFT JOIN "User" ru ON ru."id" = pc."userId"`

// formatShareComment 格式化单条评论（含被回复者信息）。
func formatShareComment(cr commentFullRow) shareCommentJSON {
	var replyToUserID, replyToUserName *string
	if cr.RUID != nil {
		replyToUserID = cr.RUID
		name := ""
		if cr.RUNick != nil && *cr.RUNick != "" {
			name = *cr.RUNick
		} else if cr.RUName != nil {
			name = *cr.RUName
		}
		replyToUserName = &name
	}
	return shareCommentJSON{
		ID:              cr.ID,
		UserID:          cr.UserID,
		UserName:        displayName(cr.UNick, cr.UName),
		UserAvatar:      avatarToProxy(cr.UAvatar),
		Content:         cr.Content,
		ParentID:        cr.ReplyToID,
		ReplyToUserID:   replyToUserID,
		ReplyToUserName: replyToUserName,
		CreatedAt:       cr.CreatedAt.UnixMilli(),
		IsDeleted:       false,
	}
}

// loadShareMoments 批量组装分享格式动态（Feed / 外链页共用）。
// likeTake / commentTake / replyTake 对应各场景的 include take（Feed: 10/10/5，外链页: 20/50/0）。
func (h *Handler) loadShareMoments(ctx context.Context, moments []db.Moment, liked map[string]bool, likeTake, commentTake, replyTake int) ([]shareMomentJSON, error) {
	out := make([]shareMomentJSON, 0, len(moments))
	if len(moments) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(moments))
	authorIDs := make([]string, 0, len(moments))
	for _, m := range moments {
		ids = append(ids, m.Id)
		authorIDs = append(authorIDs, m.UserId)
	}
	users := h.batchUsers(ctx, uniqueIDs(authorIDs))
	mediaMap := h.batchMedia(ctx, ids)
	likeCounts := h.momentCounts(ctx, "MomentLike", ids)
	commentCounts := h.momentCounts(ctx, "MomentComment", ids)

	// 点赞（asc）+ 用户
	likeRows, err := db.QueryToStructs[likeFullRow](ctx, h.deps.DB,
		`SELECT l."id", l."momentId", l."userId", l."createdAt",
			u."username" AS "uname", u."nickname" AS "unick", u."avatar" AS "uavatar"
		 FROM "MomentLike" l JOIN "User" u ON u."id" = l."userId"
		 WHERE l."momentId" = ANY($1) ORDER BY l."createdAt" ASC`, ids)
	if err != nil {
		return nil, err
	}
	likesByMoment := make(map[string][]likeFullRow)
	for _, lr := range likeRows {
		likesByMoment[lr.MomentID] = append(likesByMoment[lr.MomentID], lr)
	}

	// 顶层评论（asc）+ 用户
	topRows, err := db.QueryToStructs[commentFullRow](ctx, h.deps.DB,
		`SELECT `+commentUserJoin+` `+commentUserFrom+`
		 WHERE c."momentId" = ANY($1) AND c."replyToId" IS NULL ORDER BY c."createdAt" ASC`, ids)
	if err != nil {
		return nil, err
	}
	topsByMoment := make(map[string][]commentFullRow)
	topIDs := []string{}
	for _, cr := range topRows {
		topsByMoment[cr.MomentID] = append(topsByMoment[cr.MomentID], cr)
		topIDs = append(topIDs, cr.ID)
	}
	// 嵌套回复（asc，每条顶层评论 take N）
	repliesByParent := make(map[string][]commentFullRow)
	if replyTake > 0 && len(topIDs) > 0 {
		replyRows, err := db.QueryToStructs[commentFullRow](ctx, h.deps.DB,
			`SELECT `+commentUserJoin+` `+commentUserFrom+`
			 WHERE c."replyToId" = ANY($1) ORDER BY c."createdAt" ASC`, topIDs)
		if err != nil {
			return nil, err
		}
		for _, rr := range replyRows {
			if rr.ReplyToID == nil {
				continue
			}
			repliesByParent[*rr.ReplyToID] = append(repliesByParent[*rr.ReplyToID], rr)
		}
	}

	for _, m := range moments {
		u := users[m.UserId]
		authorID := m.UserId
		authorName := ""
		authorAvatar := ""
		if u != nil {
			authorID = u.Id
			authorName = displayName(u.Nickname, u.Username)
			authorAvatar = h.avatarToSigned(ctx, u.Avatar)
		}

		var mediaIn []shareMediaIn
		for _, md := range mediaMap[m.Id] {
			mediaIn = append(mediaIn, shareMediaIn{
				Type: md.Type, URL: md.Url, Width: md.Width, Height: md.Height, Duration: md.Duration,
			})
		}
		proxyMedia := h.signShareMedia(ctx, mediaIn)

		likes := []shareLikeJSON{}
		for i, lr := range likesByMoment[m.Id] {
			if i >= likeTake {
				break
			}
			likes = append(likes, shareLikeJSON{
				UserID:     lr.UserID,
				UserName:   displayName(lr.UNick, lr.UName),
				UserAvatar: avatarToProxy(lr.UAvatar),
				CreatedAt:  lr.CreatedAt.UnixMilli(),
			})
		}

		comments := []shareCommentJSON{}
		for i, cr := range topsByMoment[m.Id] {
			if i >= commentTake {
				break
			}
			comments = append(comments, formatShareComment(cr))
			for j, rr := range repliesByParent[cr.ID] {
				if j >= replyTake {
					break
				}
				comments = append(comments, formatShareComment(rr))
			}
		}

		// 老分享页兼容字段：images / videos / coverUrl
		images := []string{}
		videos := []string{}
		var coverURL *string
		for _, item := range proxyMedia {
			if item.URL == "" {
				continue
			}
			if item.Type == "image" {
				img := item.URL
				if item.MediumURL != nil && *item.MediumURL != "" {
					img = *item.MediumURL
				}
				images = append(images, img)
			} else if item.Type == "video" {
				videos = append(videos, item.URL)
				if coverURL == nil {
					if item.PosterURL != nil {
						coverURL = item.PosterURL
					} else if item.ThumbURL != nil {
						coverURL = item.ThumbURL
					}
				}
			}
		}

		var pinnedAt *int64
		if m.PinnedAt != nil {
			v := m.PinnedAt.UnixMilli()
			pinnedAt = &v
		}
		updated := m.UpdatedAt.UnixMilli()
		var loc *string
		if m.Location != nil && *m.Location != "" {
			loc = m.Location
		}
		out = append(out, shareMomentJSON{
			ID: m.Id, AuthorID: authorID, AuthorName: authorName, AuthorAvatar: authorAvatar,
			Content: m.Content, Media: proxyMedia,
			Images: images, Videos: videos, CoverURL: coverURL,
			Topics: splitTopics(m.Topics), Location: loc, Visibility: m.Visibility,
			IsPinned: m.IsPinned, PinnedAt: pinnedAt, SortOrder: m.SortOrder,
			CreatedAt: m.CreatedAt.UnixMilli(), UpdatedAt: &updated,
			LikeCount: int(likeCounts[m.Id]), CommentCount: int(commentCounts[m.Id]),
			IsLiked: liked[m.Id], Likes: likes, Comments: comments,
		})
	}
	return out, nil
}

// buildMomentCards 组装公开广场列表卡片（对应 TS 非分享模式格式）。
func (h *Handler) buildMomentCards(ctx context.Context, moments []db.Moment, liked map[string]bool) []momentCardJSON {
	cards := make([]momentCardJSON, 0, len(moments))
	if len(moments) == 0 {
		return cards
	}
	ids := make([]string, 0, len(moments))
	authorIDs := make([]string, 0, len(moments))
	for _, m := range moments {
		ids = append(ids, m.Id)
		authorIDs = append(authorIDs, m.UserId)
	}
	users := h.batchUsers(ctx, uniqueIDs(authorIDs))
	mediaMap := h.batchMedia(ctx, ids)
	likeCounts := h.momentCounts(ctx, "MomentLike", ids)
	commentCounts := h.momentCounts(ctx, "MomentComment", ids)

	for _, m := range moments {
		var uu *userLiteBio
		if u := users[m.UserId]; u != nil {
			uu = &userLiteBio{
				ID: u.Id, Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar,
				BackgroundURL: u.BackgroundUrl, Bio: u.Bio,
			}
		}
		cards = append(cards, momentCardJSON{
			ID: m.Id, UserID: m.UserId, Content: m.Content, Visibility: m.Visibility,
			Location: m.Location, Topics: splitTopics(m.Topics),
			IsPinned: m.IsPinned, PinnedAt: m.PinnedAt, ViewCount: m.ViewCount,
			SortOrder: m.SortOrder, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
			User: uu, Media: h.signMediaFull(ctx, mediaMap[m.Id]),
			Count:        countJSON{Comments: int(commentCounts[m.Id]), Likes: int(likeCounts[m.Id])},
			LikeCount:    int(likeCounts[m.Id]),
			CommentCount: int(commentCounts[m.Id]),
			IsLiked:      liked[m.Id],
		})
	}
	return cards
}

// detailComments 详情页评论（顶层 + 嵌套回复，asc）。
func (h *Handler) detailComments(ctx context.Context, momentID string) []commentDetailJSON {
	out := []commentDetailJSON{}
	topRows, err := db.QueryToStructs[commentFullRow](ctx, h.deps.DB,
		`SELECT `+commentUserJoin+` `+commentUserFrom+`
		 WHERE c."momentId"=$1 AND c."replyToId" IS NULL ORDER BY c."createdAt" ASC`, momentID)
	if err != nil {
		return out
	}
	topIDs := make([]string, 0, len(topRows))
	for _, cr := range topRows {
		topIDs = append(topIDs, cr.ID)
	}
	repliesByParent := make(map[string][]commentReplyJSON)
	if len(topIDs) > 0 {
		replyRows, err := db.QueryToStructs[commentFullRow](ctx, h.deps.DB,
			`SELECT `+commentUserJoin+` `+commentUserFrom+`
			 WHERE c."replyToId"=ANY($1) ORDER BY c."createdAt" ASC`, topIDs)
		if err == nil {
			for _, rr := range replyRows {
				if rr.ReplyToID == nil {
					continue
				}
				repliesByParent[*rr.ReplyToID] = append(repliesByParent[*rr.ReplyToID], commentReplyJSON{
					ID: rr.ID, MomentID: rr.MomentID, UserID: rr.UserID, Content: rr.Content,
					ReplyToID: rr.ReplyToID, CreatedAt: rr.CreatedAt, UpdatedAt: rr.UpdatedAt,
					User: &userLite{ID: rr.UserID, Username: rr.UName, Nickname: rr.UNick, Avatar: rr.UAvatar},
				})
			}
		}
	}
	for _, cr := range topRows {
		replies := repliesByParent[cr.ID]
		if replies == nil {
			replies = []commentReplyJSON{}
		}
		out = append(out, commentDetailJSON{
			ID: cr.ID, MomentID: cr.MomentID, UserID: cr.UserID, Content: cr.Content,
			ReplyToID: cr.ReplyToID, CreatedAt: cr.CreatedAt, UpdatedAt: cr.UpdatedAt,
			User:    &userLite{ID: cr.UserID, Username: cr.UName, Nickname: cr.UNick, Avatar: cr.UAvatar},
			Replies: replies,
		})
	}
	return out
}

// detailLikes 详情页点赞列表（asc）。
func (h *Handler) detailLikes(ctx context.Context, momentID string) []likeDetailJSON {
	out := []likeDetailJSON{}
	rows, err := db.QueryToStructs[likeFullRow](ctx, h.deps.DB,
		`SELECT l."id", l."momentId", l."userId", l."createdAt",
			u."username" AS "uname", u."nickname" AS "unick", u."avatar" AS "uavatar"
		 FROM "MomentLike" l JOIN "User" u ON u."id" = l."userId"
		 WHERE l."momentId"=$1 ORDER BY l."createdAt" ASC`, momentID)
	if err != nil {
		return out
	}
	for _, lr := range rows {
		out = append(out, likeDetailJSON{
			ID: lr.ID, MomentID: lr.MomentID, UserID: lr.UserID, CreatedAt: lr.CreatedAt,
			User: &userLite{ID: lr.UserID, Username: lr.UName, Nickname: lr.UNick, Avatar: lr.UAvatar},
		})
	}
	return out
}
