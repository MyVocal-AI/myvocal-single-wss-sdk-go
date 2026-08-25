package myvocalsinglewss

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var supportedModels = map[string]bool{
	"flash_v2_5":     true,
	"multilingual_v2": true,
}

var supportedFormats = map[string]bool{
	"mp3_44100_64":  true,
	"mp3_44100_96":  true,
	"mp3_44100_128": true,
	"mp3_44100_192": true,
	"pcm_8000":       true,
	"pcm_16000":      true,
	"pcm_22050":      true,
	"pcm_24000":      true,
	"pcm_44100":      true,
	"ulaw_8000":      true,
}

var commonLanguages = map[string]bool{
	"en": true, "ja": true, "zh": true, "de": true, "hi": true,
	"fr": true, "ko": true, "pt": true, "it": true, "es": true,
	"id": true, "nl": true, "tr": true, "fil": true, "pl": true,
	"sv": true, "bg": true, "ro": true, "ar": true, "cs": true,
	"el": true, "fi": true, "hr": true, "ms": true, "sk": true,
	"da": true, "ta": true, "uk": true, "ru": true,
}

var flashExtraLanguages = map[string]bool{
	"hu": true, "no": true, "vi": true,
}

type ConnectionOptions struct {
	BaseURL         string
	VoiceID        string
	ModelID        string
	Priority       string
	OutputFormat   string
	IdleTimeout    *int
	LanguageCode   string
	Timestamps     *bool
	DirectStreaming *bool
	Seed            *uint64
	AutoMode        *bool
}

func (options ConnectionOptions) normalized() ConnectionOptions {
	if options.BaseURL == "" {
		options.BaseURL = "wss://api.voicelibrary.co"
	}
	if options.Priority == "" {
		options.Priority = "dedicated_concurrency"
	}
	return options
}

func (options ConnectionOptions) BuildURL() (string, error) {
	options = options.normalized()
	if options.VoiceID == "" {
		return "", &ValidationError{Message: "voice_id must not be empty"}
	}
	if !supportedModels[options.ModelID] {
		return "", &ValidationError{Message: "unsupported model_id"}
	}
	if options.Priority != "dedicated_concurrency" {
		return "", &ValidationError{
			Message: "priority must be dedicated_concurrency",
		}
	}
	if !supportedFormats[options.OutputFormat] {
		return "", &ValidationError{Message: "unsupported output format"}
	}
	if options.IdleTimeout != nil &&
		(*options.IdleTimeout < 1 || *options.IdleTimeout > 180) {
		return "", &ValidationError{
			Message: "idle timeout must be between 1 and 180",
		}
	}
	if options.Seed != nil && *options.Seed > 4294967295 {
		return "", &ValidationError{
			Message: "seed must be between 0 and 4294967295",
		}
	}
	if options.LanguageCode != "" {
		supported := commonLanguages[options.LanguageCode]
		if options.ModelID == "flash_v2_5" {
			supported = supported || flashExtraLanguages[options.LanguageCode]
		}
		if !supported {
			return "", &ValidationError{
				Message: "language_code is not supported by the selected model",
			}
		}
	}
	if !strings.HasPrefix(options.BaseURL, "wss://") &&
		!strings.HasPrefix(options.BaseURL, "ws://") {
		return "", &ValidationError{Message: "base URL must use ws or wss"}
	}

	query := url.Values{}
	query.Set("model_id", options.ModelID)
	query.Set("priority", options.Priority)
	query.Set("format", options.OutputFormat)
	if options.IdleTimeout != nil {
		query.Set("idleTimeout", strconv.Itoa(*options.IdleTimeout))
	}
	if options.LanguageCode != "" {
		query.Set("language_code", options.LanguageCode)
	}
	if options.Timestamps != nil {
		query.Set("Timestamps", strconv.FormatBool(*options.Timestamps))
	}
	if options.DirectStreaming != nil {
		query.Set(
			"directStreaming",
			strconv.FormatBool(*options.DirectStreaming),
		)
	}
	if options.Seed != nil {
		query.Set("seed", strconv.FormatUint(*options.Seed, 10))
	}
	if options.AutoMode != nil {
		query.Set("auto_mode", strconv.FormatBool(*options.AutoMode))
	}
	return fmt.Sprintf(
		"%s/enterprise/v1/tts/%s/websocket?%s",
		strings.TrimRight(options.BaseURL, "/"),
		url.PathEscape(options.VoiceID),
		query.Encode(),
	), nil
}

type VoiceSettings struct {
	Stability       *float64
	SimilarityBoost *float64
	Style           *float64
	UseSpeakerBoost *bool
}

func (settings VoiceSettings) Wire() (map[string]any, error) {
	values := []struct {
		name  string
		value *float64
	}{
		{"stability", settings.Stability},
		{"similarity_boost", settings.SimilarityBoost},
		{"style", settings.Style},
	}
	for _, item := range values {
		if item.value != nil && (*item.value < 0 || *item.value > 1) {
			return nil, &ValidationError{
				Message: item.name + " must be between 0 and 1",
			}
		}
	}
	value := map[string]any{}
	if settings.Stability != nil {
		value["stability"] = *settings.Stability
	}
	if settings.SimilarityBoost != nil {
		value["similarity_boost"] = *settings.SimilarityBoost
	}
	if settings.Style != nil {
		value["style"] = *settings.Style
	}
	if settings.UseSpeakerBoost != nil {
		value["use_speaker_boost"] = *settings.UseSpeakerBoost
	}
	return value, nil
}

type InitializationOptions struct {
	Text              string
	PreviousText      string
	NextText          string
	VoiceSettings     *VoiceSettings
	SynthesisSchedule []int
}

func (options InitializationOptions) Wire() (map[string]any, error) {
	text := options.Text
	if text == "" {
		text = " "
	}
	value := map[string]any{"text": text}
	if options.PreviousText != "" {
		value["previous_text"] = options.PreviousText
	}
	if options.NextText != "" {
		value["next_text"] = options.NextText
	}
	if options.VoiceSettings != nil {
		settings, err := options.VoiceSettings.Wire()
		if err != nil {
			return nil, err
		}
		value["voice_settings"] = settings
	}
	if options.SynthesisSchedule != nil {
		if len(options.SynthesisSchedule) == 0 {
			return nil, &ValidationError{
				Message: "synthesis schedule must not be empty",
			}
		}
		for _, item := range options.SynthesisSchedule {
			if item <= 0 {
				return nil, &ValidationError{
					Message: "synthesis schedule values must be positive",
				}
			}
		}
		value["generation_config"] = map[string]any{
			"synthesisSchedule": options.SynthesisSchedule,
		}
	}
	return value, nil
}
