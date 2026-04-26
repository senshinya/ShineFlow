package http

import (
	"github.com/cloudwego/hertz/pkg/app/server"

	"github.com/shinya/shineflow/facade/http/handler"
)

func Register(h *server.Hertz) {
	h.GET("/ping", handler.Ping)
}
