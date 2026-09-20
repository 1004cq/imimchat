package api

import (
	"strings"
	"testing"
)

func TestMomentMapStringAcceptsStringOrPointer(t *testing.T) {
	nick := "Ada"
	cases := []struct {
		in   any
		want string
	}{
		{"Ada", "Ada"},
		{&nick, "Ada"},
		{(*string)(nil), ""},
		{nil, ""},
		{12, ""},
	}
	for _, tc := range cases {
		if got := momentMapString(tc.in); got != tc.want {
			t.Fatalf("momentMapString(%T)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestMomentAuthorFieldsSafeAvatarAndNickname(t *testing.T) {
	cos := "https://bucket.cos.ap-guangzhou.myqcloud.com/a"
	nick := "Ada"
	name, avatar := momentAuthorFields(map[string]any{
		"nickname": &nick,
		"username": "ada_user",
		"avatar":   cos,
	})
	if name != "Ada" {
		t.Fatalf("name=%q", name)
	}
	if avatar != "" {
		t.Fatalf("COS avatar should be stripped, got %q", avatar)
	}

	name, avatar = momentAuthorFields(map[string]any{
		"nickname": "",
		"username": "ada_user",
		"avatar":   "/api/media/abc",
	})
	if name != "ada_user" {
		t.Fatalf("fallback name=%q", name)
	}
	if avatar != "/api/media/abc" {
		t.Fatalf("avatar=%q", avatar)
	}

	// Must not panic when nickname is a plain string (the old *string assert did).
	name, _ = momentAuthorFields(map[string]any{
		"nickname": "plain",
		"username": "u",
		"avatar":   "",
	})
	if name != "plain" {
		t.Fatalf("plain nickname name=%q", name)
	}
}

func TestMomentsListMode(t *testing.T) {
	viewer := &user{ID: "me"}
	if got := momentsListMode(nil, ""); got != "public" {
		t.Fatalf("anon list=%s", got)
	}
	if got := momentsListMode(viewer, ""); got != "friends" {
		t.Fatalf("auth list=%s", got)
	}
	if got := momentsListMode(viewer, "other"); got != "profile" {
		t.Fatalf("profile=%s", got)
	}
	if got := momentsListMode(nil, "other"); got != "profile" {
		t.Fatalf("anon profile=%s", got)
	}
}

func TestMomentsFriendsWhereIncludesSelfAndFriendsVisibility(t *testing.T) {
	where := momentsFriendsWhere()
	for _, needle := range []string{`userId"=ANY($1)`, `"userId"=$2`, `'public'`, `'friends'`} {
		if !strings.Contains(where, needle) {
			t.Fatalf("friends where missing %s: %s", needle, where)
		}
	}
	if strings.Contains(where, `'private'`) {
		t.Fatalf("friends where should not list private explicitly: %s", where)
	}
}

func TestMomentsProfileWhere(t *testing.T) {
	where, args := momentsProfileWhere(nil, "u2")
	if !strings.Contains(where, `"userId"=$1`) || !strings.Contains(where, `"visibility"='public'`) {
		t.Fatalf("anon profile where=%s", where)
	}
	if len(args) != 1 || args[0] != "u2" {
		t.Fatalf("anon profile args=%v", args)
	}

	where, args = momentsProfileWhere(&user{ID: "me"}, "u2")
	if !strings.Contains(where, `"visibility"='friends'`) || !strings.Contains(where, `Friendship`) {
		t.Fatalf("friend profile where=%s", where)
	}
	if len(args) != 2 || args[1] != "me" {
		t.Fatalf("friend profile args=%v", args)
	}

	where, args = momentsProfileWhere(&user{ID: "me"}, "me")
	if strings.Contains(where, `visibility`) {
		t.Fatalf("own profile should not filter visibility: %s", where)
	}
	if len(args) != 1 {
		t.Fatalf("own profile args=%v", args)
	}
}
