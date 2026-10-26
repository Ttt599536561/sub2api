//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// geminiAbortingStreamBody 先吐出 data，读完后返回 abortErr；onAbort 用于模拟客户端断开时请求 context 被取消。
type geminiAbortingStreamBody struct {
	data     *bytes.Reader
	abortErr error
	onAbort  func()
}

func (b *geminiAbortingStreamBody) Read(p []byte) (int, error) {
	n, err := b.data.Read(p)
	if err == io.EOF {
		if b.onAbort != nil {
			b.onAbort()
		}
		return n, b.abortErr
	}
	return n, err
}

func (b *geminiAbortingStreamBody) Close() error { return nil }

const (
	geminiAbortTextChunk  = `data: {"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"}}]}` + "\n\n"
	geminiAbortFinalChunk = `data: {"candidates":[{"content":{"parts":[{"text":" world"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}` + "\n\n"
	geminiAbortImageChunk = `data: {"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"totalTokenCount":1300}}` + "\n\n"
	geminiAbortUsageChunk = `data: {"candidates":[{"content":{"parts":[{"text":"drawing"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}` + "\n\n"
)

// newGeminiAbortingStreamService 返回的上游流在吐完 sse 后中断；clientGone=true 时中断伴随请求 context 取消。
func newGeminiAbortingStreamService(t *testing.T, path string, reqBody []byte, sse string, clientGone bool) (*GeminiMessagesCompatService, *gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	reqCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(reqBody)).WithContext(reqCtx)

	body := &geminiAbortingStreamBody{data: bytes.NewReader([]byte(sse)), abortErr: io.ErrUnexpectedEOF}
	if clientGone {
		body.abortErr = context.Canceled
		body.onAbort = cancel
	}
	svc, httpStub := newGeminiVertexAPIKeyTestService()
	httpStub.response = &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	return svc, c, rec
}

func TestGeminiForwardNative_ClientDisconnectAfterFinalChunkBillsCollectedUsage(t *testing.T) {
	reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	svc, c, _ := newGeminiAbortingStreamService(t, "/v1beta/models/gemini-3-flash-preview:streamGenerateContent", reqBody, geminiAbortTextChunk+geminiAbortFinalChunk, true)

	result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-flash-preview", "streamGenerateContent", true, reqBody)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Zero(t, result.ImageCount)
}

func TestGeminiForwardNative_ClientDisconnectWithoutUsageStaysUnbilled(t *testing.T) {
	reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	svc, c, _ := newGeminiAbortingStreamService(t, "/v1beta/models/gemini-3-flash-preview:streamGenerateContent", reqBody, geminiAbortTextChunk, true)

	result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-flash-preview", "streamGenerateContent", true, reqBody)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
}

func TestGeminiForwardNative_UpstreamReadErrorWithLiveClientStaysError(t *testing.T) {
	reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	svc, c, _ := newGeminiAbortingStreamService(t, "/v1beta/models/gemini-3-flash-preview:streamGenerateContent", reqBody, geminiAbortTextChunk+geminiAbortFinalChunk, false)

	result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-flash-preview", "streamGenerateContent", true, reqBody)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Nil(t, result)
}

func TestGeminiForwardNative_ClientDisconnectBillsOnlyDeliveredImages(t *testing.T) {
	reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw a cat"}]}]}`)

	t.Run("image delivered before disconnect", func(t *testing.T) {
		svc, c, _ := newGeminiAbortingStreamService(t, "/v1beta/models/gemini-3-pro-image:streamGenerateContent", reqBody, geminiAbortImageChunk, true)
		result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-pro-image", "streamGenerateContent", true, reqBody)
		require.NoError(t, err)
		require.True(t, result.ClientDisconnect)
		require.Equal(t, 1, result.ImageCount)
	})

	t.Run("disconnect before the image arrives", func(t *testing.T) {
		svc, c, _ := newGeminiAbortingStreamService(t, "/v1beta/models/gemini-3-pro-image:streamGenerateContent", reqBody, geminiAbortUsageChunk, true)
		result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-pro-image", "streamGenerateContent", true, reqBody)
		require.NoError(t, err)
		require.True(t, result.ClientDisconnect)
		require.Zero(t, result.ImageCount)
		require.Equal(t, 2, result.Usage.OutputTokens)
	})
}

