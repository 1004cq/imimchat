package handler

import (
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/config"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/redisx"
)

// Deps 所有业务 handler 共享的依赖。
type Deps struct {
	Cfg   *config.Config
	DB    *db.DB
	Redis *redisx.Client
	Auth  *middleware.AuthContext
}
