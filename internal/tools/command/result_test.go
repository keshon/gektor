package command

import (
	"testing"
	"time"
)

func TestForegroundTimeout(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seconds *int
		want    time.Duration
	}{
		{name: "default", want: 120 * time.Second},
		{name: "minimum", seconds: new(1), want: time.Second},
		{name: "explicit above default", seconds: new(300), want: 5 * time.Minute},
		{name: "maximum", seconds: new(600), want: 10 * time.Minute},
		{name: "zero", seconds: new(0)},
		{name: "negative", seconds: new(-1)},
		{name: "above maximum", seconds: new(601)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Timeout(tc.seconds)
			if tc.want == 0 {
				if err == nil {
					t.Fatalf("invalid timeout accepted: %v", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("timeout = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}
