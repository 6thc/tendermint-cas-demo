package cas

import (
	"bytes"
	"testing"

	"github.com/go-kit/kit/log"
	tendermintabci "github.com/tendermint/tendermint/abci/types"
)

func TestApplicationPersistLoad(t *testing.T) {
	// Create an application, send some transactions, persist to a buffer.
	var buf bytes.Buffer
	{
		a, _ := NewApplication(nil, newNopWriteCloser(&buf), log.NewNopLogger())
		a.BeginBlock(tendermintabci.RequestBeginBlock{})
		a.DeliverTx([]byte("a::one"))
		a.DeliverTx([]byte("a:one:two"))
		a.DeliverTx([]byte("a:two:three"))
		a.DeliverTx([]byte("b::foo"))
		a.DeliverTx([]byte("b::bar")) // should be rejected
		a.EndBlock(tendermintabci.RequestEndBlock{})
		a.Commit()
		a.BeginBlock(tendermintabci.RequestBeginBlock{})
		a.DeliverTx([]byte("a:three:four")) // delivered but not persisted
	}

	// Load a new application from that persisted buffer.
	a, _ := NewApplication(&buf, nil, log.NewNopLogger())

	// Committed transactions should be visible.
	{
		response := a.Query(tendermintabci.RequestQuery{Data: []byte("a")})
		if want, have := []byte("three"), response.Value; !bytes.Equal(want, have) {
			t.Errorf("Query(a): want %q, have %q", string(want), string(have))
		}
	}
	{
		response := a.Query(tendermintabci.RequestQuery{Data: []byte("b")})
		if want, have := []byte("foo"), response.Value; !bytes.Equal(want, have) {
			t.Errorf("Query(b): want %q, have %q", want, have)
		}
	}
}
