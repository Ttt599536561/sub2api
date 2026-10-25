package middleware

import (
	"github.com/gin-gonic/gin"
	"strings"
)

// QQImageBindingPagePrivacy protects the initial HTML response, before the SPA
// can remove the short-lived request locator from the address bar. Other pages
// retain the existing referrer/cache policy.
func QQImageBindingPagePrivacy() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimRight(c.Request.URL.Path, "/") == "/qq/bind" {
			c.Writer = &qqBindingPrivacyWriter{ResponseWriter: c.Writer}
		}
		c.Next()
	}
}

type qqBindingPrivacyWriter struct{ gin.ResponseWriter }

func (w *qqBindingPrivacyWriter) protect() {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Del("ETag")
}
func (w *qqBindingPrivacyWriter) WriteHeader(status int) {
	w.protect()
	w.ResponseWriter.WriteHeader(status)
}
func (w *qqBindingPrivacyWriter) WriteHeaderNow() { w.protect(); w.ResponseWriter.WriteHeaderNow() }
func (w *qqBindingPrivacyWriter) Write(body []byte) (int, error) {
	w.protect()
	return w.ResponseWriter.Write(body)
}
func (w *qqBindingPrivacyWriter) WriteString(body string) (int, error) {
	w.protect()
	return w.ResponseWriter.WriteString(body)
}
