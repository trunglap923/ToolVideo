package main

import (
	"fmt"
	"krillin-ai/internal/storage"
	"krillin-ai/log"
	"krillin-ai/pkg/gtts"
	"os"
)

func main() {
	log.InitLogger()

	// Tìm gtts-cli tự động
	storage.GttsBinPath = "gtts-cli"
	storage.FfmpegPath = "bin\\ffmpeg.exe"

	client := gtts.NewGTtsClient()

	tests := []struct {
		voice string
		text  string
	}{
		{"vi-VN-HoaiMyNeural", "Xin chào! Đây là thử nghiệm giọng tiếng Việt."},
		{"vi", "Thử nghiệm với mã ngôn ngữ ngắn vi."},
		{"en-US-JennyNeural", "Hello, this is an English voice test."},
	}

	for i, tt := range tests {
		outFile := fmt.Sprintf("test_gtts_output_%d.wav", i+1)
		fmt.Printf("\n[Test %d] Voice: %q\n  Text: %q\n  Output: %s\n", i+1, tt.voice, tt.text, outFile)

		err := client.Text2Speech(tt.text, tt.voice, outFile)
		if err != nil {
			fmt.Printf("  ❌ FAIL: %v\n", err)
		} else {
			info, _ := os.Stat(outFile)
			fmt.Printf("  ✅ OK: %d bytes\n", info.Size())
		}
	}

	fmt.Println("\nDọn dẹp file test...")
	for i := range tests {
		os.Remove(fmt.Sprintf("test_gtts_output_%d.wav", i+1))
	}
	fmt.Println("Xong!")
}
