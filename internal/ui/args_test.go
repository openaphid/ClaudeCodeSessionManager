package ui

import (
	"reflect"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"single flag", "--chrome", []string{"--chrome"}},
		{"multiple flags", "--chrome --model opus", []string{"--chrome", "--model", "opus"}},
		{"quoted value with space", `--add-dir "/tmp/with space"`, []string{"--add-dir", "/tmp/with space"}},
		{"single quotes", `--add-dir '/a b'`, []string{"--add-dir", "/a b"}},
		{"strip resume", "--resume --chrome", []string{"--chrome"}},
		{"strip continue short", "-c --chrome -r", []string{"--chrome"}},
		{"extra spaces", "  --chrome   --bare  ", []string{"--chrome", "--bare"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitArgs(tt.in)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitArgs(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}
