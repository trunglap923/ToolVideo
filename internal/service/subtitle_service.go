package service

import (
	"context"
	"errors"
	"encoding/json"
	"fmt"
	"krillin-ai/config"
	"krillin-ai/internal/dto"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/util"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"krillin-ai/internal/service/dubbing"

	"github.com/samber/lo"
	"go.uber.org/zap"
)

func (s Service) StartSubtitleTask(req dto.StartVideoSubtitleTaskReq) (*dto.StartVideoSubtitleTaskResData, error) {
	// 校验链接
	if strings.Contains(req.Url, "youtube.com") {
		videoId, _ := util.GetYouTubeID(req.Url)
		if videoId == "" {
			return nil, fmt.Errorf("链接不合法")
		}
	}
	if strings.Contains(req.Url, "bilibili.com") {
		videoId := util.GetBilibiliVideoId(req.Url)
		if videoId == "" {
			return nil, fmt.Errorf("链接不合法")
		}
	}
	// 生成任务id
	seperates := strings.Split(req.Url, "/")
	taskId := fmt.Sprintf("%s_%s", util.SanitizePathName(string([]rune(strings.ReplaceAll(seperates[len(seperates)-1], " ", ""))[:16])), util.GenerateRandStringWithUpperLowerNum(4))
	taskId = strings.ReplaceAll(taskId, "=", "") // 等于号影响ffmpeg处理
	taskId = strings.ReplaceAll(taskId, "?", "") // 问号影响ffmpeg处理
	// 构造任务所需参数
	var resultType types.SubtitleResultType
	// 根据入参选项确定要返回的字幕类型
	if req.TargetLang == "none" {
		resultType = types.SubtitleResultTypeOriginOnly
	} else {
		if req.Bilingual == types.SubtitleTaskBilingualYes {
			if req.TranslationSubtitlePos == types.SubtitleTaskTranslationSubtitlePosTop {
				resultType = types.SubtitleResultTypeBilingualTranslationOnTop
			} else {
				resultType = types.SubtitleResultTypeBilingualTranslationOnBottom
			}
		} else {
			resultType = types.SubtitleResultTypeTargetOnly
		}
	}
	// 文字替换map
	replaceWordsMap := make(map[string]string)
	if len(req.Replace) > 0 {
		for _, replace := range req.Replace {
			beforeAfter := strings.Split(replace, "|")
			if len(beforeAfter) == 2 {
				replaceWordsMap[beforeAfter[0]] = beforeAfter[1]
			} else {
				log.GetLogger().Info("generateAudioSubtitles replace param length err", zap.Any("replace", replace), zap.Any("taskId", taskId))
			}
		}
	}
	var err error
	ctx, cancel := context.WithCancel(context.Background())
	// 创建字幕任务文件夹
	taskBasePath := filepath.Join("./tasks", taskId)
	if _, err = os.Stat(taskBasePath); os.IsNotExist(err) {
		// 不存在则创建
		err = os.MkdirAll(filepath.Join(taskBasePath, "output"), os.ModePerm)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask MkdirAll err", zap.Any("req", req), zap.Error(err))
		}
	}

	// 创建任务
	taskPtr := &types.SubtitleTask{
		TaskId:   taskId,
		VideoSrc: req.Url,
		Status:   types.SubtitleTaskStatusProcessing,
		Cancel:   cancel,
	}
	storage.SubtitleTasks.Store(taskId, taskPtr)

	// 处理声音克隆源
	var voiceCloneAudioUrl string
	if req.TtsVoiceCloneSrcFileUrl != "" {
		localFileUrl := strings.TrimPrefix(req.TtsVoiceCloneSrcFileUrl, "local:")
		fileKey := util.GenerateRandStringWithUpperLowerNum(5) + filepath.Ext(localFileUrl) // 防止url encode的问题，这里统一处理
		err = s.OssClient.UploadFile(context.Background(), fileKey, localFileUrl, s.OssClient.Bucket)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask UploadFile err", zap.Any("req", req), zap.Error(err))
			return nil, errors.New("上传声音克隆源失败")
		}
		voiceCloneAudioUrl = fmt.Sprintf("https://%s.oss-cn-shanghai.aliyuncs.com/%s", s.OssClient.Bucket, fileKey)
		log.GetLogger().Info("StartVideoSubtitleTask 上传声音克隆源成功", zap.Any("oss url", voiceCloneAudioUrl))
	}

	stepParam := types.SubtitleTaskStepParam{
		TaskId:                  taskId,
		TaskPtr:                 taskPtr,
		TaskBasePath:            taskBasePath,
		Link:                    req.Url,
		SubtitleResultType:      resultType,
		EnableModalFilter:       req.ModalFilter == types.SubtitleTaskModalFilterYes,
		EnableTts:               req.Tts == types.SubtitleTaskTtsYes,
		TtsVoiceCode:            req.TtsVoiceCode,
		VoiceCloneAudioUrl:      voiceCloneAudioUrl,
		ReplaceWordsMap:         replaceWordsMap,
		OriginLanguage:          types.StandardLanguageCode(req.OriginLanguage),
		TargetLanguage:          types.StandardLanguageCode(req.TargetLang),
		UserUILanguage:          types.StandardLanguageCode(req.Language),
		EmbedSubtitleVideoType:  req.EmbedSubtitleVideoType,
		VerticalVideoMajorTitle: req.VerticalMajorTitle,
		VerticalVideoMinorTitle: req.VerticalMinorTitle,
		MaxWordOneLine:          12, // 默认值
		VttSwitch:               req.VttSwitch,
	}
	if req.OriginLanguageWordOneLine != 0 {
		stepParam.MaxWordOneLine = req.OriginLanguageWordOneLine
	}

	log.GetLogger().Info("current task info", zap.String("taskId", taskId), zap.Any("param", stepParam))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				const size = 64 << 10
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]
				log.GetLogger().Error("autoVideoSubtitle panic", zap.Any("panic:", r), zap.Any("stack:", buf))
				stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
			}
		}()
		defer func() {
			if taskPtr.Cancel != nil {
				taskPtr.Cancel()
				taskPtr.Cancel = nil
			}
		}()
		// 新版流程：链接->本地音频文件->视频信息获取（若有）->本地字幕文件->语言合成->视频合成->字幕文件链接生成
		log.GetLogger().Info("video subtitle start task", zap.String("taskId", taskId))
		stepParam.TaskPtr.StatusMsg = "Đang bắt đầu xử lý video..."
		
		err := s.linkToFile(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask linkToFile err", zap.Any("req", req), zap.Error(err))
			stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
			stepParam.TaskPtr.FailReason = err.Error()
			return
		}
		// 暂时不加视频信息
		//err = s.getVideoInfo(ctx, &stepParam)
		//if err != nil {
		//	log.GetLogger().Error("StartVideoSubtitleTask getVideoInfo err", zap.Any("req", req), zap.Error(err))
		//	stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
		//	stepParam.TaskPtr.FailReason = "get video info error"
		//	return
		//}

		// 针对YouTube视频优先尝试使用yt-dlp下载字幕
		if strings.Contains(req.Url, "youtube.com") && stepParam.VttSwitch {
			stepParam.TaskPtr.StatusMsg = "Đang xử lý phụ đề YouTube..."
			log.GetLogger().Info("Start Process youtube video with vtt", zap.String("taskId", taskId))
			req := &YoutubeSubtitleReq{
				TaskBasePath:        stepParam.TaskBasePath,
				TaskId:              taskId,
				OriginLanguage:      string(stepParam.OriginLanguage),
				TargetLanguage:      string(stepParam.TargetLanguage),
				URL:                 req.Url,
				TaskPtr:             stepParam.TaskPtr,
				TargetLanguageFirst: config.Conf.App.TargetLanguageFirst,
			}

			// 先下载VTT字幕
			vttFile, err := s.YouTubeSubtitleSrv.downloadYouTubeSubtitle(ctx, req)
			if err != nil {
				// 下载失败，回退到音频转录方式
				log.GetLogger().Warn("Failed to download YouTube subtitles, falling back to audio transcription",
					zap.String("taskId", taskId), zap.Error(err))
				stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
				stepParam.TaskPtr.FailReason = err.Error()
				return
			}
			req.VttFile = vttFile

			// 检测VTT格式类型
			hasWordTimestamps := true // 默认假设有单词级时间戳
			if config.Conf.App.EnableBlockVttBatch {
				// 只有启用了新功能才进行格式检测
				detected, detectErr := s.YouTubeSubtitleSrv.DetectVttFormat(vttFile)
				if detectErr != nil {
					log.GetLogger().Warn("VTT格式检测失败，使用默认处理方式",
						zap.String("taskId", taskId), zap.Error(detectErr))
				} else {
					hasWordTimestamps = detected
				}
			}

			var srtFile string
			if hasWordTimestamps {
				// 使用原有的word-level处理流程（完全不变）
				log.GetLogger().Info("使用word-level VTT处理流程", zap.String("taskId", taskId))
				srtFile, err = s.YouTubeSubtitleSrv.processYouTubeSubtitle(ctx, req)
			} else {
				// 使用新的block-level处理流程
				log.GetLogger().Info("使用block-level VTT处理流程", zap.String("taskId", taskId))
				srtFile, err = s.YouTubeSubtitleSrv.ProcessBlockLevelVtt(ctx, req)
			}

			if err != nil {
				// 处理字幕失败，回退到音频转录方式
				log.GetLogger().Warn("Failed to process YouTube subtitles, falling back to audio transcription",
					zap.String("taskId", taskId), zap.Error(err))
				stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
				stepParam.TaskPtr.FailReason = err.Error()
				return
			}

			stepParam.BilingualSrtFilePath = srtFile
			err = splitSrt(&stepParam)
			if err != nil {
				return
			}
			stepParam.TaskPtr.ProcessPct = 95
		} else {
			stepParam.TaskPtr.StatusMsg = "Đang trích xuất âm thanh và tạo phụ đề (STT)..."
			// 非YouTube视频，使用原来的音频转录流程
			err = s.audioToSubtitle(ctx, &stepParam)
			if err != nil {
				log.GetLogger().Error("StartVideoSubtitleTask audioToSubtitle err", zap.Any("req", req), zap.Error(err))
				stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
				stepParam.TaskPtr.FailReason = err.Error()
				return
			}
		}
		
		// Bỏ qua tạo lồng tiếng và kết xuất âm thanh ở giai đoạn này
		// stepParam.TaskPtr.StatusMsg = "Đang tạo giọng nói nhân tạo (TTS)..."
		// err = s.srtFileToSpeech(ctx, &stepParam)
		
		// [Theo yêu cầu của User: Không nhúng phụ đề cứng vào video trước khi chỉnh sửa]
		// Chỉ lưu SRT và cho phép xem trực tiếp trên trình duyệt
		stepParam.TaskPtr.StatusMsg = "Đã tạo xong phụ đề. Vui lòng mở Web Studio để chỉnh sửa!"
		
		stepParam.TaskPtr.StatusMsg = "Đang hoàn tất quá trình..."
		err = s.uploadSubtitles(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask uploadSubtitles err", zap.Any("req", req), zap.Error(err))
			stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
			stepParam.TaskPtr.FailReason = err.Error()
			return
		}

		// Save final StepParam to config.json so we capture AudioFilePath and InputVideoPath
		if finalBytes, err := json.Marshal(stepParam); err == nil {
			os.WriteFile(filepath.Join(stepParam.TaskBasePath, "config.json"), finalBytes, 0644)
		}

		log.GetLogger().Info("video subtitle task end", zap.String("taskId", taskId))
	}()

	return &dto.StartVideoSubtitleTaskResData{
		TaskId: taskId,
	}, nil
}