func TestGeminiCompatForward_ClientDisconnectAfterFinalChunkBillsCollectedUsage(t *testing.T) {
	reqBody := []byte(`{"model":"gemini-3-flash-preview","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	t.Run("client gone", func(t *testing.T) {
		svc, c, _ := newGeminiAbortingStreamService(t, "/v1/messages", reqBody, geminiAbortTextChunk+geminiAbortFinalChunk, true)
		result, err := svc.Forward(context.Background(), c, geminiVertexAPIKeyTestAccount(""), reqBody)
		require.NoError(t, err)
		require.True(t, result.ClientDisconnect)
		require.Equal(t, 10, result.Usage.InputTokens)
		require.Equal(t, 5, result.Usage.OutputTokens)
	})

	t.Run("upstream read error with live client", func(t *testing.T) {
		svc, c, _ := newGeminiAbortingStreamService(t, "/v1/messages", reqBody, geminiAbortTextChunk+geminiAbortFinalChunk, false)
		result, err := svc.Forward(context.Background(), c, geminiVertexAPIKeyTestAccount(""), reqBody)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.Nil(t, result)
	})
}

func TestGeminiForwardAsChatCompletions_ClientDisconnectAfterFinalChunkBillsCollectedUsage(t *testing.T) {
	reqBody := []byte(`{"model":"gemini-3-flash-preview","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	t.Run("client gone", func(t *testing.T) {
		svc, c, _ := newGeminiAbortingStreamService(t, "/v1/chat/completions", reqBody, geminiAbortTextChunk+geminiAbortFinalChunk, true)
		result, err := svc.ForwardAsChatCompletions(context.Background(), c, geminiVertexAPIKeyTestAccount(""), reqBody)
		require.NoError(t, err)
		require.True(t, result.ClientDisconnect)
		require.Equal(t, 10, result.Usage.InputTokens)
		require.Equal(t, 5, result.Usage.OutputTokens)
	})

	t.Run("client gone before usage", func(t *testing.T) {
		svc, c, _ := newGeminiAbortingStreamService(t, "/v1/chat/completions", reqBody, geminiAbortTextChunk, true)
		result, err := svc.ForwardAsChatCompletions(context.Background(), c, geminiVertexAPIKeyTestAccount(""), reqBody)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, result)
	})
}

func TestGeminiForwardNative_ClientDisconnectAfterFinishKeepsImageFallback(t *testing.T) {
	// 结束分片已到达但图片不以 inlineData 回吐时，断开与正常完成同口径按模型名兜底。
	finishWithoutInline := `data: {"candidates":[{"content":{"parts":[{"fileData":{"mimeType":"image/png","fileUri":"gs://bucket/cat.png"}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"totalTokenCount":1300}}` + "\n\n"
	reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw a cat"}]}]}`)
	svc, c, _ := newGeminiAbortingStreamService(t, "/v1beta/models/gemini-3-pro-image:streamGenerateContent", reqBody, finishWithoutInline, true)

	result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-pro-image", "streamGenerateContent", true, reqBody)
	require.NoError(t, err)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 1, result.ImageCount)
}

func TestGeminiForwardNative_ClientDisconnectIgnoresTruncatedTrailingLine(t *testing.T) {
	complete := `data: {"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":300,"totalTokenCount":1300}}` + "\n\n"
	truncated := `data: {"candidates":[{"content":{"parts":[{"text":" more"}],"role":"model"}}],"usageMetadata":{`
	reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	svc, c, _ := newGeminiAbortingStreamService(t, "/v1beta/models/gemini-3-flash-preview:streamGenerateContent", reqBody, complete+truncated, true)

	result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-flash-preview", "streamGenerateContent", true, reqBody)
	require.NoError(t, err)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 1000, result.Usage.InputTokens)
	require.Equal(t, 300, result.Usage.OutputTokens)
}

func TestGeminiForwardAsChatCompletions_ClientDisconnectImageCount(t *testing.T) {
	t.Run("disconnect before finish bills no image", func(t *testing.T) {
		reqBody := []byte(`{"model":"gemini-3-pro-image","stream":true,"messages":[{"role":"user","content":"draw a cat"}]}`)
		svc, c, _ := newGeminiAbortingStreamService(t, "/v1/chat/completions", reqBody, geminiAbortUsageChunk, true)
		result, err := svc.ForwardAsChatCompletions(context.Background(), c, geminiVertexAPIKeyTestAccount(""), reqBody)
		require.NoError(t, err)
		require.True(t, result.ClientDisconnect)
		require.Zero(t, result.ImageCount)
		require.Equal(t, 2, result.Usage.OutputTokens)
	})

	t.Run("disconnect after finish keeps completed-request image count", func(t *testing.T) {
		reqBody := []byte(`{"model":"gemini-3-pro-image","stream":true,"messages":[{"role":"user","content":"draw a cat"}]}`)
		svc, c, _ := newGeminiAbortingStreamService(t, "/v1/chat/completions", reqBody, geminiAbortImageChunk, true)
		result, err := svc.ForwardAsChatCompletions(context.Background(), c, geminiVertexAPIKeyTestAccount(""), reqBody)
		require.NoError(t, err)
		require.True(t, result.ClientDisconnect)
		require.Equal(t, 1, result.ImageCount)
	})
}

