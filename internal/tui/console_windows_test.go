//go:build windows

package tui

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	xwindows "github.com/charmbracelet/x/windows"
	"golang.org/x/sys/windows"
)

func nativeKeyRecord(vk, char uint16, modifiers uint32) xwindows.InputRecord {
	r := xwindows.InputRecord{EventType: xwindows.KEY_EVENT}
	binary.LittleEndian.PutUint32(r.Event[0:4], 1)
	binary.LittleEndian.PutUint16(r.Event[4:6], 1)
	binary.LittleEndian.PutUint16(r.Event[6:8], vk)
	binary.LittleEndian.PutUint16(r.Event[10:12], char)
	binary.LittleEndian.PutUint32(r.Event[12:16], modifiers)
	return r
}

func decodeConsoleEvents(t *testing.T, input string) []uv.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	events := make(chan uv.Event, 64)
	reader := uv.NewTerminalReader(strings.NewReader(input), "")
	done := make(chan error, 1)
	go func() {
		done <- reader.StreamEvents(ctx, events)
		close(events)
	}()
	var result []uv.Event
	for event := range events {
		result = append(result, event)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return result
}

func TestNativeConsoleShiftEnterPreservesModifier(t *testing.T) {
	r := &consoleReader{}
	for _, record := range []xwindows.InputRecord{
		nativeKeyRecord('F', 'f', 0),
		nativeKeyRecord(xwindows.VK_RETURN, '\r', xwindows.SHIFT_PRESSED),
		nativeKeyRecord('S', 's', 0),
		nativeKeyRecord(xwindows.VK_RETURN, '\r', 0),
	} {
		r.encode(record)
	}
	events := decodeConsoleEvents(t, r.buffer.String())
	if len(events) != 4 {
		t.Fatalf("wrong event count: %#v", events)
	}
	m := sizeModel(t, testModel())
	m.state = stRunning
	for i, event := range events {
		key, ok := event.(uv.KeyPressEvent)
		if !ok {
			t.Fatalf("unexpected console event: %#v", event)
		}
		m.Update(tea.KeyPressMsg(key))
		if i == 2 && (m.input.Value() != "f\ns" || m.queued != "") {
			t.Fatal("native Shift+Enter submitted instead of inserting a newline")
		}
	}
	if m.queued != "f\ns" {
		t.Fatal("native plain Enter did not submit")
	}
}

func TestNativeConsoleDecodesPastedSurrogatePair(t *testing.T) {
	r := &consoleReader{}
	r.encode(nativeKeyRecord(0, 0xd83d, 0))
	r.encode(nativeKeyRecord(0, 0xde00, 0))
	events := decodeConsoleEvents(t, r.buffer.String())
	if len(events) != 1 {
		t.Fatalf("surrogate pair split into events: %#v", events)
	}
	key, ok := events[0].(uv.KeyPressEvent)
	if !ok || key.Text != "😀" {
		t.Fatalf("emoji corrupted: %#v", events)
	}
}