func (s Service) GetTaskStatus(req dto.GetVideoSubtitleTaskReq) (*dto.GetVideoSubtitleTaskResData, error) {
	task, ok := storage.SubtitleTasks.Load(req.TaskId)
	if !ok || task == nil {
		return nil, errors.New("任务不存在")
	}
	taskPtr := task.(*types.SubtitleTask)
	if taskPtr.Status == types.SubtitleTaskStatusFailed {
		return nil, fmt.Errorf("任务失败，原因：%s", taskPtr.FailReason)
	}
	return &dto.GetVideoSubtitleTaskResData{
		TaskId:         taskPtr.TaskId,
		ProcessPercent: taskPtr.ProcessPct,
		StatusMsg:      taskPtr.StatusMsg,
		VideoInfo: &dto.VideoInfo{
			Title:                 taskPtr.Title,
			Description:           taskPtr.Description,
			TranslatedTitle:       taskPtr.TranslatedTitle,
			TranslatedDescription: taskPtr.TranslatedDescription,
		},
		SubtitleInfo: lo.Map(taskPtr.SubtitleInfos, func(item types.SubtitleInfo, _ int) *dto.SubtitleInfo {
			return &dto.SubtitleInfo{
				Name:        item.Name,
				DownloadUrl: item.DownloadUrl,
			}
		}),
		TargetLanguage:    taskPtr.TargetLanguage,
		SpeechDownloadUrl: taskPtr.SpeechDownloadUrl,
	}, nil
}

