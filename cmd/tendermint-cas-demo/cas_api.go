package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/peterbourgon/ctxlog"
	tendermintabci "github.com/tendermint/tendermint/abci/types"
	tendermintrpcclient "github.com/tendermint/tendermint/rpc/client"
	tenderminttypes "github.com/tendermint/tendermint/types"
)

// CompareAndSwapAPI provides a simple HTTP API to a Tendermint client running
// the compare-and-swap key-value ABCI applciation.
type CompareAndSwapAPI struct {
	http.Handler
	client tendermintrpcclient.Client
}

// NewCompareAndSwapAPI returns a usable API calling out to the provided
// Tendermint client.
func NewCompareAndSwapAPI(client tendermintrpcclient.Client) *CompareAndSwapAPI {
	a := &CompareAndSwapAPI{
		client: client,
	}
	r := mux.NewRouter()
	r.StrictSlash(true)
	r.Methods("GET").Path("/{key}").HandlerFunc(a.handleGet)
	r.Methods("POST").Path("/{key}").HandlerFunc(a.handleSet)
	a.Handler = r
	return a
}

func (a *CompareAndSwapAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	if key == "" {
		respond(w, http.StatusBadRequest, apiResponse{Error: "no key"})
		return
	}

	result, err := a.client.ABCIQuery("", []byte(key))
	if err != nil {
		respond(w, http.StatusBadGateway, apiResponse{Key: key, Error: err.Error()})
		return
	}

	respond(w, http.StatusOK, apiResponse{
		Key:   key,
		Value: string(result.Response.Value),
		Info:  result.Response.Info,
		Log:   result.Response.Log,
	})
}

func (a *CompareAndSwapAPI) handleSet(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	if key == "" {
		respond(w, http.StatusBadRequest, apiResponse{Error: "no key"})
		return
	}

	if err := r.ParseForm(); err != nil {
		respond(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}

	var (
		old  = r.Form.Get("old")
		new  = r.Form.Get("new")
		data = fmt.Sprintf("%s:%s:%s", key, old, new)
	)

	// BroadcastTxAsync fires-and-forgets. BroadcastTxSync waits until CheckTx
	// is successful. BroadcastTxCommit waits until the transaction is included
	// in a signed block.
	result, err := a.client.BroadcastTxSync(tenderminttypes.Tx(data))
	if err != nil {
		respond(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}

	if result.Code != tendermintabci.CodeTypeOK {
		respond(w, http.StatusBadRequest, apiResponse{
			Error: fmt.Sprintf("result code %d", result.Code),
			Log:   result.Log,
		})
		return
	}

	respond(w, http.StatusOK, apiResponse{
		Key:   key,
		Value: new,
	})
}

func respond(w http.ResponseWriter, code int, response apiResponse) {
	w.WriteHeader(code)
	buf, _ := json.MarshalIndent(response, "", "    ")
	w.Write(buf)
}

type apiResponse struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
	Info  string `json:"info,omitempty"`
	Log   string `json:"log,omitempty"`
}

//
//
//

type loggingMiddleware struct {
	next   http.Handler
	logger log.Logger
}

func (mw loggingMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, logger := ctxlog.New(r.Context())
	logger.Log(
		"http_method", r.Method,
		"http_path", r.URL.Path,
		"http_content_length", r.ContentLength,
	)

	iw := &interceptingWriter{w, http.StatusOK}
	defer func(begin time.Time) {
		logger.Log(
			"http_status_code", iw.code,
			"http_duration", time.Since(begin),
		)
		level.Info(mw.logger).Log(logger.Keyvals()...)
	}(time.Now())

	mw.next.ServeHTTP(iw, r.WithContext(ctx))
}

type interceptingWriter struct {
	http.ResponseWriter
	code int
}

func (iw *interceptingWriter) WriteHeader(code int) {
	iw.code = code
	iw.ResponseWriter.WriteHeader(code)
}
