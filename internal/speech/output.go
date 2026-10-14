package speech

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Projection removes reasoning, code blocks and stage directions from speech.
// It preserves incomplete marker prefixes across streamed token boundaries.
type Projection struct {
	buffer string
	hidden string
	cues   map[string]bool
}

func (p *Projection) Feed(text string, final bool) string {
	p.buffer += text
	var out strings.Builder
	markers := []string{"<think>", "```", "["}
	for len(p.buffer) > 0 {
		if p.hidden != "" {
			at := strings.Index(p.buffer, p.hidden)
			if at < 0 {
				keep := min(len(p.hidden)-1, len(p.buffer))
				p.buffer = p.buffer[len(p.buffer)-keep:]
				break
			}
			p.buffer = p.buffer[at+len(p.hidden):]
			p.hidden = ""
			continue
		}
		if strings.HasPrefix(p.buffer, "[") {
			at := strings.Index(p.buffer, "]")
			if at < 0 {
				if !final && len(p.buffer) < 128 {
					break
				}
				p.buffer = p.buffer[1:]
				p.hidden = "]"
				continue
			}
			tag := p.buffer[1:at]
			if p.cues[tag] {
				out.WriteString(p.buffer[:at+1])
			}
			p.buffer = p.buffer[at+1:]
			continue
		}
		partial := false
		for _, marker := range markers {
			if strings.HasPrefix(p.buffer, marker) {
				p.buffer = p.buffer[len(marker):]
				switch marker {
				case "<think>":
					p.hidden = "</think>"
				case "```":
					p.hidden = "```"
				case "[":
					p.hidden = "]"
				}
				partial = true
				break
			}
			if !final && strings.HasPrefix(marker, p.buffer) {
				partial = true
				break
			}
		}
		if partial {
			if p.hidden != "" {
				continue
			}
			break
		}
		r, n := utf8.DecodeRuneInString(p.buffer)
		if r == utf8.RuneError && n == 1 && !utf8.FullRuneInString(p.buffer) && !final {
			break
		}
		p.buffer = p.buffer[n:]
		if r != '*' && r != '_' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Output synthesizes sentence chunks while the model continues generating.
// One bounded queue and one player preserve order and the final audio tail.
type Output struct {
	ctx        context.Context
	cancel     context.CancelFunc
	queue      chan string
	done       chan struct{}
	err        error
	projection Projection
	pending    string
	notify     func(string)
}

func NewOutput(ctx context.Context, service Service, notify func(string)) *Output {
	ctx, cancel := context.WithCancel(ctx)
	o := &Output{ctx: ctx, cancel: cancel, queue: make(chan string, 2), done: make(chan struct{}), notify: notify}
	if provider, ok := service.(DeliveryProvider); ok {
		o.projection.cues = map[string]bool{}
		for _, cue := range provider.DeliveryCues() {
			o.projection.cues[cue] = true
		}
	}
	go func() {
		defer close(o.done)
		player := service.Player(ctx)
		defer player.Cancel()
		for {
			select {
			case <-ctx.Done():
				player.Cancel()
				player.Finish()
				o.err = ctx.Err()
				return
			case text, ok := <-o.queue:
				if !ok {
					o.err = player.Finish()
					return
				}
				if notify != nil {
					notify("Preparing speech")
				}
				wav, err := service.Synthesize(ctx, text)
				if err != nil {
					player.Cancel()
					player.Finish()
					o.err = err
					return
				}
				if notify != nil {
					notify("Speaking")
				}
				if err = player.Enqueue(ctx, wav); err != nil {
					player.Cancel()
					player.Finish()
					o.err = err
					return
				}
			}
		}
	}()
	return o
}
func (o *Output) send(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	select {
	case <-o.done:
		return o.err
	case <-o.ctx.Done():
		return o.ctx.Err()
	case o.queue <- strings.TrimSpace(text):
		return nil
	}
}
func (o *Output) Delta(text string) error {
	o.pending += o.projection.Feed(text, false)
	for {
		end := 0
		for i := 0; i+1 < len(o.pending); i++ {
			if strings.ContainsRune(".!?", rune(o.pending[i])) && (o.pending[i+1] == ' ' || o.pending[i+1] == '\n') {
				end = i + 1
				break
			}
		}
		// Long sentences use clause/word boundaries, never mid-word cuts.
		if end > 400 || (end == 0 && len(o.pending) > 400) {
			end = strings.LastIndexAny(o.pending[:400], ",;:")
			if end > 0 {
				end++
			} else {
				end = strings.LastIndexAny(o.pending[:400], " \n")
			}
		}
		if end <= 0 {
			if len(o.pending) > 8192 {
				return fmt.Errorf("speech phrase exceeds provider limit")
			}
			return nil
		}
		if err := o.send(o.pending[:end]); err != nil {
			return err
		}
		o.pending = o.pending[end:]
	}
}
func (o *Output) Finish() error {
	o.pending += o.projection.Feed("", true)
	if err := o.send(o.pending); err != nil {
		o.cancel()
	}
	o.pending = ""
	close(o.queue)
	<-o.done
	o.cancel()
	return o.err
}
func (o *Output) Cancel() { o.cancel() }
