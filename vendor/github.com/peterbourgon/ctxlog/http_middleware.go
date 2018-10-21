package ctxlog

import (
	"net/http"
	"time"

	"github.com/go-kit/kit/log"
)

// HTTPMiddleware uses the Logger to implement basic structured request logging.
type HTTPMiddleware struct {
	next   http.Handler
	logger log.Logger
}

// NewHTTPMiddleware wraps an http.Handler and a log.Logger,
// and performs structured request logging.
func NewHTTPMiddleware(next http.Handler, logger log.Logger) *HTTPMiddleware {
	return &HTTPMiddleware{next, logger}
}

// ServeHTTP implements http.Handler.
func (mw *HTTPMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		iw          = &interceptingWriter{http.StatusOK, w}
		ctx, ctxlog = New(r.Context(), "http_method", r.Method, "http_path", r.URL.Path)
	)

	defer func(begin time.Time) {
		ctxlog.Log("http_status_code", iw.code, "http_duration", time.Since(begin))
		mw.logger.Log(ctxlog.Keyvals()...)
	}(time.Now())

	mw.next.ServeHTTP(iw, r.WithContext(ctx))
}

type interceptingWriter struct {
	code int
	http.ResponseWriter
}

func (iw *interceptingWriter) WriteHeader(code int) {
	iw.code = code
	iw.ResponseWriter.WriteHeader(code)
}
