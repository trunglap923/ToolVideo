package main

import (
	"go.uber.org/zap"
	"krillin-ai/config"
	"krillin-ai/internal/deps"
	"krillin-ai/internal/server"
	"krillin-ai/log"
	"os"
)

func main() {
	log.InitLogger()
	defer log.GetLogger().Sync()

	var err error
	if !config.LoadConfig() {
		return
	}

	if err = config.CheckConfig(); err != nil {
		log.GetLogger().Error("Tải cấu hình thất bại", zap.Error(err))
		return
	}

	if err = deps.CheckDependency(); err != nil {
		log.GetLogger().Error("Chuẩn bị môi trường thất bại", zap.Error(err))
		return
	}
	if err = server.StartBackend(); err != nil {
		log.GetLogger().Error("Khởi động dịch vụ backend thất bại", zap.Error(err))
		os.Exit(1)
	}
}
