//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeCollectedGeminiParts_PreservesSignedPartBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name  string
		parts []map[string]any
		want  []any
	}{
		{
			name:  "consecutive signed text parts",
			parts: []map[string]any{{"text": "first", "thoughtSignature": "sig-1"}, {"text": "second", "thoughtSignature": "sig-2"}},
			want:  []any{map[string]any{"text": "first", "thoughtSignature": "sig-1"}, map[string]any{"text": "second", "thoughtSignature": "sig-2"}},
		},
		{
			name:  "unsigned text around signed part",
			parts: []map[string]any{{"text": "before"}, {"text": "signed", "thoughtSignature": "sig"}, {"text": "after"}},
			want:  []any{map[string]any{"text": "before"}, map[string]any{"text": "signed", "thoughtSignature": "sig"}, map[string]any{"text": "after"}},
		},
		{
			name:  "trailing empty signed part",
			parts: []map[string]any{{"text": "hello "}, {"text": "world"}, {"text": "", "thoughtSignature": "sig"}},
			want:  []any{map[string]any{"text": "hello world"}, map[string]any{"text": "", "thoughtSignature": "sig"}},
		},
		{
			name:  "ordinary unsigned chunks still merge",
			parts: []map[string]any{{"text": "hello "}, {"text": "world"}},
			want:  []any{map[string]any{"text": "hello world"}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeCollectedPartsToResponse(map[string]any{}, tt.parts)
			_, got, _ := getOrCreateGeminiParts(result)
			require.Equal(t, tt.want, got, "signed parts must be returned exactly as received so the client can replay them")
		})
	}
}
