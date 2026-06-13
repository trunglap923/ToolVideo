package main

import (
	"go.uber.org/zap"
	"krillin-ai/config"
	"krillin-ai/internal/desktop"
	"krillin-ai/internal/server"
	"krillin-ai/log"
	"os"
)

func main() {
	log.InitLogger()
	defer log.GetLogger().Sync()

	if !config.LoadConfig() {
		// 确保有最基础的配置
		err := config.SaveConfig()
		if err != nil {
			log.GetLogger().Error("Lưu cấu hình thất bại", zap.Error(err))
			os.Exit(1)
		}
	}
	go func() {
		if err := server.StartBackend(); err != nil {
			log.GetLogger().Error("Khởi động dịch vụ backend thất bại", zap.Error(err))
			os.Exit(1)
		}
	}()
	config.ConfigBackup = config.Conf
	desktop.Show()
}
