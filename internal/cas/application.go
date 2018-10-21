package cas

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	tendermintabci "github.com/tendermint/tendermint/abci/types"
)

// https://tendermint.com/docs/spec/abci/abci.html
// https://tendermint.com/docs/app-dev/app-development.html#blockchain-protocol
//
// Some ABCI methods don't return errors because an error would indicate a
// critical failure in the application and there's nothing Tendermint can do.
// The problem should be addressed and both Tendermint and the application
// restarted. All other methods return an application-specific response Code
// uint32, where only 0 is reserved for OK.
//
// Some ABCI methods include a Tags field in their response. Each tag is
// key-value pair denoting something about what happened during the methods
// execution. Tags can be used to index transactions and blocks according to
// what happened during their execution.
//
// Some ABCI methods return explicitly non-deterministic data in the form of
// Info and Log fields. The Log is intended for the literal output from the
// application's logger, while the Info is any additional info that should be
// returned. These are the only fields that are not included in block header
// computations, so we don't need agreement on them. All other fields in the
// response must be strictly deterministic.

var (
	_ tendermintabci.Application = (*Application)(nil)
)

// Application implements the Tendermint ABCI.
type Application struct {
	mempool   *State
	consensus *State
	persist   io.WriteCloser
	logger    log.Logger
}

// NewApplication returns a Tendermint application server, implementing the
// ABCI. If initial is non-nil, initial state is populated from it. If persist
// is non-nil, state is persisted there on each Tendermint commit.
func NewApplication(initial io.Reader, persist io.WriteCloser, logger log.Logger) (*Application, error) {
	consensus := NewState()
	if initial != nil {
		if err := consensus.Restore(initial); err != nil {
			return nil, err
		}
	}

	mempool := NewState()
	copyState(mempool, consensus)

	if persist == nil {
		persist = newNopWriteCloser(ioutil.Discard)
	}

	return &Application{
		mempool:   mempool,
		consensus: consensus,
		persist:   persist,
		logger:    logger,
	}, nil
}

// Info implements ABCI and is called by Tendermint prior to InitChain as a sort
// of handshake.
//
// When either the app or Tendermint restarts, they need to sync to a common
// chain height. When an ABCI connection is first established, Tendermint will
// call Info on the Query connection. The response should contain the
// LastBlockHeight and LastBlockAppHash; the former is the last block for which
// the app ran Commit successfully, and the latter is the response from that
// Commit.
//
// Using this information, Tendermint will determine what needs to be replayed,
// if anything, against the app, to ensure both Tendermint and the app are
// synced to the latest block height. If the app returns a LastBlockHeight of 0,
// Tendermint will just replay all blocks.
//
// The data and version fields may contain arbitrary app-specific information.
func (a *Application) Info(tendermintabci.RequestInfo) (response tendermintabci.ResponseInfo) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "Info",
			"data", response.Data,
			"version", response.Version,
			"last_block_height", response.LastBlockHeight,
			"last_block_app_hash", fmt.Sprintf("%X", response.LastBlockAppHash),
		)
	}()

	return tendermintabci.ResponseInfo{
		Data:             "",
		Version:          "",
		LastBlockHeight:  a.consensus.Commits(),
		LastBlockAppHash: a.consensus.Hash(),
	}
}

// SetOption implements ABCI and configures non-consensus-critical (i.e.
// non-deterministic) aspects of the application. For example, min-fee:
// 100fermion could set the minimum fee required for CheckTx; but not DeliverTx,
// as that would be consensus critical.
func (a *Application) SetOption(request tendermintabci.RequestSetOption) (response tendermintabci.ResponseSetOption) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "SetOption",
			"key", request.Key,
			"value", request.Value,
			"code", response.Code,
			"log", response.Log,
			"info", response.Info,
		)
	}()

	return tendermintabci.ResponseSetOption{
		Code: tendermintabci.CodeTypeOK,
	}
}

