//go:build unit

package service

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAntigravityStreamReview_ImageUsageMatchesWireAndBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fragmented := range []bool{false, true} {
		shape := "complete usage"
		if fragmented {
			shape = "fragmented usage"
		}
		for _, disconnected := range []bool{false, true} {
			name := shape + "/connected"
			if disconnected {
				name = shape + "/disconnected drain"
			}
			t.Run(name, func(t *testing.T) {
				svc := newAntigravityTestService(&config.Config{})
				c, rec := antigravityProtocolReviewContext()
				if disconnected {
					c.Writer = &antigravityFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
				}
				chunks := []string{
					`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":3,"candidatesTokenCount":1000,"thoughtsTokenCount":2,"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":8},{"modality":"IMAGE","tokenCount":992}]}}`,
				}
				if fragmented {
					chunks = []string{
						`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}}],"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":3,"candidatesTokenCount":1}}`,
						`{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":1000,"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":8},{"modality":"IMAGE","tokenCount":992}]}}`,
						`{"usageMetadata":{"thoughtsTokenCount":2}}`,
					}
				}
				resp := antigravityProtocolReviewResponse(chunks...)
				result, err := svc.handleClaudeStreamingResponse(c, resp, time.Now(), "gemini-3-pro-image")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, disconnected, result.clientDisconnect)
				if !disconnected {
					require.Contains(t, rec.Body.String(), `"image_output_tokens":992`)
				}
				require.Equal(t, 7, result.usage.InputTokens)
				require.Equal(t, 3, result.usage.CacheReadInputTokens)
				require.Equal(t, 1002, result.usage.OutputTokens)
				require.Equal(t, 992, result.usage.ImageOutputTokens, "billing must retain the same image token split sent in Claude usage")
			})
		}
	}
}

func TestAntigravityStreamReview_DonePrecedesReadError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, done := range []bool{false, true} {
		name := "before done"
		if done {
			name = "after done"
		}
		t.Run(name, func(t *testing.T) {
			svc := newAntigravityTestService(&config.Config{})
			c, rec := antigravityProtocolReviewContext()
			content := antigravityProtocolReviewResponse(
				`{"candidates":[{"content":{"parts":[{"text":"complete answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20}}`,
			)
			terminator := ""
			if done {
				terminator = "data: [DONE]\n\n"
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(io.MultiReader(
				content.Body,
				strings.NewReader(terminator),
				antigravityStreamReviewReadError{},
			))}
			result, err := svc.handleClaudeStreamingResponse(c, resp, time.Now(), "claude-sonnet-4-6")
			if !done {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF, "finishReason alone must not hide a real upstream read error")
				require.Nil(t, result)
				require.Contains(t, rec.Body.String(), "event: error")
				require.NotContains(t, rec.Body.String(), "event: message_stop\n")
				return
			}
			require.NoError(t, err, "[DONE] must terminate a completed stream before a later transport error")
			require.NotNil(t, result)
			require.Equal(t, 10, result.usage.InputTokens)
			require.Equal(t, 20, result.usage.OutputTokens)
			require.Equal(t, 1, strings.Count(rec.Body.String(), "event: message_stop\n"))
			require.NotContains(t, rec.Body.String(), "event: error")
		})
	}
}

func TestAntigravityStreamReview_DoneClosesOpenUpstreamBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAntigravityTestService(&config.Config{})
	c, rec := antigravityProtocolReviewContext()
	content := antigravityProtocolReviewResponse(
		`{"candidates":[{"content":{"parts":[{"text":"complete answer"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1}}`,
		`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`,
		`{"usageMetadata":{"candidatesTokenCount":8,"thoughtsTokenCount":2}}`,
	)
	body := &antigravityStreamReviewOpenBody{
		content:       io.MultiReader(content.Body, strings.NewReader("data: [DONE]\n\n")),
		closed:        make(chan struct{}),
		reading:       make(chan struct{}),
		readUnblocked: make(chan struct{}),
	}
	defer body.Close()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	type outcome struct {
		result *antigravityStreamResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := svc.handleClaudeStreamingResponse(c, resp, time.Now(), "claude-sonnet-4-6")
		finished <- outcome{result, err}
	}()
	var got outcome
	select {
	case got = <-finished:
	case <-time.After(5 * time.Second):
		_ = body.Close()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("stream handler did not stop after the test closed its upstream body")
		}
		t.Fatal("stream handler waited for EOF after [DONE]")
	}
	require.NoError(t, got.err)
	require.NotNil(t, got.result)
	require.Equal(t, 10, got.result.usage.InputTokens)
	require.Equal(t, 10, got.result.usage.OutputTokens, "usage arriving after finishReason must be retained before [DONE]")
	require.Contains(t, rec.Body.String(), `"stop_reason":"max_tokens"`)
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: message_stop\n"))
	require.NotContains(t, rec.Body.String(), "event: error")
	select {
	case <-body.closed:
	default:
		t.Fatal("completed stream did not close the upstream body")
	}
	select {
	case <-body.reading:
		select {
		case <-body.readUnblocked:
		case <-time.After(5 * time.Second):
			t.Fatal("closing the upstream body left its scanner read blocked")
		}
	default:
		// If the scanner has not started another Read, the closed body ensures
		// any future Read returns immediately instead of leaking a goroutine.
	}
}

type antigravityStreamReviewReadError struct{}

func (antigravityStreamReviewReadError) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

type antigravityStreamReviewOpenBody struct {
	content       io.Reader
	closed        chan struct{}
	reading       chan struct{}
	readUnblocked chan struct{}
	closeOnce     sync.Once
}

func (b *antigravityStreamReviewOpenBody) Read(p []byte) (int, error) {
	n, err := b.content.Read(p)
	if n != 0 || err != io.EOF {
		return n, err
	}
	close(b.reading)
	<-b.closed
	close(b.readUnblocked)
	return 0, io.ErrClosedPipe
}

func (b *antigravityStreamReviewOpenBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}
