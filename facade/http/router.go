package http

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	"gorm.io/gorm"

	"github.com/shinya/shineflow/facade/http/handler"
)

func Register(h *server.Hertz, _ *gorm.DB) {
	h.GET("/ping", handler.Ping)
}
