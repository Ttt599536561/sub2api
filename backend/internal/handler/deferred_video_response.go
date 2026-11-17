package handler

import (
	"bytes"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Video creation is non-streaming. Do not expose its successful task ID until
// the durable billing identity and reservation transfer have both succeeded.
type deferredVideoResponse struct {
	gin.ResponseWriter
	header http.Header
	body   bytes.Buffer
	status int
	size   int
}

func newDeferredVideoResponse(w gin.ResponseWriter) *deferredVideoResponse {
	return &deferredVideoResponse{ResponseWriter: w, header: w.Header().Clone(), status: http.StatusOK, size: -1}
}
func (w *deferredVideoResponse) Header() http.Header { return w.header }
func (w *deferredVideoResponse) WriteHeader(code int) {
	if w.size < 0 {
		w.status = code
	}
}
func (w *deferredVideoResponse) WriteHeaderNow() {
	if w.size < 0 {
		w.size = 0
	}
}
func (w *deferredVideoResponse) Write(body []byte) (int, error) {
	w.WriteHeaderNow()
	n, err := w.body.Write(body)
	w.size += n
	return n, err
}
func (w *deferredVideoResponse) WriteString(body string) (int, error) { return w.Write([]byte(body)) }
func (w *deferredVideoResponse) Status() int                          { return w.status }
func (w *deferredVideoResponse) Size() int                            { return w.size }
func (w *deferredVideoResponse) Written() bool                        { return w.size >= 0 }
func (w *deferredVideoResponse) Flush()                               { w.WriteHeaderNow() }
func (w *deferredVideoResponse) commit() {
	if !w.Written() {
		return
	}
	for key, values := range w.header {
		w.ResponseWriter.Header()[key] = values
	}
	w.ResponseWriter.WriteHeader(w.status)
	w.ResponseWriter.WriteHeaderNow()
	_, _ = w.ResponseWriter.Write(w.body.Bytes())
}
