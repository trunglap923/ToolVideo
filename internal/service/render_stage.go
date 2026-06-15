package service

import (
	"context"
	"fmt"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RenderVideoRequest struct {
	Workdir      string
	InputVideo   string
	SubtitleFile string
	OutputFile   string
	Horizontal   bool
	StepParam    *types.SubtitleTaskStepParam
}

func (s Service) RenderVideo(ctx context.Context, req RenderVideoRequest) (string, error) {
	return renderSubtitleFile(ctx, req)
}

func renderAssPath(req RenderVideoRequest) string {
	base := strings.TrimSuffix(filepath.Base(req.OutputFile), filepath.Ext(req.OutputFile))
	if base == "" || base == "." {
		base = "subtitles"
	}
	return filepath.Join(req.Workdir, fmt.Sprintf("formatted_%s.ass", base))
}

func escapeAssFilterPath(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	p = strings.ReplaceAll(p, ":", `\:`)
	return p
}

func buildEmbedSubtitleArgs(req RenderVideoRequest, videoWidth, videoHeight int) ([]string, string) {
	assPath := renderAssPath(req)
	ass := escapeAssFilterPath(assPath)

	var filters []string

	// Apply delogo (blur) regions if any
	if req.StepParam != nil && req.StepParam.BlurRegions != nil {
		for _, region := range req.StepParam.BlurRegions {
			// Convert percentage (0.0 - 1.0) to actual pixels
			x := int(region.X * float64(videoWidth))
			y := int(region.Y * float64(videoHeight))
			w := int(region.Width * float64(videoWidth))
			h := int(region.Height * float64(videoHeight))

			// Clamp values to prevent ffmpeg crashes
			if x < 0 { x = 0 }
			if y < 0 { y = 0 }
			if w <= 0 { w = 10 }
			if h <= 0 { h = 10 }
			if x+w > videoWidth { w = videoWidth - x }
			if y+h > videoHeight { h = videoHeight - y }

			filter := fmt.Sprintf("delogo=x=%d:y=%d:w=%d:h=%d", x, y, w, h)
			if region.Start > 0 || region.End > 0 {
				if region.End == 0 {
					region.End = 999999 // a very large number
				}
				filter += fmt.Sprintf(":enable='between(t,%f,%f)'", region.Start, region.End)
			}
			filters = append(filters, filter)
		}
	}

	// Finally, add the ASS subtitle filter
	filters = append(filters, fmt.Sprintf("ass=%s", ass))

	filterGraph := strings.Join(filters, ",")

	return []string{
		"-y",
		"-i", req.InputVideo,
		"-vf", filterGraph,
		"-c:a", "aac",
		"-b:a", "192k",
		req.OutputFile,
	}, assPath
}

func renderSubtitleFile(ctx context.Context, req RenderVideoRequest) (string, error) {
	if err := os.MkdirAll(filepath.Dir(req.OutputFile), 0755); err != nil {
		return "", fmt.Errorf("renderSubtitleFile mkdir output dir error: %w", err)
	}

	assPath := renderAssPath(req)
	stepParam := req.StepParam
	if stepParam == nil {
		stepParam = &types.SubtitleTaskStepParam{TaskBasePath: req.Workdir}
		req.StepParam = stepParam
	}
	if err := srtToAss(req.SubtitleFile, assPath, req.Horizontal, stepParam); err != nil {
		return "", fmt.Errorf("renderSubtitleFile srtToAss error: %w", err)
	}
	width, height, err := getResolution(req.InputVideo)
	if err != nil {
		return "", fmt.Errorf("renderSubtitleFile getResolution error: %w", err)
	}

	if !req.Horizontal {
		inputVideo, err := prepareRenderVideoInput(req, width, height, convertToVertical)
		if err != nil {
			return "", fmt.Errorf("renderSubtitleFile prepare vertical input error: %w", err)
		}
		req.InputVideo = inputVideo
		width, height, err = getResolution(req.InputVideo)
		if err != nil {
			return "", fmt.Errorf("renderSubtitleFile getResolution post-convert error: %w", err)
		}
	}
	args, _ := buildEmbedSubtitleArgs(req, width, height)
	cmd := exec.CommandContext(ctx, storage.FfmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("renderSubtitleFile ffmpeg error: %w, output: %s", err, string(output))
	}
	return req.OutputFile, nil
}

type verticalConverter func(inputVideo, outputVideo, majorTitle, minorTitle string) error

func prepareRenderVideoInput(req RenderVideoRequest, width, height int, convert verticalConverter) (string, error) {
	if req.Horizontal || width <= height {
		return req.InputVideo, nil
	}
	majorTitle, minorTitle := "", ""
	if req.StepParam != nil {
		majorTitle = req.StepParam.VerticalVideoMajorTitle
		minorTitle = req.StepParam.VerticalVideoMinorTitle
	}
	output := filepath.Join(req.Workdir, types.SubtitleTaskTransferredVerticalVideoFileName)
	if err := convert(req.InputVideo, output, majorTitle, minorTitle); err != nil {
		return "", err
	}
	if req.StepParam != nil {
		req.StepParam.InputVideoPath = output
	}
	return output, nil
}