func (s Service) CancelTask(req dto.CancelVideoSubtitleTaskReq) error {
	task, ok := storage.SubtitleTasks.Load(req.TaskId)
	if !ok || task == nil {
		return errors.New("Không tìm thấy tác vụ (Task not found)")
	}
	taskPtr := task.(*types.SubtitleTask)
	if taskPtr.Status == types.SubtitleTaskStatusProcessing {
		if taskPtr.Cancel != nil {
			taskPtr.Cancel()
			taskPtr.Cancel = nil
			taskPtr.Status = types.SubtitleTaskStatusFailed
			taskPtr.FailReason = "Bị huỷ bởi người dùng (Canceled by user)"
			taskPtr.StatusMsg = "Đã huỷ tiến trình."
			log.GetLogger().Info("Task canceled successfully", zap.String("taskId", req.TaskId))
			return nil
		}
	}
	return errors.New("Tác vụ không thể huỷ hoặc đã hoàn thành")
}

func (s Service) GetTaskSubtitles(req dto.GetVideoSubtitleTaskReq) (*dto.GetTaskSubtitlesResData, error) {
	task, ok := storage.SubtitleTasks.Load(req.TaskId)
	if !ok || task == nil {
		// Try to see if directory exists before failing
		baseDir := filepath.Join("tasks", req.TaskId)
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			return nil, errors.New("Không tìm thấy tác vụ (Task not found)")
		}
	}
	
	baseDir := filepath.Join("tasks", req.TaskId)
	
	srtPath := filepath.Join(baseDir, "bilingual_srt.srt")
	if _, err := os.Stat(srtPath); os.IsNotExist(err) {
		srtPath = filepath.Join(baseDir, "target_language_srt.srt")
		if _, err := os.Stat(srtPath); os.IsNotExist(err) {
			srtPath = filepath.Join(baseDir, "origin_language_srt.srt")
			if _, err := os.Stat(srtPath); os.IsNotExist(err) {
				return nil, errors.New("Không tìm thấy file phụ đề")
			}
		}
	}

	cues, err := dubbing.ParseSRTFile(srtPath)
	if err != nil {
		return nil, fmt.Errorf("Lỗi đọc file phụ đề: %v", err)
	}

	subtitles := make([]dto.SubtitleItem, len(cues))
	for i, c := range cues {
		subtitles[i] = dto.SubtitleItem{
			Index: c.Index,
			Start: c.Start,
			End:   c.End,
			Text:  c.Text,
		}
	}

	// Read config to get original video path, blur regions, and subtitle overlay
	videoUrl := ""
	var blurRegions []types.BlurRegion
	var subtitleOverlay *types.OverlayConfig

	configPath := filepath.Join(baseDir, "config.json")
	if configBytes, err := os.ReadFile(configPath); err == nil {
		var stepParam types.SubtitleTaskStepParam
		if json.Unmarshal(configBytes, &stepParam) == nil {
			blurRegions = stepParam.BlurRegions
			subtitleOverlay = stepParam.SubtitleOverlay

			videoPath := stepParam.InputVideoPath
			if videoPath == "" {
				if strings.HasPrefix(stepParam.Link, "local:") {
					videoPath = strings.ReplaceAll(stepParam.Link, "local:", "")
				} else {
					videoPath = filepath.Join("tasks", req.TaskId, types.SubtitleTaskVideoFileName)
				}
			}
			cleanedPath := strings.TrimPrefix(videoPath, "./")
			cleanedPath = strings.ReplaceAll(cleanedPath, "\\", "/")
			
			// Encode each segment to handle special characters like #, ?, &, spaces
			segments := strings.Split(cleanedPath, "/")
			for i, seg := range segments {
				segments[i] = url.PathEscape(seg)
			}
			encodedPath := strings.Join(segments, "/")
			videoUrl = "/api/file/" + encodedPath
		}
	}

	return &dto.GetTaskSubtitlesResData{
		TaskId:          req.TaskId,
		Subtitles:       subtitles,
		VideoUrl:        videoUrl,
		BlurRegions:     blurRegions,
		SubtitleOverlay: subtitleOverlay,
	}, nil
}

