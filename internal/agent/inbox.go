package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/keshon/gektor/internal/llm"
)

var ErrInputClosed = errors.New("agent run has finished")

// Inbox accepts steering from another goroutine. Only the agent loop drains
// it: messages cannot interrupt an assistant/tool-result group.
type Inbox struct {
	mu       sync.Mutex
	pending  []llm.Message
	closed   bool
	steering bool
	claimed  int
}

func (i *Inbox) Send(text string, images ...string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return ErrInputClosed
	}
	if strings.TrimSpace(text) == "" && len(images) == 0 {
		return fmt.Errorf("message is empty")
	}
	if len(i.pending) >= 64 {
		return fmt.Errorf("64 messages are already waiting for the agent")
	}
	i.pending = append(i.pending, llm.Message{Role: llm.RoleUser, Content: text, Images: append([]string(nil), images...)})
	return nil
}

func (i *Inbox) Len() int {
	if i == nil {
		return 0
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.pending)
}

// Latest returns a display snapshot without giving the caller ownership of
// pending attachments. Recall races atomically with the loop's take operation.
func (i *Inbox) Latest() (llm.Message, int) {
	msg, count, _ := i.Preview()
	return msg, count
}

// Preview returns the latest input, total count and non-recallable count atomically.
func (i *Inbox) Preview() (llm.Message, int, int) {
	if i == nil {
		return llm.Message{}, 0, 0
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.pending) == 0 {
		return llm.Message{}, 0, 0
	}
	msg := i.pending[len(i.pending)-1]
	msg.Images = append([]string(nil), msg.Images...)
	return msg, len(i.pending), i.claimed
}

func (i *Inbox) RecallLatest() (llm.Message, bool) {
	if i == nil {
		return llm.Message{}, false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.pending) == i.claimed {
		return llm.Message{}, false
	}
	last := len(i.pending) - 1
	msg := i.pending[last]
	i.pending[last] = llm.Message{}
	i.pending = i.pending[:last]
	return msg, true
}

func (i *Inbox) start() {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = false
}

// take closes atomically with the final empty check. A concurrent sender is
// either included in this run or told explicitly that it has finished.
func (i *Inbox) take(finishing bool) []llm.Message {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if finishing && len(i.pending) == 0 {
		i.closed = true
	}
	result := i.pending
	i.pending = nil
	i.claimed = 0
	return result
}

func (i *Inbox) stop() []llm.Message {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	result := i.pending
	i.pending = nil
	i.claimed = 0
	return result
}

func (a *Agent) acceptInputs(history []llm.Message, inputs []llm.Message, st *runState) []llm.Message {
	if len(inputs) == 0 {
		return history
	}
	history = append(history, inputs...)
	st.repeatSignature, st.repeatCount = "", 0
	st.observations, st.observationWarning = nil, false
	st.emptyFinishRetried = false
	a.saveState(history)
	for _, input := range inputs {
		if a.cfg.OnInput != nil {
			a.cfg.OnInput(input)
		}
	}
	return history
}

// SetSteering changes admission of future tool calls, never cancels running tools.
func (i *Inbox) SetSteering(enabled bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.steering = enabled
}

// admit linearizes tool admission with Send and RecallLatest. yielded belongs
// to one tool group and is guarded by this inbox's mutex.
func (i *Inbox) admit(yielded *bool) bool {
	if i == nil {
		return true
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.steering && len(i.pending) > 0 {
		*yielded = true
		i.claimed = len(i.pending)
	}
	return !*yielded
}
