package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

const (
	grokVoiceTTSPublicModel      = xai.DefaultVoiceTTSModel
	grokVoiceSTTPublicModel      = xai.DefaultVoiceSTTModel
	grokVoiceRealtimePublicModel = xai.DefaultVoiceRealtimeModel
	grokVoiceRealtimeUpstream    = "grok-voice-latest"
)

// xAI's native TTS endpoint accepts Grok voice IDs, while OpenAI-compatible
// clients commonly send the standard OpenAI voice names. Keep the ingress
// contract compatible by translating the known names before forwarding. Grok
// IDs are canonicalized only for casing; unknown values remain untouched so
// custom voice IDs continue to work.
var grokOpenAIVoiceAliases = map[string]string{
	"alloy":   "Ara",
	"ash":     "Rex",
	"ballad":  "Sal",
	"coral":   "Eve",
	"echo":    "Leo",
	"fable":   "Ara",
	"nova":    "Eve",
	"onyx":    "Rex",
	"sage":    "Sal",
	"shimmer": "Eve",
	"verse":   "Leo",
	"marin":   "Ara",
	"cedar":   "Rex",
}

var grokVoiceIDs = map[string]string{
	"ara": "Ara",
	"rex": "Rex",
	"sal": "Sal",
	"eve": "Eve",
	"leo": "Leo",
}

// normalizeGrokOpenAIAudioRequest translates the standard OpenAI audio
// request shape into the native xAI/Grok Voice request shape. The returned
// model is kept separately because Grok's HTTP voice endpoints select the
// operation from the URL rather than from a model field in the JSON body.
func normalizeGrokOpenAIAudioRequest(operation string, body []byte, contentType string) ([]byte, string, string, error) {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "speech":
		return normalizeGrokOpenAISpeechRequest(body)
	case "transcriptions":
		return normalizeGrokOpenAITranscriptionRequest(body, contentType)
	default:
		return nil, "", "", fmt.Errorf("unsupported Grok OpenAI audio operation: %s", operation)
	}
}

func normalizeGrokOpenAISpeechRequest(body []byte) ([]byte, string, string, error) {
	var request struct {
		Model    string `json:"model"`
		Input    string `json:"input"`
		Voice    string `json:"voice"`
		Language string `json:"language,omitempty"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, "", "", fmt.Errorf("invalid speech JSON: %w", err)
	}
	if strings.TrimSpace(request.Input) == "" {
		return nil, "", "", errors.New("speech input is required")
	}

	upstream := map[string]any{"text": request.Input}
	if voice := strings.TrimSpace(request.Voice); voice != "" {
		upstream["voice_id"] = normalizeGrokVoiceID(voice)
	}
	language := strings.TrimSpace(request.Language)
	if language == "" {
		language = "en"
	}
	upstream["language"] = language
	normalized, err := json.Marshal(upstream)
	if err != nil {
		return nil, "", "", fmt.Errorf("encode speech request: %w", err)
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = grokVoiceTTSPublicModel
	}
	return normalized, "application/json", model, nil
}

func normalizeGrokVoiceID(voice string) string {
	trimmed := strings.TrimSpace(voice)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if alias, ok := grokOpenAIVoiceAliases[lower]; ok {
		return alias
	}
	if canonical, ok := grokVoiceIDs[lower]; ok {
		return canonical
	}
	return trimmed
}

func normalizeGrokOpenAITranscriptionRequest(body []byte, contentType string) ([]byte, string, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return nil, "", "", errors.New("audio transcriptions require multipart/form-data")
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return nil, "", "", errors.New("audio transcriptions multipart boundary is required")
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var normalized bytes.Buffer
	writer := multipart.NewWriter(&normalized)
	model := ""
	modelSeen := false
	hasFile := false
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			_ = writer.Close()
			return nil, "", "", fmt.Errorf("read audio transcription multipart body: %w", nextErr)
		}
		data, readErr := io.ReadAll(part)
		if readErr != nil {
			_ = writer.Close()
			return nil, "", "", fmt.Errorf("read audio transcription part: %w", readErr)
		}
		if part.FormName() == "model" {
			modelSeen = true
			model = strings.TrimSpace(string(data))
			data = []byte(normalizeGrokSTTUpstreamModel(model))
		}
		if part.FormName() == "file" {
			hasFile = true
		}
		created, createErr := writer.CreatePart(part.Header)
		if createErr != nil {
			_ = writer.Close()
			return nil, "", "", fmt.Errorf("write audio transcription part: %w", createErr)
		}
		if _, writeErr := created.Write(data); writeErr != nil {
			_ = writer.Close()
			return nil, "", "", fmt.Errorf("write audio transcription data: %w", writeErr)
		}
	}
	if !hasFile {
		_ = writer.Close()
		return nil, "", "", errors.New("audio transcription file is required")
	}
	if !modelSeen {
		if err := writer.WriteField("model", "grok-stt"); err != nil {
			_ = writer.Close()
			return nil, "", "", fmt.Errorf("write default audio transcription model: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", "", fmt.Errorf("close audio transcription multipart body: %w", err)
	}
	if model == "" {
		model = grokVoiceSTTPublicModel
	}
	return normalized.Bytes(), writer.FormDataContentType(), model, nil
}

func normalizeGrokSTTUpstreamModel(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "", grokVoiceSTTPublicModel, "grok-voice-transcription", "whisper-1":
		return "grok-stt"
	default:
		return strings.TrimSpace(model)
	}
}

func normalizeGrokRealtimeModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" || strings.EqualFold(model, grokVoiceRealtimePublicModel) {
		return grokVoiceRealtimeUpstream
	}
	return model
}