// geminiFailingResponseWriter 模拟客户端已断开：所有写入失败。
type geminiFailingResponseWriter struct {
	header http.Header
}

func (w *geminiFailingResponseWriter) Header() http.Header       { return w.header }
func (w *geminiFailingResponseWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (w *geminiFailingResponseWriter) WriteHeader(int)           {}
func (w *geminiFailingResponseWriter) Flush()                    {}

func TestGeminiForwardAsChatCompletions_WriteFailureOnImageModelBillsNoImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reqBody := []byte(`{"model":"gemini-3-pro-image","stream":true,"messages":[{"role":"user","content":"draw a cat"}]}`)
	c, _ := gin.CreateTestContext(&geminiFailingResponseWriter{header: http.Header{}})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	svc, httpStub := newGeminiVertexAPIKeyTestService()
	httpStub.response = &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(geminiAbortUsageChunk))),
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, geminiVertexAPIKeyTestAccount(""), reqBody)
	require.NoError(t, err)
	require.True(t, result.ClientDisconnect)
	require.Zero(t, result.ImageCount)
}

func TestGeminiCompatForward_ClientDisconnectBeforeFinishBillsNoImage(t *testing.T) {
	inlineWithoutFinish := `data: {"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}],"role":"model"}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"totalTokenCount":1300}}` + "\n\n"
	reqBody := []byte(`{"model":"gemini-3-pro-image","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"draw a cat"}]}`)
	svc, c, _ := newGeminiAbortingStreamService(t, "/v1/messages", reqBody, inlineWithoutFinish, true)

	result, err := svc.Forward(context.Background(), c, geminiVertexAPIKeyTestAccount(""), reqBody)
	require.NoError(t, err)
	require.True(t, result.ClientDisconnect)
	require.Zero(t, result.ImageCount)
	require.Equal(t, 1290, result.Usage.OutputTokens)
}

func TestGeminiForwardNative_WriteFailureDoesNotBillUndeliveredImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name  string
		chunk string
	}{
		{name: "before_finish", chunk: `data: {"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}],"role":"model"}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"totalTokenCount":1300}}` + "\n\n"},
		{name: "finish_write_failed", chunk: geminiAbortImageChunk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw a cat"}]}]}`)
			c, _ := gin.CreateTestContext(&geminiFailingResponseWriter{header: http.Header{}})
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-pro-image:streamGenerateContent", bytes.NewReader(reqBody))
			svc, httpStub := newGeminiVertexAPIKeyTestService()
			httpStub.response = &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(bytes.NewReader([]byte(tc.chunk))),
			}

			result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-pro-image", "streamGenerateContent", true, reqBody)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, result.ImageCount, "an image rejected by the downstream writer was not delivered")
			require.True(t, result.ClientDisconnect)
			require.Equal(t, 10, result.Usage.InputTokens, "preserve upstream token usage collected before the write failed")
			require.Equal(t, 1290, result.Usage.OutputTokens)
		})
	}
}

type geminiNthFailResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	calls  int
	failAt int
}

func (w *geminiNthFailResponseWriter) Header() http.Header { return w.header }
func (w *geminiNthFailResponseWriter) WriteHeader(int)     {}
func (w *geminiNthFailResponseWriter) Flush()              {}
func (w *geminiNthFailResponseWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls >= w.failAt {
		return 0, io.ErrClosedPipe
	}
	return w.body.Write(p)
}

