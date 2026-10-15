//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"testing"
)

type mainAuditModelsBody struct{ closed bool }

func (b *mainAuditModelsBody) Read(p []byte) (int, error) {
	return copy(p, `{"models":[]}`), io.ErrUnexpectedEOF
}
func (b *mainAuditModelsBody) Close() error { b.closed = true; return nil }
func TestMainAuditModelsReadFailureDoesNotReturnSuccess(t *testing.T) {
	body := &mainAuditModelsBody{}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}}}
	svc := newGeminiModelsEndpointsTestService(nil, upstream)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://generativelanguage.googleapis.com/v1beta/models", nil)
	require.NoError(t, err)
	result, err := svc.doGeminiModelsGET(req, &Account{ID: 8, Platform: PlatformGemini})
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Nil(t, result, "an incomplete transport response must not be forwarded as a successful model catalog")
	require.True(t, body.closed)
}
