package handler

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeGrokOpenAISpeechRequest(t *testing.T) {
	body := []byte(`{"model":"grok-voice-tts","input":"Hello voice","voice":"Ara","response_format":"mp3","speed":1.25}`)

	normalized, contentType, model, err := normalizeGrokOpenAIAudioRequest("speech", body, "application/json")

	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "grok-voice-tts", model)
	// Grok's native probe contract currently accepts text/language/voice_id;
	// OpenAI-only response_format and speed fields must not be sent upstream.
	require.JSONEq(t, `{"text":"Hello voice","voice_id":"Ara","language":"en"}`, string(normalized))
}

func TestNormalizeGrokOpenAISpeechRequestRequiresInput(t *testing.T) {
	_, _, _, err := normalizeGrokOpenAIAudioRequest("speech", []byte(`{"model":"grok-voice-tts"}`), "application/json")

	require.Error(t, err)
	require.Contains(t, err.Error(), "input")
}

func TestNormalizeGrokOpenAITranscriptionRequestMapsVoiceModel(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "grok-voice-stt"))
	require.NoError(t, writer.WriteField("language", "zh"))
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="file"; filename="sample.wav"`},
		"Content-Type":        {"audio/wav"},
	})
	require.NoError(t, err)
	_, err = part.Write([]byte("RIFF-audio"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	normalized, contentType, model, err := normalizeGrokOpenAIAudioRequest(
		"transcriptions", body.Bytes(), writer.FormDataContentType(),
	)

	require.NoError(t, err)
	require.Equal(t, "grok-voice-stt", model)
	require.True(t, strings.HasPrefix(contentType, "multipart/form-data; boundary="))

	mediaType, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	reader := multipart.NewReader(bytes.NewReader(normalized), params["boundary"])

	fields := map[string]string{}
	var fileBytes []byte
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		require.NoError(t, nextErr)
		data, readErr := io.ReadAll(part)
		require.NoError(t, readErr)
		if part.FormName() == "file" {
			require.Equal(t, "sample.wav", part.FileName())
			fileBytes = data
			continue
		}
		fields[part.FormName()] = string(data)
	}
	require.Equal(t, "grok-stt", fields["model"])
	require.Equal(t, "zh", fields["language"])
	require.Equal(t, []byte("RIFF-audio"), fileBytes)
}

func TestNormalizeGrokOpenAITranscriptionRequestAddsDefaultUpstreamModel(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sample.wav")
	require.NoError(t, err)
	_, err = part.Write([]byte("RIFF-audio"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	normalized, contentType, model, err := normalizeGrokOpenAIAudioRequest(
		"transcriptions", body.Bytes(), writer.FormDataContentType(),
	)

	require.NoError(t, err)
	require.Equal(t, "grok-voice-stt", model)
	_, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	reader := multipart.NewReader(bytes.NewReader(normalized), params["boundary"])
	fields := map[string]string{}
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		require.NoError(t, nextErr)
		data, readErr := io.ReadAll(part)
		require.NoError(t, readErr)
		if part.FormName() != "file" {
			fields[part.FormName()] = string(data)
		}
	}
	require.Equal(t, "grok-stt", fields["model"])
}

func TestNormalizeGrokRealtimeModelAliases(t *testing.T) {
	require.Equal(t, "grok-voice-latest", normalizeGrokRealtimeModel(""))
	require.Equal(t, "grok-voice-latest", normalizeGrokRealtimeModel("grok-voice-realtime"))
	require.Equal(t, "grok-voice-latest", normalizeGrokRealtimeModel("grok-voice-latest"))
	require.Equal(t, "custom-realtime", normalizeGrokRealtimeModel("custom-realtime"))
}