func (s Service) UpdateTaskSubtitles(req dto.UpdateTaskSubtitlesReq) error {
	task, ok := storage.SubtitleTasks.Load(req.TaskId)
	if !ok || task == nil {
		baseDir := filepath.Join("tasks", req.TaskId)
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			return errors.New("Không tìm thấy tác vụ (Task not found)")
		}
	}

	baseDir := filepath.Join("tasks", req.TaskId)
	srtPath := filepath.Join(baseDir, "bilingual_srt.srt")
	if _, err := os.Stat(srtPath); os.IsNotExist(err) {
		srtPath = filepath.Join(baseDir, "target_language_srt.srt")
		if _, err := os.Stat(srtPath); os.IsNotExist(err) {
			srtPath = filepath.Join(baseDir, "origin_language_srt.srt")
		}
	}

	// Convert req.Subtitles to dubbing.Cue
	var cues []dubbing.Cue
	for _, sub := range req.Subtitles {
		cues = append(cues, dubbing.Cue{
			Index: sub.Index,
			Start: sub.Start,
			End:   sub.End,
			Text:  sub.Text,
		})
	}

	err := dubbing.WriteSRTFile(srtPath, cues)
	if err != nil {
		return fmt.Errorf("Lỗi lưu file phụ đề: %v", err)
	}

	// Load config.json and update BlurRegions and SubtitleOverlay
	configPath := filepath.Join(baseDir, "config.json")
	if configBytes, err := os.ReadFile(configPath); err == nil {
		var stepParam types.SubtitleTaskStepParam
		if err := json.Unmarshal(configBytes, &stepParam); err == nil {
			stepParam.BlurRegions = req.BlurRegions
			stepParam.SubtitleOverlay = req.SubtitleOverlay
			
			if updatedBytes, err := json.MarshalIndent(stepParam, "", "  "); err == nil {
				_ = os.WriteFile(configPath, updatedBytes, 0644)
			}
		}
	}

	return nil
}

