package dubbing

import (
	"context"
	"errors"
	"fmt"
	"krillin-ai/internal/types"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

func GenerateRawSegments(ctx context.Context, tts types.Ttser, plan []PlanItem, voice, dir string, run CommandRunner, duration DurationProbe) ([]PlanItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil {
		run = defaultFFmpegRunner
	}
	if duration == nil {
		return nil, errors.New("duration probe is required")
	}

	rawDir := filepath.Join(dir, "raw")
	if err := os.MkdirAll(rawDir, 0755); err != nil {
		return nil, err
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(3)

	for i := range plan {
		i := i
		g.Go(func() error {
			if err := gCtx.Err(); err != nil {
				return err
			}

			output := filepath.Join(rawDir, fmt.Sprintf("%d.wav", plan[i].Index))
			if IsSilenceOnlyText(plan[i].SpokenText) {
				if err := WriteTinySilence(output, run); err != nil {
					return err
				}
			} else {
				if tts == nil {
					return errors.New("tts is required for non-silence text")
				}
				if err := retryTTS(tts, plan[i].SpokenText, voice, output, 3); err != nil {
					return fmt.Errorf("tts segment %d failed: %w", plan[i].Index, err)
				}
			}

			dur, err := duration(output)
			if err != nil {
				return fmt.Errorf("measure segment %d duration failed for %s: %w", plan[i].Index, output, err)
			}
			plan[i].ActualDuration = dur
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return plan, nil
}

func GenerateRawChunkSegments(ctx context.Context, tts types.Ttser, plan []PlanItem, chunks []Chunk, voice, dir string, run CommandRunner, duration DurationProbe, onProgress func(int)) ([]PlanItem, []Chunk, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil {
		run = defaultFFmpegRunner
	}
	if duration == nil {
		return nil, nil, errors.New("duration probe is required")
	}

	rawDir := filepath.Join(dir, "raw")
	if err := os.MkdirAll(rawDir, 0755); err != nil {
		return nil, nil, err
	}

	outPlan := append([]PlanItem(nil), plan...)
	outChunks := append([]Chunk(nil), chunks...)
	
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(3)
	
	var completed int32

	for i := range outChunks {
		i := i
		g.Go(func() error {
			if err := gCtx.Err(); err != nil {
				return err
			}
			text, err := chunkSpeechText(outPlan, outChunks[i])
			if err != nil {
				return err
			}

			output := filepath.Join(rawDir, fmt.Sprintf("chunk_%d.wav", outChunks[i].ID))
			if IsSilenceOnlyText(text) {
				if err := WriteTinySilence(output, run); err != nil {
					return err
				}
			} else {
				if tts == nil {
					return errors.New("tts is required for non-silence text")
				}
				if err := retryTTS(tts, text, voice, output, 3); err != nil {
					return fmt.Errorf("tts chunk %d failed: %w", outChunks[i].ID, err)
				}
			}

			dur, err := duration(output)
			if err != nil {
				return fmt.Errorf("measure chunk %d duration failed for %s: %w", outChunks[i].ID, output, err)
			}
			outChunks[i].ActualDuration = dur

			// Backfill to plan items so frontend can access the duration
			if len(outChunks[i].Items) > 0 {
				avg := dur / float64(len(outChunks[i].Items))
				for _, idx := range outChunks[i].Items {
					if idx >= 0 && idx < len(outPlan) {
						outPlan[idx].ActualDuration = avg
					}
				}
			}

			currCompleted := atomic.AddInt32(&completed, 1)
			if onProgress != nil {
				onProgress(int(float64(currCompleted) / float64(len(outChunks)) * 95))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	return outPlan, outChunks, nil
}

func chunkSpeechText(plan []PlanItem, chunk Chunk) (string, error) {
	parts := make([]string, 0, len(chunk.Items))
	for itemIndex, idx := range chunk.Items {
		if idx < 0 || idx >= len(plan) {
			return "", fmt.Errorf("chunk %d references plan item %d out of range", chunk.ID, itemIndex)
		}
		text := strings.Join(strings.Fields(plan[idx].SpokenText), " ")
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " "), nil
}

func retryTTS(tts types.Ttser, text, voice, output string, attempts int) error {
	if attempts <= 0 {
		return fmt.Errorf("attempts must be > 0: %d", attempts)
	}

	var last error
	for i := 0; i < attempts; i++ {
		if err := os.Remove(output); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale output %s: %w", output, err)
		}
		last = tts.Text2Speech(text, voice, output)
		if last == nil {
			if _, err := os.Stat(output); err == nil {
				return nil
			}
			last = fmt.Errorf("output file missing: %s", output)
		}
	}
	return last
}
