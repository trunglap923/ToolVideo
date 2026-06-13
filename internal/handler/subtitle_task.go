package handler

import (
	"krillin-ai/internal/deps"
	"krillin-ai/internal/dto"
	"krillin-ai/internal/response"
	"krillin-ai/internal/service"
	"krillin-ai/log"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h Handler) StartSubtitleTask(c *gin.Context) {
	var req dto.StartVideoSubtitleTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		log.GetLogger().Error("StartSubtitleTask ShouldBindJSON err", zap.Error(err))
		response.R(c, response.Response{
			Error: -1,
			Msg:   "Parameter error",
			Data:  nil,
		})
		return
	}

	// 检查配置是否需要重新初始化
	if configUpdated {
		log.GetLogger().Info("检测到配置更新，重新初始化服务")
		deps.CheckDependency()
		h.Service = service.NewService()
		configUpdated = false
	}

	svc := h.Service

	data, err := svc.StartSubtitleTask(req)
	if err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   err.Error(),
			Data:  nil,
		})
		return
	}
	response.R(c, response.Response{
		Error: 0,
		Msg:   "Success",
		Data:  data,
	})
}

func (h Handler) GetSubtitleTask(c *gin.Context) {
	var req dto.GetVideoSubtitleTaskReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "Parameter error",
			Data:  nil,
		})
		return
	}

	// 检查配置是否需要重新初始化
	if configUpdated {
		log.GetLogger().Info("检测到配置更新，重新初始化服务")
		h.Service = service.NewService()
		configUpdated = false
	}

	svc := h.Service
	data, err := svc.GetTaskStatus(req)
	if err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   err.Error(),
			Data:  nil,
		})
		return
	}
	response.R(c, response.Response{
		Error: 0,
		Msg:   "Success",
		Data:  data,
	})
}

func (h Handler) UploadFile(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "Failed to get file",
			Data:  nil,
		})
		return
	}

	files := form.File["file"]
	if len(files) == 0 {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "No file uploaded",
			Data:  nil,
		})
		return
	}

	// 保存每个文件
	var savedFiles []string
	for _, file := range files {
		savePath := "./uploads/" + file.Filename
		if err := c.SaveUploadedFile(file, savePath); err != nil {
			response.R(c, response.Response{
				Error: -1,
				Msg:   "Failed to save file: " + file.Filename,
				Data:  nil,
			})
			return
		}
		savedFiles = append(savedFiles, "local:"+savePath)
	}

	response.R(c, response.Response{
		Error: 0,
		Msg:   "File uploaded successfully",
		Data:  gin.H{"file_path": savedFiles},
	})
}

func (h Handler) DownloadFile(c *gin.Context) {
	requestedFile := c.Param("filepath")
	if requestedFile == "" {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "File path is empty",
			Data:  nil,
		})
		return
	}

	localFilePath := filepath.Join(".", requestedFile)
	if _, err := os.Stat(localFilePath); os.IsNotExist(err) {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "File does not exist",
			Data:  nil,
		})
		return
	}
	c.FileAttachment(localFilePath, filepath.Base(localFilePath))
}
