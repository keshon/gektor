package documents

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTextReconstructionAcrossFields(t *testing.T) {
	want := []string{"", strings.Repeat("aЖ🙂e\u0301", 30), "", strings.Repeat("яx", 17), ""}
	for _, budget := range []int{4, 7, 32, MaxTextBytes} {
		got := make([]string, len(want))
		offset := 0
		for calls := 0; ; calls++ {
			parts, next, used, err := sliceText(want, offset, budget)
			if err != nil || parts == nil || used > budget || calls > 1000 {
				t.Fatalf("budget=%d offset=%d next=%d used=%d err=%v", budget, offset, next, used, err)
			}
			for i, part := range parts {
				if !utf8.ValidString(part) {
					t.Fatal("invalid UTF-8", part)
				}
				got[i] += part
			}
			if next == 0 {
				break
			}
			if next <= offset || used == 0 {
				t.Fatal("continuation did not advance")
			}
			offset = next
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("budget=%d: reconstructed fields differ", budget)
		}
	}
}

func TestTextBoundaryAndInvalidPositions(t *testing.T) {
	for _, offset := range []int{-1, 3} {
		if _, _, _, err := sliceText([]string{"Ж🙂"}, offset, 4); err == nil {
			t.Fatal("accepted offset", offset)
		}
	}
	parts, next, used, err := sliceText([]string{"Ж🙂"}, 2, 4)
	if err != nil || len(parts) != 1 || parts[0] != "" || next != 0 || used != 0 {
		t.Fatal(parts, next, used, err)
	}
	for _, offset := range []int{0, 1} {
		parts, _, _, err := sliceText([]string{"🙂🙂"}, offset, 3)
		if err != nil || parts != nil {
			t.Fatal("must defer an item when no character fits", parts, err)
		}
	}
	parts, next, used, err = sliceText([]string{"", ""}, 0, 0)
	if err != nil || len(parts) != 2 || next != 0 || used != 0 {
		t.Fatal("empty fields must complete", parts, next, used, err)
	}
}
