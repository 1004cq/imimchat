package db

// sanitizeDSN 的单测：只剔除 Prisma 专有参数，保留 sslmode 等合法参数。

import "testing"

func TestSanitizeDSN(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "仅 schema 参数时整个 query 被去掉",
			in:   "postgres://u:p@localhost:5432/db?schema=public",
			want: "postgres://u:p@localhost:5432/db",
		},
		{
			name: "合法参数 sslmode 必须保留",
			in:   "postgres://u:p@localhost:5432/db?sslmode=require",
			want: "postgres://u:p@localhost:5432/db?sslmode=require",
		},
		{
			name: "混合参数：只剔除 schema",
			in:   "postgres://u:p@localhost:5432/db?schema=public&sslmode=require&connect_timeout=5",
			want: "postgres://u:p@localhost:5432/db?sslmode=require&connect_timeout=5",
		},
		{
			name: "无参数保持不变",
			in:   "postgres://u:p@localhost:5432/db",
			want: "postgres://u:p@localhost:5432/db",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeDSN(c.in); got != c.want {
				t.Errorf("sanitizeDSN(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
