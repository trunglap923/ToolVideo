package dto

import "krillin-ai/internal/types"

type StartVideoSubtitleTaskReq struct {
	AppId                     uint32   `json:"app_id"`
	Url                       string   `json:"url"`
	OriginLanguage            string   `json:"origin_lang"`
	TargetLang                string   `json:"target_lang"`
	Bilingual                 uint8    `json:"bilingual"`
	TranslationSubtitlePos    uint8    `json:"translation_subtitle_pos"`
	ModalFilter               uint8    `json:"modal_filter"`
	Tts                       uint8    `json:"tts"`
	TtsVoiceCode              string   `json:"tts_voice_code"`
	TtsVoiceCloneSrcFileUrl   string   `json:"tts_voice_clone_src_file_url"`
	Replace                   []string `json:"replace"`
	Language                  string   `json:"language"`
	EmbedSubtitleVideoType    string   `json:"embed_subtitle_video_type"`
	VerticalMajorTitle        string   `json:"vertical_major_title"`
	VerticalMinorTitle        string   `json:"vertical_minor_title"`
	OriginLanguageWordOneLine int      `json:"origin_language_word_one_line"`
	VttSwitch                 bool     `json:"vtt_switch"` // 是否使用VTT格式字幕文件
}

type StartVideoSubtitleTaskResData struct {
	TaskId string `json:"task_id"`
}

type StartVideoSubtitleTaskRes struct {
	Error int32                          `json:"error"`
	Msg   string                         `json:"msg"`
	Data  *StartVideoSubtitleTaskResData `json:"data"`
}

type GetVideoSubtitleTaskReq struct {
	TaskId string `form:"taskId"`
}

type VideoInfo struct {
	Title                 string `json:"title"`
	Description           string `json:"description"`
	TranslatedTitle       string `json:"translated_title"`
	TranslatedDescription string `json:"translated_description"`
	Language              string `json:"language"`
}

type SubtitleInfo struct {
	Name        string `json:"name"`
	DownloadUrl string `json:"download_url"`
}

type GetVideoSubtitleTaskResData struct {
	TaskId            string          `json:"task_id"`
	ProcessPercent    uint8           `json:"process_percent"`
	StatusMsg         string          `json:"status_msg"`
	VideoInfo         *VideoInfo      `json:"video_info"`
	SubtitleInfo      []*SubtitleInfo `json:"subtitle_info"`
	TargetLanguage    string          `json:"target_language"`
	SpeechDownloadUrl string          `json:"speech_download_url"`
}

type GetVideoSubtitleTaskRes struct {
	Error int32                        `json:"error"`
	Msg   string                       `json:"msg"`
	Data  *GetVideoSubtitleTaskResData `json:"data"`
}

type CancelVideoSubtitleTaskReq struct {
	TaskId string `json:"task_id"`
}

type CancelVideoSubtitleTaskRes struct {
	Error int32  `json:"error"`
	Msg   string `json:"msg"`
}

type SubtitleItem struct {
	Index int     `json:"index"`
	Start float64 `json:"start"`
	End             float64 `json:"end"`
	Text            string  `json:"text"`
	RawAudioUrl     string  `json:"raw_audio_url,omitempty"`
	RawAudioDuration float64 `json:"raw_audio_duration,omitempty"`
}

type GetTaskSubtitlesResData struct {
	TaskId          string               `json:"task_id"`
	Subtitles       []SubtitleItem       `json:"subtitles"`
	VideoUrl        string               `json:"video_url"`
	SpeechUrl       string               `json:"speech_url"`
	BlurRegions     []types.BlurRegion   `json:"blur_regions"`
	SubtitleOverlay *types.OverlayConfig `json:"subtitle_overlay"`
}

type UpdateTaskSubtitlesReq struct {
	TaskId          string               `json:"task_id"`
	Subtitles       []SubtitleItem       `json:"subtitles"`
	BlurRegions     []types.BlurRegion   `json:"blur_regions"`
	SubtitleOverlay *types.OverlayConfig `json:"subtitle_overlay"`
}

type RunWhisperTaskReq struct {
	TaskId         string `json:"task_id"`
	OriginLanguage string `json:"origin_lang"`
}

type RunTranslateTaskReq struct {
	TaskId                 string `json:"task_id"`
	TargetLang             string `json:"target_lang"`
	Bilingual              uint8  `json:"bilingual"`
	TranslationSubtitlePos uint8  `json:"translation_subtitle_pos"`
	ModalFilter            uint8  `json:"modal_filter"`
}

type RunTtsOnlyTaskReq struct {
	TaskId                  string `json:"task_id"`
	TtsVoiceCode            string `json:"tts_voice_code"`
	TtsVoiceCloneSrcFileUrl string `json:"tts_voice_clone_src_file_url"`
}

type ExportVideoTaskReq struct {
	TaskId    string `json:"task_id"`
	EnableTts bool   `json:"enable_tts"`
}

type ExportVideoTaskRes struct {
	Error int32  `json:"error"`
	Msg   string `json:"msg"`
}
