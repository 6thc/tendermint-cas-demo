# ctxlog

Create wide log events in Go programs.

When requests first hit your system, use the New constructor to create a
request logger and inject it into a context in one motion. During request
processing, use the From constructor to extract the logger from the context and
add keyvals. At the end of request processing, use Keyvals to get all of the
added data and report it somewhere.

```go
import (
	"github.com/go-kit/kit/log"
	"github.com/peterbourgon/ctxlog"
)

func handleRequest(ctx context.Context, ..., logger log.Logger) {
	subctx, ctxlogger := ctxlog.New(ctx)
	process(subctx)
	logger.Log(ctxlogger.Keyvals()...)
}
```

Package ctxlog comes with a default HTTP middleware that does this setup and
teardown work for you, and adds a few useful default keyvals.

```go
var h http.Handler
{
	h = myHandler
	h = ctxlog.NewHTTPMiddleware(h, logger)
}
```

