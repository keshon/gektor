package conversation

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/keshon/gektor/internal/llm"
)

// FailureText bounds saved diagnostics and omits backend bodies and URL credentials.
func FailureText(err error) string {
	if err == nil {
		return ""
	}
	var text string
	if api, ok := llm.AsAPIError(err); ok {
		text = fmt.Sprintf("Backend HTTP %d; response body omitted from saved diagnostics", api.Status)
	} else {
		var request *url.Error
		if errors.As(err, &request) {
			text = request.Op + ": " + FailureText(request.Err)
		} else {
			text = err.Error()
		}
	}
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	runes := []rune(text)
	if len(runes) > 4096 {
		text = string(runes[:4096]) + "…"
	}
	return text
}
