package myvocalsinglewss

import (
	"strings"
	"testing"
)

func validOptions() ConnectionOptions {
	return ConnectionOptions{
		BaseURL:      "wss://api.voicelibrary.co",
		VoiceID:     "voice/id",
		ModelID:     "flash_v2_5",
		Priority:    "dedicated_concurrency",
		OutputFormat: "pcm_16000",
	}
}

func TestBuildURLUsesEnterpriseWireNames(t *testing.T) {
	options := validOptions()
	timeout := 120
	seed := uint64(0)
	enabled := true
	disabled := false
	options.IdleTimeout = &timeout
	options.LanguageCode = "zh"
	options.Timestamps = &enabled
	options.DirectStreaming = &disabled
	options.Seed = &seed
	options.AutoMode = &enabled

	rawURL, err := options.BuildURL()
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"/voice%2Fid/websocket?",
		"model_id=flash_v2_5",
		"priority=dedicated_concurrency",
		"format=pcm_16000",
		"idleTimeout=120",
		"language_code=zh",
		"Timestamps=true",
		"directStreaming=false",
		"seed=0",
		"auto_mode=true",
	} {
		if !strings.Contains(rawURL, part) {
			t.Fatalf("missing %q in %s", part, rawURL)
		}
	}
}

func TestValidationRejectsInvalidContractValues(t *testing.T) {
	cases := []func(*ConnectionOptions){
		func(o *ConnectionOptions) { o.ModelID = "myvocal_v3" },
		func(o *ConnectionOptions) { o.Priority = "auto" },
		func(o *ConnectionOptions) { o.OutputFormat = "wav" },
		func(o *ConnectionOptions) { v := 0; o.IdleTimeout = &v },
		func(o *ConnectionOptions) { v := 181; o.IdleTimeout = &v },
		func(o *ConnectionOptions) { v := uint64(4294967296); o.Seed = &v },
		func(o *ConnectionOptions) { o.ModelID = "multilingual_v2"; o.LanguageCode = "vi" },
	}
	for index, mutate := range cases {
		options := validOptions()
		mutate(&options)
		if _, err := options.BuildURL(); err == nil {
			t.Fatalf("case %d should fail", index)
		}
	}
}

func TestInitializationWireShape(t *testing.T) {
	settings := &VoiceSettings{
		Stability:         number(0.5),
		SimilarityBoost:   number(0.75),
		Style:             number(0.2),
		UseSpeakerBoost:   boolean(true),
	}
	init := InitializationOptions{
		PreviousText:      "before",
		NextText:          "after",
		VoiceSettings:     settings,
		SynthesisSchedule: []int{120, 160},
	}
	wire, err := init.Wire()
	if err != nil {
		t.Fatal(err)
	}
	if wire["text"] != " " {
		t.Fatalf("unexpected init text: %#v", wire["text"])
	}
	if _, ok := wire["generation_config"]; !ok {
		t.Fatal("generation_config missing")
	}
}

func number(value float64) *float64 { return &value }
func boolean(value bool) *bool      { return &value }