func (s Service) ExportVideoTask(ctx context.Context, req dto.ExportVideoTaskReq) error {
	task, ok := storage.SubtitleTasks.Load(req.TaskId)
	var taskPtr *types.SubtitleTask
	if !ok || task == nil {
		// Recreate task in memory so polling works
		taskPtr = &types.SubtitleTask{
			TaskId: req.TaskId,
		}
		storage.SubtitleTasks.Store(req.TaskId, taskPtr)
	} else {
		taskPtr = task.(*types.SubtitleTask)
	}

	configPath := filepath.Join("tasks", req.TaskId, "config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("Không tìm thấy cấu hình cũ của tác vụ: %v", err)
	}

	var stepParam types.SubtitleTaskStepParam
	if err := json.Unmarshal(configBytes, &stepParam); err != nil {
		return fmt.Errorf("Lỗi đọc cấu hình: %v", err)
	}
	
	// Khôi phục con trỏ taskPtr
	stepParam.TaskPtr = taskPtr

	// Bắt đầu tiến trình Export
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.GetLogger().Error("ExportVideoTask panic", zap.Any("panic:", r))
				taskPtr.Status = types.SubtitleTaskStatusFailed
				taskPtr.FailReason = "Lỗi nghiêm trọng khi xuất video"
			}
		}()

		taskPtr.Status = types.SubtitleTaskStatusProcessing
		taskPtr.ProcessPct = 0

		if req.EnableTts {
			taskPtr.StatusMsg = "Đang tạo giọng nói nhân tạo (TTS)..."
			stepParam.EnableTts = true
			if err := s.srtFileToSpeech(context.Background(), &stepParam); err != nil {
				log.GetLogger().Error("ExportVideoTask srtFileToSpeech err", zap.Error(err))
				taskPtr.Status = types.SubtitleTaskStatusFailed
				taskPtr.FailReason = "Lỗi tạo TTS: " + err.Error()
				return
			}
		} else {
			stepParam.EnableTts = false
		}

		taskPtr.StatusMsg = "Đang ghép và kết xuất video cuối cùng..."
		taskPtr.ProcessPct = 50
		if err := s.embedSubtitles(context.Background(), &stepParam); err != nil {
			log.GetLogger().Error("ExportVideoTask embedSubtitles err", zap.Error(err))
			taskPtr.Status = types.SubtitleTaskStatusFailed
			taskPtr.FailReason = "Lỗi ghép video: " + err.Error()
			return
		}

		taskPtr.StatusMsg = "Hoàn tất xuất video!"
		taskPtr.ProcessPct = 100
		taskPtr.Status = types.SubtitleTaskStatusSuccess
		
		// Upload again if needed, or just let it finish
		s.uploadSubtitles(context.Background(), &stepParam)
	}()

	return nil
}

