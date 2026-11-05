package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	textclipboard "github.com/atotto/clipboard"
	imageclipboard "github.com/keshon/gektor/internal/clipboard"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/llm"
)

const maxDraftImages = 8
const maxDraftImageBytes = 40 << 20

type clipboardMsg struct {
	path, text, session string
	revision            uint64
	err                 error
}

func (m *model) pasteClipboard(imageOnly bool) tea.Cmd {
	if m.clipboardBusy || m.activeInputOwner() != focusInput || m.state == stAsk {
		return nil
	}
	m.clipboardBusy = true
	m.fitBottom()
	session, revision := m.stateFile, m.attachmentEpoch
	return func() tea.Msg {
		out := clipboardMsg{session: session, revision: revision}
		data, err := imageclipboard.ReadImage()
		if !imageOnly && errors.Is(err, imageclipboard.ErrUnsupported) {
			err = nil
		}
		if err != nil {
			out.err = err
			return out
		}
		if len(data) == 0 {
			if imageOnly {
				out.err = fmt.Errorf("clipboard contains no image")
			} else {
				out.text, out.err = textclipboard.ReadAll()
				if out.err == nil {
					out = importPastedImage(out)
				}
			}
			return out
		}
		dir := gektor.Sub("images")
		if err = os.MkdirAll(dir, 0700); err != nil {
			out.err = err
			return out
		}
		file, err := os.CreateTemp(dir, ".clipboard-*.png")
		if err != nil {
			out.err = err
			return out
		}
		defer os.Remove(file.Name())
		defer file.Close()
		if _, err = file.Write(data); err != nil {
			out.err = err
			return out
		}
		out.path, out.err = llm.SnapshotImageFile(file, "screenshot.png", dir)

		return out
	}
}
func (m *model) acceptClipboard(msg clipboardMsg) {
	m.clipboardBusy = false
	defer m.fitBottom()
	if msg.session != m.stateFile || msg.revision != m.attachmentEpoch || m.activeInputOwner() != focusInput || m.state == stAsk {
		m.appendBlock(markerBlock("Clipboard capture discarded because the active draft changed. Paste again."))
		return
	}
	if msg.err != nil {
		m.appendBlock(errorBlock(msg.err.Error()))
		return
	}
	if msg.path != "" {
		images := append(append([]string(nil), m.draftImages...), msg.path)
		if err := validateAttachments(images); err != nil {
			m.appendBlock(errorBlock(err.Error()))
			return
		}
		m.draftImages = images
	} else if msg.text != "" {
		m.input.InsertString(msg.text)
	}
	m.fitInput()
	m.fitBottom()
}
func validateAttachments(images []string) error {
	if len(images) > maxDraftImages {
		return fmt.Errorf("at most %d images per message; remove an image with /detach N", maxDraftImages)
	}
	var total int64
	for _, path := range images {
		if err := llm.ValidateImage(path); err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		total += info.Size()
	}
	if total > maxDraftImageBytes {
		return fmt.Errorf("images exceed the 40 MiB message limit")
	}
	return nil
}
func (m *model) draftAttachments(text string) (string, []string, error) {
	if m.clipboardBusy {
		return "", nil, fmt.Errorf("wait for clipboard capture to finish")
	}
	if len(m.draftImages) > 0 && m.baseEnv.VisionKnown && !m.baseEnv.Vision {
		return "", nil, fmt.Errorf("this model does not support image input; select a vision model or remove the attachments")
	}
	clean, images, err := splitAttachments(m.ws, text)
	if err != nil {
		return "", nil, err
	}
	images = append(append([]string(nil), m.draftImages...), images...)
	if len(images) > 0 && m.baseEnv.VisionKnown && !m.baseEnv.Vision {
		return "", nil, fmt.Errorf("this model does not support image input; select a vision model or remove the attachments")
	}
	return clean, images, validateAttachments(images)
}
func (m *model) detachImage(arg string) {
	n, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || n < 1 || n > len(m.draftImages) {
		m.appendBlock(errorBlock("use /detach N with an attached image number"))
		return
	}
	m.draftImages = append(m.draftImages[:n-1], m.draftImages[n:]...)
	m.fitBottom()
}
func (m *model) attachmentLine() string {
	if m.clipboardBusy {
		return "Reading clipboard..."
	}
	var labels []string
	for i := range m.draftImages {
		labels = append(labels, fmt.Sprintf("[%d]", i+1))
	}
	return "Images " + strings.Join(labels, " ") + "  Alt+N remove"
}

func (m *model) hasQueued() bool { return m.queued != "" || len(m.queuedImages) > 0 }

func (m *model) queuedLabel() string {
	if len(m.queuedImages) > 0 {
		return fmt.Sprintf("%s [%d images]", m.queued, len(m.queuedImages))
	}
	return m.queued
}

// Only an explicit paste of a single absolute image path grants import access.
// Ordinary typed paths and agent tools retain the workspace boundary.
func pastedImagePath(text string) string {
	path := strings.TrimSpace(text)
	if len(path) >= 2 && ((path[0] == '"' && path[len(path)-1] == '"') || (path[0] == '\'' && path[len(path)-1] == '\'')) {
		path = path[1 : len(path)-1]
	}
	if strings.ContainsAny(path, "\r\n") || !filepath.IsAbs(path) || !llm.IsImagePath(path) {
		return ""
	}
	return path
}
func importPastedImage(msg clipboardMsg) clipboardMsg {
	path := pastedImagePath(msg.text)
	if path == "" {
		return msg
	}
	msg.path, msg.err = llm.SnapshotImage(path, gektor.Sub("images"))
	msg.text = ""
	return msg
}
func (m *model) pasteImagePath(text string) tea.Cmd {
	if m.clipboardBusy {
		m.appendBlock(errorBlock("Wait for the current image import, then paste again."))
		return nil
	}
	m.clipboardBusy = true
	m.fitBottom()
	msg := clipboardMsg{text: text, session: m.stateFile, revision: m.attachmentEpoch}
	return func() tea.Msg { return importPastedImage(msg) }
}
