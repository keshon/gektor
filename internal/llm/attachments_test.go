package llm

import (
	"context"
	"testing"
)

func TestImageAttachmentsAreScopedAndStable(t *testing.T) {
	h := []Message{{Role: RoleUser, Images: []string{"a.png", "b.png"}}, {Role: RoleUser, Images: []string{"a.png"}}}
	ctx := WithAttachments(context.Background(), h)
	if got := Attachments(ctx); len(got) != 2 || got[0].ID != "attachment:1" || got[1].Name != "b.png" {
		t.Fatal(got)
	}
	h = append(h, Message{Role: RoleUser, Images: []string{"c.png"}})
	next := WithAttachments(context.Background(), h)
	if path, err := ResolveAttachment(next, "attachment:2"); err != nil || path != "b.png" {
		t.Fatal(path, err)
	}
	for _, id := range []string{"attachment:0", "attachment:01", "attachment:4", "attachment:../a.png", "a.png"} {
		if _, err := ResolveAttachment(next, id); err == nil {
			t.Fatal("accepted", id)
		}
	}
	if _, err := ResolveAttachment(context.Background(), "attachment:1"); err == nil {
		t.Fatal("leaked attachment across contexts")
	}
}