// InitChain implements ABCI and is called once, at genesis.
//
// Certain request parameters come from the genesis file. The time is the
// genesis time; if this is relevant to the application, it must be preferred to
// system time. Chain ID uniquely identifies the blockchain. AppStateBytes is
// the Amino/JSON-serialized initial application state.
//
// ConsensusParams describe attributes of Tendermint behavior. If the
// application wants to change any of them, it can include modified values in
// its response. Those modifications must be deterministic, all nodes must
// return the same params for the same InitChain.
//
// If the application returns an empty validator set, the initial validator set
// will be the validators proposed in the request. If the application returns a
// nonempty validator set, the initial validator set will be those validators,
// regardless of what was proposed in the request. This allows the app to decide
// if it wants to accept the initial validators proposed by Tendermint or if it
// wants to use a different one. For example, validators may be computed based
// on some application-specific information in the genesis file.
func (a *Application) InitChain(request tendermintabci.RequestInitChain) (response tendermintabci.ResponseInitChain) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "InitChain",
			"time", request.Time.String(),
			"chain_id", request.ChainId,
			"app_state_bytes", len(request.AppStateBytes),
		)
	}()

	return tendermintabci.ResponseInitChain{}
}

// Query implements ABCI and is used for reads. In this application, we
// interpret the data as the key, and return the current value.
func (a *Application) Query(query tendermintabci.RequestQuery) (response tendermintabci.ResponseQuery) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "Query",
			"path", query.Path,
			"data", string(query.Data),
			"ok", response.IsOK(),
			"code", response.Code,
			"key", string(response.Key),
			"value", string(response.Value),
			"log", response.Log,
			"info", response.Info,
		)
	}()

	// TODO(pb): filter out the /p2p paths
	// TODO(pb): maybe require a /store or /cas path prefix?
	// TODO(pb): respect query.Height, though I'm not sure how
	// TODO(pb): respect query.Prove, though I'm not sure how

	value, err := a.consensus.Get(string(query.Data))
	if err != nil {
		return tendermintabci.ResponseQuery{
			Code: codeBadRequest,
			Key:  query.Data,
			Log:  err.Error(),
		}
	}

	return tendermintabci.ResponseQuery{
		Code:  tendermintabci.CodeTypeOK,
		Key:   query.Data,
		Value: value,
	}
}

// BeginBlock implements ABCI and demarcates the start of a block (of
// transactions) in the chain.
func (a *Application) BeginBlock(request tendermintabci.RequestBeginBlock) (response tendermintabci.ResponseBeginBlock) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "BeginBlock",
			"hash", fmt.Sprintf("%x", request.Hash),
			"header.chain_id", request.Header.ChainID,
			"header.height", request.Header.Height,
			"header.time", request.Header.Time,
			"header.num_txs", request.Header.NumTxs,
			"header.total_txs", request.Header.TotalTxs,
			"last_commit_info.round", request.LastCommitInfo.Round,
			"byzantine_validators", len(request.ByzantineValidators),
		)
	}()

	// TODO(pb): probably should validate request.Hash

	return tendermintabci.ResponseBeginBlock{}
}

// CheckTx implements ABCI and is invoked before DeliverTx.
// Invalid transactions can be rejected before they're persisted or gossiped.
// This is an optimization step: simply returning OK won't affect correctness.
func (a *Application) CheckTx(p []byte) (response tendermintabci.ResponseCheckTx) {
	var (
		key      string
		old, new []byte
	)

	defer func() {
		level.Debug(a.logger).Log(
			"abci", "CheckTx",
			"key", key,
			"old", string(old),
			"new", string(new),
			"ok", response.IsOK(),
			"code", response.Code,
			"gas_used", response.GasUsed,
			"gas_wanted", response.GasWanted,
			"log", response.Log,
			"info", response.Info,
		)
	}()

	var (
		code uint32
		log  string
	)
	key, old, new, code, log = parseTx(p)
	if code != tendermintabci.CodeTypeOK {
		return tendermintabci.ResponseCheckTx{
			Code: code,
			Log:  log,
			// TODO(pb): Gas accounting
		}
	}

	// Note this is mempool, not consensus.
	if err := a.mempool.CompareAndSwap(key, old, new); err != nil {
		return tendermintabci.ResponseCheckTx{
			Code: codeCASFailure,
			Log:  err.Error(),
			// TODO(pb): Gas accounting
		}
	}

	return tendermintabci.ResponseCheckTx{
		Code: tendermintabci.CodeTypeOK,
		// TODO(pb): Gas accounting
	}
}

