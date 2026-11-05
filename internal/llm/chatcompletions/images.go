package chatcompletions

import (
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/llm"
)

type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// encodeContent builds a message's wire content: a plain string when
// imageless (byte-identical to the old shape — text models, text
// backends, and golden tests never see a difference), a multipart
// array otherwise. allowImages is false on backends that cannot
// process pictures: attaching there is a loud error, never a silent
// drop the model then hallucinates around.
func encodeContent(text string, images []string, allowImages bool) (json.RawMessage, error) {
	if len(images) == 0 {
		raw, err := json.Marshal(text)
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
	if !allowImages {
		return nil, fmt.Errorf("images attached but this backend cannot process images — use a vision-capable llama-server backend with --mmproj")
	}
	parts := []contentPart{{Type: "text", Text: text}}
	for _, path := range images {
		url, err := llm.EncodeImageFile(path)
		if err != nil {
			return nil, err
		}
		parts = append(parts, contentPart{
			Type: "image_url",
			ImageURL: &struct {
				URL string `json:"url"`
			}{URL: url},
		})
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// encodeMessages converts history to the wire shape, shared by Chat
// and Stream so both paths attach (or refuse) identically.
func encodeMessages(messages []llm.Message, allowImages bool) ([]wireMessage, error) {
	out := make([]wireMessage, 0, len(messages))
	for _, m := range messages {
		content, err := encodeContent(m.Content, m.Images, allowImages)
		if err != nil {
			return nil, err
		}
		wm := wireMessage{Role: string(m.Role), Content: content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			// Per the OpenAI tool-call wire format, function.arguments is a
			// JSON-encoded *string*, not a raw object — encodeArguments
			// re-wraps our internal object form before it goes back out.
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: wireFunction{
					Name:      tc.Name,
					Arguments: encodeArguments(tc.Arguments),
				},
			})
		}
		out = append(out, wm)
	}
	return out, nil
}
