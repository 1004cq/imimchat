package moments

// 动态详情可见性的单测：private/friends/public 三档。
// friends 且非作者本人时需要查好友关系（走 DB/Redis），此处只覆盖不依赖外部服务分支。

import (
	"context"
	"testing"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
)

func TestCanViewMoment(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()
	author := &db.User{Id: "u_author"}
	other := &db.User{Id: "u_other"}
	mk := func(visibility, userID string) *db.Moment {
		return &db.Moment{Id: "m1", UserId: userID, Visibility: visibility}
	}

	// public：任何人可见
	if !h.canViewMoment(ctx, nil, mk("public", "u_author")) {
		t.Error("匿名应能看 public 动态")
	}
	if !h.canViewMoment(ctx, other, mk("public", "u_author")) {
		t.Error("登录用户应能看 public 动态")
	}

	// private：仅作者本人
	if h.canViewMoment(ctx, nil, mk("private", "u_author")) {
		t.Error("匿名不应看到 private 动态")
	}
	if h.canViewMoment(ctx, other, mk("private", "u_author")) {
		t.Error("非作者不应看到 private 动态")
	}
	if !h.canViewMoment(ctx, author, mk("private", "u_author")) {
		t.Error("作者应能看自己的 private 动态")
	}

	// friends：匿名不可见；作者本人可见（不查 DB）
	if h.canViewMoment(ctx, nil, mk("friends", "u_author")) {
		t.Error("匿名不应看到 friends 动态")
	}
	if !h.canViewMoment(ctx, author, mk("friends", "u_author")) {
		t.Error("作者应能看自己的 friends 动态")
	}
}
