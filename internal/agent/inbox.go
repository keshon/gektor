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
	mu      sync.Mutex
	pending []llm.Message
	closed  bool
}

func (i *Inbox) Send(text string, images ...string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return ErrInputClosed
	}
	if strings.TrimSpace(text) == "" {
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
	return result
}

func (a *Agent) acceptInputs(history []llm.Message, inputs []llm.Message, st *runState) []llm.Message {
	if len(inputs) == 0 {
		return history
	}
	history = append(history, inputs...)
	st.repeatSignature, st.repeatCount = "", 0
	st.emptyFinishRetried = false
	a.saveState(history)
	for _, input := range inputs {
		if a.cfg.OnInput != nil {
			a.cfg.OnInput(input)
		}
	}
	return history
}