func TestConsoleReaderReadsRealControlNewline(t *testing.T) {
	handle := windows.Handle(os.Stdin.Fd())
	var original uint32
	if windows.GetConsoleMode(handle, &original) != nil {
		t.Skip("requires a console; run the compiled test binary in a terminal")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, restore, err := consoleInput(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil || mode&windows.ENABLE_VIRTUAL_TERMINAL_INPUT == 0 {
		t.Fatal("native console input disabled paste boundaries")
	}
	record := nativeKeyRecord('O', 15, xwindows.LEFT_CTRL_PRESSED)
	var written uint32
	writeInput := windows.NewLazySystemDLL("kernel32.dll").NewProc("WriteConsoleInputW")
	wrote, _, callErr := writeInput.Call(uintptr(handle), uintptr(unsafe.Pointer(&record)), 1, uintptr(unsafe.Pointer(&written)))
	if wrote == 0 || written != 1 {
		t.Fatalf("write console record: %v", callErr)
	}
	r := &consoleReader{ctx: ctx, handle: handle}
	var buf [256]byte
	n, err := r.Read(buf[:])
	if err != nil {
		t.Fatal(err)
	}
	events := decodeConsoleEvents(t, string(buf[:n]))
	if len(events) != 1 {
		t.Fatalf("unexpected native events: %#v", events)
	}
	key, ok := events[0].(uv.KeyPressEvent)
	if !ok || tea.KeyPressMsg(key).String() != "ctrl+o" {
		t.Fatalf("console lost Ctrl+O newline shortcut: %#v", events)
	}
	if mode == original {
		t.Fatal("console input mode did not change")
	}
}

func TestConsoleReaderReadsRealMultilinePaste(t *testing.T) {
	handle := windows.Handle(os.Stdin.Fd())
	var original uint32
	if windows.GetConsoleMode(handle, &original) != nil {
		t.Skip("requires a console; run the compiled test binary in a terminal")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, restore, err := consoleInput(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { restore() }()
	text := "first line\n/quit\nПривет 😀\n" + strings.Repeat("another line\n", 20)
	var records []xwindows.InputRecord
	for _, unit := range utf16.Encode([]rune("\x1b[200~" + text + "\x1b[201~")) {
		records = append(records, nativeKeyRecord(0, unit, 0))
	}
	var written uint32
	writeInput := windows.NewLazySystemDLL("kernel32.dll").NewProc("WriteConsoleInputW")
	wrote, _, callErr := writeInput.Call(uintptr(handle), uintptr(unsafe.Pointer(&records[0])), uintptr(len(records)), uintptr(unsafe.Pointer(&written)))
	if wrote == 0 || int(written) != len(records) {
		t.Fatalf("write paste records: %v", callErr)
	}
	r := &consoleReader{ctx: ctx, handle: handle}
	var trace bytes.Buffer
	reader := uv.NewTerminalReader(io.TeeReader(r, &trace), "")
	events := make(chan uv.Event, 8)
	done := make(chan error, 1)
	go func() {
		done <- reader.StreamEvents(ctx, events)
		close(events)
	}()
	pastes := 0
	unexpected := 0
	for event := range events {
		switch e := event.(type) {
		case uv.PasteStartEvent:
		case uv.PasteEvent:
			pastes++
			if e.Content != text {
				t.Errorf("native paste content changed: %q", e.Content)
			}
		case uv.PasteEndEvent:
			cancel()
		default:
			unexpected++
			if unexpected == 1 {
				t.Errorf("native paste escaped as input: %#v", event)
			}
		}
	}
	<-done
	if pastes != 1 {
		t.Fatalf("expected one complete paste, got %d; prefix: %q", pastes, trace.String()[:min(500, trace.Len())])
	}
	// Restore must not leave the PowerShell console in the application's mode.
	restore()
	restore = func() {}
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil || mode != original {
		t.Fatalf("console mode was not restored: %#x, want %#x: %v", mode, original, err)
	}
}

func TestNativeConsoleModeKeepsPasteAndNativeEvents(t *testing.T) {
	for _, original := range []uint32{0, windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_QUICK_EDIT_MODE, ^uint32(0)} {
		mode := nativeConsoleMode(original)
		required := uint32(windows.ENABLE_VIRTUAL_TERMINAL_INPUT | windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_MOUSE_INPUT | windows.ENABLE_WINDOW_INPUT)
		forbidden := uint32(windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_QUICK_EDIT_MODE)
		if mode&required != required || mode&forbidden != 0 {
			t.Fatalf("unsafe console mode: %#x -> %#x", original, mode)
		}
	}
}

func TestConsoleResizeUsesVisibleWindowAndIgnoresBufferChurn(t *testing.T) {
	r := &consoleReader{}
	info := windows.ConsoleScreenBufferInfo{
		Size:   windows.Coord{X: 200, Y: 9000},
		Window: windows.SmallRect{Left: 10, Top: 500, Right: 109, Bottom: 529},
	}
	r.encodeResize(info)
	// Buffer growth and a shifted viewport are not changes to layout size.
	info.Size.Y = 16000
	info.Window.Top, info.Window.Bottom = 600, 629
	r.encodeResize(info)
	info.Window.Right = 129
	r.encodeResize(info)
	events := decodeConsoleEvents(t, r.buffer.String())
	if len(events) != 2 {
		t.Fatalf("buffer churn generated resize events: %#v", events)
	}
	for i, width := range []int{100, 120} {
		e, ok := events[i].(uv.WindowSizeEvent)
		if !ok || e.Width != width || e.Height != 30 {
			t.Fatalf("resize used buffer dimensions: %#v", events[i])
		}
	}
}

func TestNativeConsoleMultilinePasteWaitsForExplicitEnter(t *testing.T) {
	// Exercise the native record encoder and the actual terminal decoder, not
	// just a synthetic PasteMsg. Include text that would execute local commands
	// or replace the active task if it escaped from the paste envelope.
	text := "first line\n/quit\n/new another task\nUnicode: Привет 😀\nlast line\n"
	r := &consoleReader{}
	for _, unit := range utf16.Encode([]rune("\x1b[200~" + text + "\x1b[201~")) {
		r.encode(nativeKeyRecord(0, unit, 0))
	}
	events := decodeConsoleEvents(t, r.buffer.String())
	for _, state := range []runState{stDone, stRunning, stAsk} {
		m := sizeModel(t, testModel())
		m.state = state
		pastes := 0
		for _, event := range events {
			switch e := event.(type) {
			case uv.PasteEvent:
				pastes++
				m.Update(tea.PasteMsg(e))
			case uv.PasteStartEvent:
				m.Update(tea.PasteStartMsg(e))
			case uv.PasteEndEvent:
				m.Update(tea.PasteEndMsg(e))
			default:
				t.Fatalf("paste escaped as ordinary input: %#v", event)
			}
		}
		if pastes != 1 || m.input.Value() != text || m.state != state || m.queued != "" || m.quit {
			t.Fatalf("paste submitted or changed task in state %d: %q", state, m.input.Value())
		}
		if state == stRunning {
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.input.Value() != "" || m.queued != strings.TrimSpace(text) {
				t.Fatal("explicit Enter did not submit the pasted draft")
			}
		}
	}
}

func TestConsoleReaderCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &consoleReader{ctx: ctx}
	_, err := r.Read(make([]byte, 8))
	if err != io.EOF {
		t.Fatalf("canceled read returned %v", err)
	}
}
