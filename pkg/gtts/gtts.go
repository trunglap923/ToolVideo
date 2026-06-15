package gtts

import (
	"context"
	"fmt"
	"krillin-ai/internal/storage"
	"krillin-ai/log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

type GTtsClient struct{}

func NewGTtsClient() *GTtsClient {
	return &GTtsClient{}
}

// Text2Speech converts text to speech using gTTS CLI and saves as WAV (via MP3 -> WAV conversion with ffmpeg).
func (c *GTtsClient) Text2Speech(text, voice, outputFile string) error {
	// Normalize voice code: gTTS only accepts 2-letter lang codes like "vi", "en", "zh"
	// If given an edge-tts style code like "vi-VN-HoaiMyNeural", extract just "vi"
	lang := normalizeLangCode(strings.TrimSpace(voice))

	// Ensure output directory exists
	outputDir := filepath.Dir(outputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	absOutputFile, err := filepath.Abs(outputFile)
	if err != nil {
		return fmt.Errorf("failed to resolve output path: %w", err)
	}

	// gTTS outputs MP3, so we write to a temp mp3 first, then convert to WAV
	mp3File := strings.TrimSuffix(absOutputFile, filepath.Ext(absOutputFile)) + "_gtts_tmp.mp3"
	defer os.Remove(mp3File)

	// Create a temp file for text input to handle special characters
	tmpTextFile, err := os.CreateTemp(filepath.Dir(absOutputFile), "gtts_text_*.txt")
	if err != nil {
		return fmt.Errorf("failed to create temp text file: %w", err)
	}
	tmpTextPath := tmpTextFile.Name()
	defer os.Remove(tmpTextPath)

	if _, err := tmpTextFile.WriteString(text); err != nil {
		tmpTextFile.Close()
		return fmt.Errorf("failed to write text to temp file: %w", err)
	}
	tmpTextFile.Close()

	maxRetries := 3
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.GetLogger().Info("gTTS attempt",
			zap.Int("attempt", attempt),
			zap.String("lang", lang),
			zap.Int("text_len", len(text)))

		os.Remove(mp3File)

		lastErr = c.attemptGTTS(tmpTextPath, lang, mp3File, attempt)
		if lastErr != nil {
			log.GetLogger().Warn("gTTS attempt failed", zap.Int("attempt", attempt), zap.Error(lastErr))
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
			}
			continue
		}

		// Convert MP3 -> WAV using ffmpeg
		if err := c.convertMp3ToWav(mp3File, absOutputFile); err != nil {
			lastErr = fmt.Errorf("ffmpeg mp3->wav conversion failed: %w", err)
			log.GetLogger().Warn("gTTS mp3->wav failed", zap.Int("attempt", attempt), zap.Error(err))
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
			}
			continue
		}

		log.GetLogger().Info("gTTS success", zap.String("output", absOutputFile))
		return nil
	}

	return fmt.Errorf("gTTS failed after %d retries: %w", maxRetries, lastErr)
}

func (c *GTtsClient) attemptGTTS(textFile, lang, mp3Output string, attempt int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// gtts-cli reads from stdin or file; use --file flag with the text file
	cmd := exec.CommandContext(ctx, storage.GttsBinPath,
		"--file", textFile,
		"--lang", lang,
		"--output", mp3Output,
	)

	log.GetLogger().Info("gTTS command", zap.String("cmd", cmd.String()), zap.Int("attempt", attempt))

	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("gTTS timeout")
		}
		return fmt.Errorf("gTTS exit error: %w, output: %s", err, string(out))
	}
	return nil
}

func (c *GTtsClient) convertMp3ToWav(mp3File, wavFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ffmpegPath := storage.FfmpegPath
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y", "-i", mp3File,
		"-ar", "44100",
		"-ac", "1",
		wavFile,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w, output: %s", err, string(out))
	}
	return nil
}

// normalizeLangCode converts edge-tts style codes (e.g. "vi-VN-HoaiMyNeural") into
// gTTS-compatible 2-letter codes (e.g. "vi"). If already short, returns as-is.
func normalizeLangCode(voice string) string {
	if voice == "" {
		return "vi" // default
	}
	// edge-tts format: "vi-VN-HoaiMyNeural" -> take first segment "vi"
	parts := strings.SplitN(voice, "-", 2)
	lang := strings.ToLower(parts[0])
	if lang == "" {
		return "vi"
	}
	// Map common aliases
	switch lang {
	case "zh":
		return "zh-TW" // gTTS uses zh-TW for Traditional, zh-CN for Simplified
	}
	return lang
}
