package speech

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type outputService struct {
	mu      sync.Mutex
	text    []string
	started chan struct{}
	player  *outputPlayer
	fail    bool
	block   bool
}
type outputPlayer struct {
	mu      sync.Mutex
	chunks  int
	drained bool
}

func (p *outputPlayer) Enqueue(context.Context, []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chunks++
	return nil
}
func (p *outputPlayer) Finish() error                            { p.mu.Lock(); defer p.mu.Unlock(); p.drained = true; return nil }
func (p *outputPlayer) Cancel()                                  {}
func (s *outputService) Record(context.Context) (Capture, error) { return nil, errors.New("unused") }
func (s *outputService) Recognize(context.Context, []byte) (Transcript, error) {
	return Transcript{}, errors.New("unused")
}
func (s *outputService) Player(context.Context) Player { return s.player }
func (s *outputService) Close()                        {}
func (s *outputService) Synthesize(ctx context.Context, text string) ([]byte, error) {
	s.mu.Lock()
	s.text = append(s.text, text)
	s.mu.Unlock()
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.fail {
		return nil, errors.New("tts failed")
	}
	return testWAV(), nil
}
func TestProjectionAcrossEveryByteBoundary(t *testing.T) {
	input := "<think>private idea.</think>Hello. [leans in] Nice.\n```go\nsecret\n```End."
	var p Projection
	var spoken strings.Builder
	for i := range len(input) {
		spoken.WriteString(p.Feed(input[i:i+1], false))
	}
	spoken.WriteString(p.Feed("", true))
	if spoken.String() != "Hello.  Nice.\nEnd." {
		t.Fatalf("spoken=%q", spoken.String())
	}
}
func TestSentenceSpeechStartsBeforeFinalModelText(t *testing.T) {
	s := &outputService{started: make(chan struct{}, 1), player: &outputPlayer{}}
	o := NewOutput(context.Background(), s, nil)
	if err := o.Delta("First sentence. "); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.started:
	case <-time.After(time.Second):
		t.Fatal("waited for full model reply")
	}
	if err := o.Delta("Final sentence."); err != nil {
		t.Fatal(err)
	}
	if err := o.Finish(); err != nil {
		t.Fatal(err)
	}
	if len(s.text) != 2 || s.text[0] != "First sentence." || s.text[1] != "Final sentence." || !s.player.drained || s.player.chunks != 2 {
		t.Fatalf("speech lost ordering/tail: %+v", s.text)
	}
}
func TestSpeechFailureAndCancellationCannotDeadlock(t *testing.T) {
	for _, fail := range []bool{false, true} {
		s := &outputService{started: make(chan struct{}, 1), player: &outputPlayer{}, fail: fail, block: !fail}
		o := NewOutput(context.Background(), s, nil)
		o.Delta("First. ")
		<-s.started
		if !fail {
			o.Cancel()
		}
		done := make(chan error, 1)
		go func() { done <- o.Finish() }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("failure/cancel accepted")
			}
		case <-time.After(time.Second):
			t.Fatal("output did not drain after cancellation")
		}
	}
}

func TestLongSpeechChunksPreserveEveryWord(t *testing.T) {
	s := &outputService{player: &outputPlayer{}}
	o := NewOutput(context.Background(), s, nil)
	text := strings.Repeat("a meaningful word ", 100) + "last word."
	if err := o.Delta(text); err != nil {
		t.Fatal(err)
	}
	if err := o.Finish(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.text, " ") != text {
		t.Fatal("chunking lost or split words")
	}
	for _, chunk := range s.text {
		if len(chunk) > 400 {
			t.Fatal("unbounded speech chunk")
		}
	}
}

func TestProviderCuesSurviveEveryStreamBoundary(t *testing.T) {
	input := "<think>[happy] secret</think>[whispering] Hello. [leans in] [laugh] Bye."
	for split := 0; split <= len(input); split++ {
		p := Projection{cues: map[string]bool{"whispering": true, "laugh": true}}
		got := p.Feed(input[:split], false) + p.Feed(input[split:], false) + p.Feed("", true)
		if got != "[whispering] Hello.  [laugh] Bye." {
			t.Fatalf("split %d: %q", split, got)
		}
	}
}

type expressiveService struct{ *outputService }

func (*expressiveService) DeliveryCues() []string       { return []string{"happy", "laugh"} }
func (*expressiveService) DeliveryInstructions() string { return "Use laugh for amusement." }
func TestDeliveryPromptAndOutput(t *testing.T) {
	s := &expressiveService{&outputService{player: &outputPlayer{}}}
	prompt := DeliveryPrompt(s)
	if !strings.Contains(prompt, "[happy], [laugh]") || !strings.Contains(prompt, "Use laugh") {
		t.Fatal(prompt)
	}
	if DeliveryPrompt(s.outputService) != "" {
		t.Fatal("plain provider received cues")
	}
	o := NewOutput(context.Background(), s, nil)
	if err := o.Delta("[happy] Hello. [made up] Bye."); err != nil {
		t.Fatal(err)
	}
	if err := o.Finish(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.text, " "); got != "[happy] Hello. Bye." {
		t.Fatal(got)
	}
}