func (s Service) RunWhisperTask(req dto.RunWhisperTaskReq) error {
	task, ok := storage.SubtitleTasks.Load(req.TaskId)
	var taskPtr *types.SubtitleTask
	if !ok || task == nil {
		baseDir := filepath.Join("tasks", req.TaskId)
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			return errors.New("Không tìm thấy tác vụ (Task not found)")
		}
		taskPtr = &types.SubtitleTask{
			TaskId: req.TaskId,
		}
		storage.SubtitleTasks.Store(req.TaskId, taskPtr)
	} else {
		taskPtr = task.(*types.SubtitleTask)
	}

	baseDir := filepath.Join("tasks", req.TaskId)
	configPath := filepath.Join(baseDir, "config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("Không thể đọc config task: %v", err)
	}

	var stepParam types.SubtitleTaskStepParam
	if err := json.Unmarshal(configBytes, &stepParam); err != nil {
		return fmt.Errorf("Lỗi parse config task: %v", err)
	}

	stepParam.OriginLanguage = types.StandardLanguageCode(req.OriginLanguage)
	stepParam.TargetLanguage = "none" // Force none to avoid translation
	stepParam.SubtitleResultType = types.SubtitleResultTypeOriginOnly

	// Update the config file
	if updatedBytes, err := json.MarshalIndent(stepParam, "", "  "); err == nil {
		_ = os.WriteFile(configPath, updatedBytes, 0644)
	}

	ctx, cancel := context.WithCancel(context.Background())
	taskPtr.Cancel = cancel
	taskPtr.Status = types.SubtitleTaskStatusProcessing
	taskPtr.ProcessPct = 0

	go func() {
		defer func() {
			if r := recover(); r != nil {
				taskPtr.Status = types.SubtitleTaskStatusFailed
				taskPtr.FailReason = fmt.Sprintf("Panic: %v", r)
			}
		}()
		defer func() {
			if taskPtr.Cancel != nil {
				taskPtr.Cancel()
				taskPtr.Cancel = nil
			}
		}()

		taskPtr.StatusMsg = "Đang trích xuất âm thanh và tạo phụ đề (STT)..."
		
		err = s.audioToSubtitle(ctx, &stepParam)
		if err != nil {
			taskPtr.Status = types.SubtitleTaskStatusFailed
			taskPtr.FailReason = "Lỗi trích xuất phụ đề: " + err.Error()
			return
		}

		taskPtr.ProcessPct = 100
		taskPtr.StatusMsg = "Trích xuất phụ đề hoàn tất"
	}()

	return nil
}

