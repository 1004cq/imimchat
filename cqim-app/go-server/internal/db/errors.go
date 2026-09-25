package db

import "errors"

var errMissingDSN = errors.New("DATABASE_URL 未设置")
