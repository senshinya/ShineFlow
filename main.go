package main

import (
	"github.com/cloudwego/hertz/pkg/app/server"

	httpfacade "github.com/shinya/shineflow/facade/http"
	"github.com/shinya/shineflow/infrastructure/config"
	"github.com/shinya/shineflow/infrastructure/storage"
)

func main() {
	cfg := config.Load()
	storage.MustInit(cfg.DBDSN)

	h := server.New(server.WithHostPorts(":" + cfg.Port))
	httpfacade.Register(h)
	h.Spin()
}