func (s Service) RunTranslateTask(req dto.RunTranslateTaskReq) error {
	task, ok := storage.SubtitleTasks.Load(req.TaskId)
	var taskPtr *types.SubtitleTask
	if !ok || task == nil {
		baseDir := filepath.Join("tasks", req.TaskId)
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			return errors.New("Không tìm thấy tác vụ (Task not found)")
		}
		taskPtr = &types.SubtitleTask{
			TaskId: req.TaskId,
		}
		storage.SubtitleTasks.Store(req.TaskId, taskPtr)
	} else {
		taskPtr = task.(*types.SubtitleTask)
	}

	baseDir := filepath.Join("tasks", req.TaskId)
	configPath := filepath.Join(baseDir, "config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("Không thể đọc config task: %v", err)
	}

	var stepParam types.SubtitleTaskStepParam
	if err := json.Unmarshal(configBytes, &stepParam); err != nil {
		return fmt.Errorf("Lỗi parse config task: %v", err)
	}

	stepParam.TargetLanguage = types.StandardLanguageCode(req.TargetLang)
	stepParam.EnableModalFilter = req.ModalFilter == types.SubtitleTaskModalFilterYes
	
	var resultType types.SubtitleResultType
	if req.TargetLang == "none" {
		resultType = types.SubtitleResultTypeOriginOnly
	} else {
		if req.Bilingual == types.SubtitleTaskBilingualYes {
			if req.TranslationSubtitlePos == types.SubtitleTaskTranslationSubtitlePosTop {
				resultType = types.SubtitleResultTypeBilingualTranslationOnTop
			} else {
				resultType = types.SubtitleResultTypeBilingualTranslationOnBottom
			}
		} else {
			resultType = types.SubtitleResultTypeTargetOnly
		}
	}
	stepParam.SubtitleResultType = resultType

	// Update the config file
	if updatedBytes, err := json.MarshalIndent(stepParam, "", "  "); err == nil {
		_ = os.WriteFile(configPath, updatedBytes, 0644)
	}

	ctx, cancel := context.WithCancel(context.Background())
	taskPtr.Cancel = cancel
	taskPtr.Status = types.SubtitleTaskStatusProcessing
	taskPtr.ProcessPct = 0

	go func() {
		defer func() {
			if r := recover(); r != nil {
				taskPtr.Status = types.SubtitleTaskStatusFailed
				taskPtr.FailReason = fmt.Sprintf("Panic: %v", r)
			}
		}()
		defer func() {
			if taskPtr.Cancel != nil {
				taskPtr.Cancel()
				taskPtr.Cancel = nil
			}
		}()

		taskPtr.StatusMsg = "Đang dịch phụ đề..."
		
		if req.TargetLang != "none" {
			originSrtPath := filepath.Join(baseDir, "origin_language_srt.srt")
			
			cues, err := dubbing.ParseSRTFile(originSrtPath)
			if err != nil {
				taskPtr.Status = types.SubtitleTaskStatusFailed
				taskPtr.FailReason = "Lỗi đọc file phụ đề gốc: " + err.Error()
				return
			}
			
			var srtBlocks []*util.SrtBlock
			for _, cue := range cues {
				srtBlocks = append(srtBlocks, &util.SrtBlock{
					Index:                  cue.Index,
					Timestamp:              fmt.Sprintf("%s --> %s", dubbing.FormatTimestamp(cue.Start), dubbing.FormatTimestamp(cue.End)),
					OriginLanguageSentence: cue.Text,
				})
			}

			translator := NewTranslator()
			err = translator.BatchTranslateSrtBlocks(srtBlocks, string(stepParam.OriginLanguage), req.TargetLang, taskPtr)
			if err != nil {
				taskPtr.Status = types.SubtitleTaskStatusFailed
				taskPtr.FailReason = "Lỗi dịch thuật: " + err.Error()
				return
			}

			targetSrtFile := filepath.Join(baseDir, types.SubtitleTaskTargetLanguageSrtFileName)
			bilingualSrtFile := filepath.Join(baseDir, types.SubtitleTaskBilingualSrtFileName)
			
			s.YouTubeSubtitleSrv.writeTargetLanguageSrtFile(srtBlocks, targetSrtFile)
			s.YouTubeSubtitleSrv.writeBilingualSrtFile(srtBlocks, bilingualSrtFile, config.Conf.App.TargetLanguageFirst)
			
			stepParam.BilingualSrtFilePath = bilingualSrtFile
		} else {
			stepParam.BilingualSrtFilePath = filepath.Join(baseDir, "origin_language_srt.srt")
		}

		if finalBytes, err := json.MarshalIndent(stepParam, "", "  "); err == nil {
			_ = os.WriteFile(configPath, finalBytes, 0644)
		}

		err = s.uploadSubtitles(ctx, &stepParam)
		if err != nil {
			taskPtr.Status = types.SubtitleTaskStatusFailed
			taskPtr.FailReason = "Lỗi cập nhật subtitles: " + err.Error()
			return
		}

		taskPtr.ProcessPct = 100
		taskPtr.StatusMsg = "Dịch phụ đề hoàn tất"
	}()

	return nil
}