func TestGeminiForwardNative_ImageBillingRequiresCompleteSSEEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inlineWithoutFinish := `data: {"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}],"role":"model"}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"totalTokenCount":1300}}` + "\n\n"
	fileImageFinish := `data: {"candidates":[{"content":{"parts":[{"fileData":{"mimeType":"image/png","fileUri":"gs://bucket/cat.png"}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"totalTokenCount":1300}}` + "\n\n"
	for _, tc := range []struct {
		name       string
		stream     string
		failAt     int
		wantImages int
		wantOutput int
	}{
		{name: "inline_image_delimiter_failed", stream: geminiAbortImageChunk, failAt: 2, wantImages: 0, wantOutput: 1290},
		{name: "file_image_finish_delimiter_failed", stream: fileImageFinish, failAt: 2, wantImages: 0, wantOutput: 1290},
		{name: "completed_image_before_next_write_failed", stream: geminiAbortImageChunk + geminiAbortFinalChunk, failAt: 3, wantImages: 1, wantOutput: 5},
		{name: "completed_image_before_terminal_write_failed", stream: inlineWithoutFinish + geminiAbortFinalChunk, failAt: 3, wantImages: 1, wantOutput: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw a cat"}]}]}`)
			writer := &geminiNthFailResponseWriter{header: http.Header{}, failAt: tc.failAt}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-pro-image:streamGenerateContent", bytes.NewReader(reqBody))
			svc, httpStub := newGeminiVertexAPIKeyTestService()
			httpStub.response = &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(bytes.NewReader([]byte(tc.stream))),
			}
			result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-pro-image", "streamGenerateContent", true, reqBody)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.ClientDisconnect)
			require.Equal(t, tc.wantImages, result.ImageCount, "only complete SSE events count as delivered images")
			require.Equal(t, tc.failAt, writer.calls)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, tc.wantOutput, result.Usage.OutputTokens, "upstream token usage survives downstream framing failure")
			if tc.wantImages == 0 {
				require.NotContains(t, writer.body.String(), "\n\n")
			} else {
				require.Contains(t, writer.body.String(), "\n\n")
			}
		})
	}
}

func TestGeminiForwardNative_CleanEOFWithoutDelimiterPreservesImageBilling(t *testing.T) {
	// Preserve clean-EOF tail compatibility by completing its SSE delimiter
	// before billing the image; do not discard otherwise valid upstream output.
	for _, tc := range []struct{ name, suffix string }{{"without_newline", ""}, {"single_newline", "\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw a cat"}]}]}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/private-image-alias:streamGenerateContent", bytes.NewReader(reqBody))
			svc, httpStub := newGeminiVertexAPIKeyTestService()
			chunk := append(bytes.TrimRight([]byte(geminiAbortImageChunk), "\n"), []byte(tc.suffix)...)
			httpStub.response = &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(bytes.NewReader(chunk)),
			}
			result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "private-image-alias", "streamGenerateContent", true, reqBody)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.ClientDisconnect)
			require.Equal(t, 1, result.ImageCount, "the alias must use observed images, not a model-name fallback")
			require.Equal(t, 1290, result.Usage.OutputTokens)
			require.Equal(t, geminiAbortImageChunk, rec.Body.String(), "clean EOF must complete exactly one SSE event")
		})
	}
}

func TestGeminiForwardNative_CleanEOFDelimiterFailureDoesNotBillImage(t *testing.T) {
	for _, tc := range []struct{ name, suffix string }{{"without_newline", ""}, {"single_newline", "\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw a cat"}]}]}`)
			writer := &geminiNthFailResponseWriter{header: http.Header{}, failAt: 2}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-pro-image:streamGenerateContent", bytes.NewReader(reqBody))
			svc, httpStub := newGeminiVertexAPIKeyTestService()
			chunk := append(bytes.TrimRight([]byte(geminiAbortImageChunk), "\n"), []byte(tc.suffix)...)
			httpStub.response = &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(bytes.NewReader(chunk)),
			}
			result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-pro-image", "streamGenerateContent", true, reqBody)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, result.ImageCount)
			require.True(t, result.ClientDisconnect)
			require.Equal(t, 2, writer.calls)
			require.NotContains(t, writer.body.String(), "\n\n")
			require.Equal(t, 1290, result.Usage.OutputTokens)
		})
	}
}

func TestGeminiForwardNative_CanceledAtCleanEOFDoesNotCompletePendingEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw a cat"}]}]}`)
	writer := &geminiNthFailResponseWriter{header: http.Header{}, failAt: 99}
	c, _ := gin.CreateTestContext(writer)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-pro-image:streamGenerateContent", bytes.NewReader(reqBody)).WithContext(ctx)
	svc, httpStub := newGeminiVertexAPIKeyTestService()
	httpStub.response = &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       &geminiAbortingStreamBody{data: bytes.NewReader(bytes.TrimRight([]byte(geminiAbortImageChunk), "\n")), abortErr: io.EOF, onAbort: cancel},
	}
	result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), "gemini-3-pro-image", "streamGenerateContent", true, reqBody)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Zero(t, result.ImageCount)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 1, writer.calls, "cancellation must not trigger an EOF completion write")
	require.Equal(t, 1290, result.Usage.OutputTokens)
}
