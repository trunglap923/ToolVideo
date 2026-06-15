package router

import (
	"krillin-ai/internal/handler"
	"krillin-ai/static"
	"net/http"

	"github.com/gin-gonic/gin"
)

func SetupRouter(r *gin.Engine) {
	api := r.Group("/api")

	hdl := handler.NewHandler()
	{
		api.POST("/capability/subtitleTask", hdl.StartSubtitleTask)
		api.GET("/capability/subtitleTask", hdl.GetSubtitleTask)
		api.POST("/capability/subtitleTask/cancel", hdl.CancelSubtitleTask)
		api.POST("/file", hdl.UploadFile)
		api.GET("/file/*filepath", hdl.DownloadFile)
		api.HEAD("/file/*filepath", hdl.DownloadFile)
		api.GET("/config", hdl.GetConfig)
		api.POST("/config", hdl.UpdateConfig)
		api.GET("/task/subtitles", hdl.GetTaskSubtitles)
		api.POST("/task/update_subtitles", hdl.UpdateTaskSubtitles)
		api.POST("/task/export_video", hdl.ExportVideoTask)
		api.POST("/task/run_whisper", hdl.RunWhisperTask)
		api.POST("/task/run_translate", hdl.RunTranslateTask)
		api.POST("/task/run_tts_only", hdl.RunTtsOnlyTask)
	}

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/static")
	})
	r.StaticFS("/static", http.FS(static.EmbeddedFiles))
	r.Static("/tasks", "./tasks")
}