func (s Service) RunTtsOnlyTask(req dto.RunTtsOnlyTaskReq) error {
	task, ok := storage.SubtitleTasks.Load(req.TaskId)
	var taskPtr *types.SubtitleTask
	if !ok || task == nil {
		baseDir := filepath.Join("tasks", req.TaskId)
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			return errors.New("Không tìm thấy tác vụ (Task not found)")
		}
		taskPtr = &types.SubtitleTask{
			TaskId: req.TaskId,
		}
		storage.SubtitleTasks.Store(req.TaskId, taskPtr)
	} else {
		taskPtr = task.(*types.SubtitleTask)
	}

	baseDir := filepath.Join("tasks", req.TaskId)
	configPath := filepath.Join(baseDir, "config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("Không thể đọc config task: %v", err)
	}

	var stepParam types.SubtitleTaskStepParam
	if err := json.Unmarshal(configBytes, &stepParam); err != nil {
		return fmt.Errorf("Lỗi parse config task: %v", err)
	}

	stepParam.TtsVoiceCode = req.TtsVoiceCode
	if req.TtsVoiceCloneSrcFileUrl != "" {
		stepParam.VoiceCloneAudioUrl = req.TtsVoiceCloneSrcFileUrl
	}
	stepParam.EnableTts = true

	// Update the config file
	if updatedBytes, err := json.MarshalIndent(stepParam, "", "  "); err == nil {
		_ = os.WriteFile(configPath, updatedBytes, 0644)
	}

	ctx, cancel := context.WithCancel(context.Background())
	taskPtr.Cancel = cancel
	taskPtr.Status = types.SubtitleTaskStatusProcessing
	taskPtr.ProcessPct = 0

	go func() {
		defer func() {
			if r := recover(); r != nil {
				taskPtr.Status = types.SubtitleTaskStatusFailed
				taskPtr.FailReason = fmt.Sprintf("Panic: %v", r)
			}
		}()
		defer func() {
			if taskPtr.Cancel != nil {
				taskPtr.Cancel()
				taskPtr.Cancel = nil
			}
		}()

		taskPtr.StatusMsg = "Đang tạo giọng nói nhân tạo (TTS)..."
		
		err = s.srtFileToSpeech(ctx, &stepParam)
		if err != nil {
			taskPtr.Status = types.SubtitleTaskStatusFailed
			taskPtr.FailReason = "Lỗi tạo TTS: " + err.Error()
			return
		}

		err = s.uploadSubtitles(ctx, &stepParam)
		if err != nil {
			taskPtr.Status = types.SubtitleTaskStatusFailed
			taskPtr.FailReason = "Lỗi cập nhật âm thanh lên UI: " + err.Error()
			return
		}

		taskPtr.ProcessPct = 100
		taskPtr.StatusMsg = "Lồng tiếng hoàn tất"
	}()

	return nil
}
