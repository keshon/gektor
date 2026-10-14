package chatterbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/keshon/tars/internal/speech"
)

// This vocabulary matches the pinned Nano checkpoint's added_tokens.json.
var deliveryCues = [...]string{
	"advertisement", "angry", "chuckle", "clear throat", "cough", "crying",
	"dramatic", "fear", "gasp", "groan", "happy", "laugh", "narration",
	"sarcastic", "shush", "sigh", "sniff", "surprised", "whispering",
}

func (s *Synthesizer) DeliveryCues() []string { return append([]string(nil), deliveryCues[:]...) }

func (s *Synthesizer) DeliveryInstructions() string {
	return `Choose delivery from the conversational context, the character, and the user's requested performance. Use happy for genuine delight, surprised for unexpected news, angry or fear when the character or scene warrants them, and sarcastic only when the intended irony is clear. Use whispering for a hushed delivery, crying for a tearful performance, dramatic for heightened acting, narration for storytelling, and advertisement for a requested promotional performance.
Place chuckle or laugh at an amused reaction; sigh at relief, weariness, or disappointment; gasp at a startled reaction; groan at frustration; shush when intentionally hushing. Cough, clear throat, and sniff are deliberate vocal actions, not routine filler. Do not use them to simulate illness unless the scene calls for it.
Match intensity to the situation. Neutral answers can have no cues; expressive scenes can combine and change cues as the dialogue develops. Avoid repetitive tics, contradictory simultaneous delivery, or laughter at distress. An explicit request to demonstrate a supported cue is a reason to use it. Place a style cue before the phrase it affects and a vocal-action cue at the exact point where the sound belongs. Never claim a cue changes the persistent voice identity.`
}

func (s *Synthesizer) SynthesizeUtterance(ctx context.Context, u speech.Utterance, language, voice string) ([]byte, error) {
	if s.log != nil && len(u.Cues) > 0 {
		names := make([]string, 0, len(u.Cues))
		for _, cue := range u.Cues {
			names = append(names, cue.Name)
		}
		fmt.Fprintf(s.log, "Delivery cues: %s\n", strings.Join(names, ", "))
	}
	return s.Synthesize(ctx, renderDelivery(u), language, voice)
}

func renderDelivery(u speech.Utterance) string {
	var out strings.Builder
	start := 0
	for _, cue := range u.Cues {
		if cue.Offset < start || cue.Offset > len(u.Text) {
			continue
		}
		supported := false
		for _, name := range deliveryCues {
			if cue.Name == name {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		out.WriteString(u.Text[start:cue.Offset])
		out.WriteString("[" + cue.Name + "] ")
		start = cue.Offset
	}
	out.WriteString(u.Text[start:])
	return out.String()
}
