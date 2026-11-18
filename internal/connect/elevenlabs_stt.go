package connect

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// Transcribe turns speech into text with ElevenLabs Speech to Text
// (Scribe), for voice notes the owner and contacts send on WhatsApp.
func (e *ElevenLabs) Transcribe(ctx context.Context, env *Env, name, mime string, audio []byte) (string, error) {
	if len(audio) == 0 {
		return "", errors.New("áudio vazio")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("model_id", "scribe_v1")
	_ = w.WriteField("tag_audio_events", "false")
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+strings.ReplaceAll(orName(name), `"`, "")+`"`)
	if mime == "" {
		mime = "application/octet-stream"
	}
	h.Set("Content-Type", mime)
	part, err := w.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := callJSON(ctx, request{Method: "POST", URL: e.base() + "/v1/speech-to-text", Header: e.header(env),
		Body: body.Bytes(), ContentType: w.FormDataContentType()}, &out); err != nil {
		return "", err
	}
	text := strings.TrimSpace(out.Text)
	if text == "" {
		return "", errors.New("não deu para entender a fala do áudio")
	}
	return text, nil
}

func orName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "audio.ogg"
	}
	return name
}
