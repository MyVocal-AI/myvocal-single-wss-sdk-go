package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	singlewss "github.com/MyVocal-AI/myvocal-single-wss-sdk-go"
)

func main() {
	apiKey := requireEnvironment("MYVOCAL_TTS_API_KEY")
	voiceID := requireEnvironment("MYVOCAL_TTS_VOICE_ID")
	var mu sync.Mutex
	audioBytes := 0
	addAudio := func(audio []byte) {
		mu.Lock()
		audioBytes += len(audio)
		mu.Unlock()
	}
	client := singlewss.NewClient(
		apiKey,
		singlewss.ConnectionOptions{
			VoiceID:      voiceID,
			ModelID:      "flash_v2_5",
			OutputFormat: "pcm_16000",
			LanguageCode: "en",
		},
		singlewss.Callbacks{
			OnAudio:       addAudio,
			OnBinaryAudio: addAudio,
		},
	)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		panic(err)
	}
	defer client.Close()
	if err := client.Initialize(singlewss.InitializationOptions{
		VoiceSettings: &singlewss.VoiceSettings{
			Stability:       number(0.5),
			SimilarityBoost: number(0.75),
		},
	}); err != nil {
		panic(err)
	}
	if err := client.SendText(
		"This is a MyVocal Single WebSocket SDK test.",
		false,
	); err != nil {
		panic(err)
	}
	if err := client.Finish(ctx); err != nil {
		panic(err)
	}
	if err := client.DrainCallbacks(ctx); err != nil {
		panic(err)
	}
	mu.Lock()
	fmt.Printf("Audio bytes: %d\n", audioBytes)
	mu.Unlock()
}

func requireEnvironment(name string) string {
	value := os.Getenv(name)
	if value == "" {
		panic(name + " is required")
	}
	return value
}

func number(value float64) *float64 { return &value }
