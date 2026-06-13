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
			Msg:   "Lỗi tham số",
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
		Msg:   "Thành công",
		Data:  data,
	})
}

func (h Handler) GetSubtitleTask(c *gin.Context) {
	var req dto.GetVideoSubtitleTaskReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "Lỗi tham số",
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
		Msg:   "Thành công",
		Data:  data,
	})
}

func (h Handler) CancelSubtitleTask(c *gin.Context) {
	var req dto.CancelVideoSubtitleTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		log.GetLogger().Error("CancelSubtitleTask ShouldBindJSON err", zap.Error(err))
		response.R(c, response.Response{
			Error: -1,
			Msg:   "Lỗi tham số",
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
	err := svc.CancelTask(req)
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
		Msg:   "Thành công",
		Data:  nil,
	})
}

func (h Handler) UploadFile(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "Lấy file thất bại",
			Data:  nil,
		})
		return
	}

	files := form.File["file"]
	if len(files) == 0 {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "Chưa upload file nào",
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
				Msg:   "Lưu file thất bại: " + file.Filename,
				Data:  nil,
			})
			return
		}
		savedFiles = append(savedFiles, "local:"+savePath)
	}

	response.R(c, response.Response{
		Error: 0,
		Msg:   "Upload file thành công",
		Data:  gin.H{"file_path": savedFiles},
	})
}

func (h Handler) DownloadFile(c *gin.Context) {
	requestedFile := c.Param("filepath")
	if requestedFile == "" {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "Đường dẫn file trống",
			Data:  nil,
		})
		return
	}

	localFilePath := filepath.Join(".", requestedFile)
	if _, err := os.Stat(localFilePath); os.IsNotExist(err) {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "File không tồn tại",
			Data:  nil,
		})
		return
	}
	c.FileAttachment(localFilePath, filepath.Base(localFilePath))
}