// DeliverTx implements ABCI and is used for all writes.
func (a *Application) DeliverTx(p []byte) (response tendermintabci.ResponseDeliverTx) {
	var (
		key      string
		old, new []byte
	)

	defer func() {
		level.Debug(a.logger).Log(
			"abci", "DeliverTx",
			"key", key,
			"old", string(old),
			"new", string(new),
			"ok", response.IsOK(),
			"code", response.Code,
			"log", response.Log,
			"info", response.Info,
		)
	}()

	var (
		code uint32
		log  string
	)
	key, old, new, code, log = parseTx(p)
	if code != tendermintabci.CodeTypeOK {
		return tendermintabci.ResponseDeliverTx{
			Code: code,
			Log:  log,
			// TODO(pb): Gas accounting
		}
	}

	// Note this is consensus, not mempool.
	if err := a.consensus.CompareAndSwap(key, old, new); err != nil {
		return tendermintabci.ResponseDeliverTx{
			Code: codeCASFailure,
			Log:  err.Error(),
			// TODO(pb): Gas accounting
		}
	}

	return tendermintabci.ResponseDeliverTx{
		Code: tendermintabci.CodeTypeOK,
		// TODO(pb): Gas accounting
	}
}

// EndBlock implements ABCI and demarcates the end of a block (of transactions)
// in the chain.
func (a *Application) EndBlock(request tendermintabci.RequestEndBlock) (response tendermintabci.ResponseEndBlock) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "EndBlock",
			"height", request.Height,
			"consensus_state_commits", a.consensus.Commits(),
		)
	}()

	// TODO(pb): probably should validate height

	return tendermintabci.ResponseEndBlock{}
}

// Commit implements ABCI and persists the current state.
// A hash of that state is returned to the caller, i.e. the Tendermint core machinery.
func (a *Application) Commit() (response tendermintabci.ResponseCommit) {
	defer func() {
		level.Debug(a.logger).Log(
			"abci", "Commit",
			"data", fmt.Sprintf("%x", response.Data),
		)
	}()

	err := a.consensus.Commit(a.persist)
	if err != nil {
		panic(fmt.Sprintf("error: Commit failed: %v", err)) // TODO(pb): I guess Commit isn't allowed to fail?
	}

	// Tendermint expects mempool state to be equal to consensus state after a
	// successful commit.
	copyState(a.mempool, a.consensus)

	return tendermintabci.ResponseCommit{
		Data: a.consensus.Hash(),
	}
}

func parseTx(p []byte) (key string, old, new []byte, code uint32, log string) {
	tokens := bytes.SplitN(p, []byte{':'}, 3)
	if len(tokens) != 3 {
		return key, old, new, codeBadRequest, `bad request: tx data must be "<key>:<old>:<new>"`
	}
	return string(tokens[0]), tokens[1], tokens[2], tendermintabci.CodeTypeOK, log
}

const (
	codeBadRequest = 513 // arbitrary non-zero
	codeCASFailure = 514 // arbitrary non-zero
)

func newNopWriteCloser(w io.Writer) io.WriteCloser {
	return writeCloser{Writer: w, Closer: nopCloser}
}

type writeCloser struct {
	io.Writer
	io.Closer
}

type nopCloserType struct{}

func (nopCloserType) Close() error { return nil }

var nopCloser = nopCloserType{}
