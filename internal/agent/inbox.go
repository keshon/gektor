package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/keshon/gektor/internal/llm"
)

var ErrInputClosed = errors.New("agent run has finished")
var ErrInputReceived = errors.New("message was received by the agent and can no longer be changed")
var ErrInputMissing = errors.New("message is no longer in the queue")

// QueuedInput is a detached snapshot. Received inputs wait for active tools
// to finish and can only be read.
type QueuedInput struct {
	ID       uint64
	Message  llm.Message
	Received bool
}

// Inbox accepts steering from another goroutine. Only the agent loop drains
// it: messages cannot interrupt an assistant/tool-result group.
type Inbox struct {
	mu       sync.Mutex
	pending  []QueuedInput
	nextID   uint64
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
	i.nextID++
	i.pending = append(i.pending, QueuedInput{ID: i.nextID, Message: llm.Message{Role: llm.RoleUser, Content: text, Images: append([]string(nil), images...)}})
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
	msg := i.pending[len(i.pending)-1].Message
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
	msg := i.pending[last].Message
	i.pending[last] = QueuedInput{}
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
	result := make([]llm.Message, len(i.pending))
	for n, input := range i.pending {
		result[n] = input.Message
	}
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
	result := make([]llm.Message, len(i.pending))
	for n, input := range i.pending {
		result[n] = input.Message
	}
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

// Inputs returns messages in send order, with attachments owned by the caller.
func (i *Inbox) Inputs() []QueuedInput {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	result := append([]QueuedInput(nil), i.pending...)
	for n := range result {
		result[n].Received = n < i.claimed
		result[n].Message.Images = append([]string(nil), result[n].Message.Images...)
	}
	return result
}

// EditInput keeps order and attachments. Admission, consumption and edits
// share the same lock, so a successful edit cannot race receipt.
func (i *Inbox) EditInput(id uint64, text string) error {
	return i.changeInput(id, &text)
}
func (i *Inbox) RemoveInput(id uint64) error {
	return i.changeInput(id, nil)
}
func (i *Inbox) changeInput(id uint64, text *string) error {
	if i == nil {
		return ErrInputMissing
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for n := range i.pending {
		if i.pending[n].ID != id {
			continue
		}
		if n < i.claimed {
			return ErrInputReceived
		}
		if text != nil {
			if strings.TrimSpace(*text) == "" && len(i.pending[n].Message.Images) == 0 {
				return fmt.Errorf("message is empty")
			}
			i.pending[n].Message.Content = *text
		} else {
			copy(i.pending[n:], i.pending[n+1:])
			i.pending[len(i.pending)-1] = QueuedInput{}
			i.pending = i.pending[:len(i.pending)-1]
		}
		return nil
	}
	return ErrInputMissing
}
