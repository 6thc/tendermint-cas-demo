# Tendermint CAS demo

This repository is a tutorial for building a complete application on top of the
Tendermint ABCI, implementing a key-value store with a compare-and-swap API.

## Tendermint and Cosmos

[Tendermint][tendermint] is a distributed, byzantine fault-tolerant consensus
system designed to replicate arbitrary state machines. You can build on top of
Tendermint by plugging into it at different levels of abstraction, depending on
what kind of application you're building.

[tendermint]: https://www.tendermint.com

At the lowest level, Tendermint defines an API, called the Application
BlockChain Interface, or ABCI. Applications that implement the ABCI can be
replicated with the Tendermint protocol.

One level above Tendermint is [Cosmos][cosmos], a federated network of
blockchains; or, more accurately, the [Cosmos SDK][sdk], which allows users to
build Cosmos-compatible applications. The [Basecoin][basecoin] demo application 
is built on the Cosmos SDK.

[cosmos]: https://cosmos.network
[sdk]: https://cosmos.network/docs/getting-started/installation.html
[basecoin]: https://cosmos.network/docs/sdk/core/app5.html

```
+------------+ +------------+
| Cosmos SDK | | This repo  |
+------------+-+------------+
| ABCI                      |
+---------------------------+
| Tendermint machinery      |
+---------------------------+
```

## Goal

We're going to build a distributed system in Go. Each node will have an HTTP API
implementing compare-and-swap semantics for a key-value store. The keys and
values will be reliably and consistently replicated between nodes by Tendermint.

```
$ curl -Ss -XPOST 'http://localhost:10001/x?new=foo'
{
    "key": "x",
    "value: "foo"
}
$ curl -Ss -XPOST 'http://localhost:10002/x?old=foo&new=bar'
{
    "key": "x",
    "value: "bar"
}
$ curl -Ss -XGET 'http://localhost:10003/key'
{
    "key": "x",
    "value: "bar"
}
```

Hopefully, this exercise will expose you to enough of the Tendermint programming
model that you can confidently build your own Tendermint ABCI applications.


## Concepts

### Understanding ABCI

A picture is worth a thousand words; [this diagram][diagram] provides a great
conceptual overview of all of the interacting components in a Tendermint
network. In the full node, we're going to implement the blue box titled ABCI
App. To do that, we need to implement [the ABCI interface][application].

[diagram]: https://drive.google.com/file/d/1yR2XpRi9YCY9H9uMfcw8-RMJpvDyvjz9/view
[application]: https://godoc.org/github.com/tendermint/tendermint/abci/types#Application

```go
type Application interface {
	Info(RequestInfo) ResponseInfo
	SetOption(RequestSetOption) ResponseSetOption
	Query(RequestQuery) ResponseQuery
	CheckTx(tx []byte) ResponseCheckTx
	InitChain(RequestInitChain) ResponseInitChain
	BeginBlock(RequestBeginBlock) ResponseBeginBlock
	DeliverTx(tx []byte) ResponseDeliverTx
	EndBlock(RequestEndBlock) ResponseEndBlock
	Commit() ResponseCommit
}
```

Understanding these methods requires understanding the Tendermint state machine.
Let's understand things at a high level first, and then describe each method in
detail.

### Writing application state

It's expected that your application wraps some state, which Tendermint
transactions (Tx) manipulate. Furthermore, it's expected that your application
state has a notion of a commit, which should 

- **Count** the number of commits made
- **Persist** the state to long-term storage
- **Hash** the complete state at the time of commit

Tendermint takes care of delivering transactions to your application via
DeliverTx. Those transactions are guaranteed to come in the same order to all
instances of your application, on all nodes in the network. Each transaction
must have the same deterministic effect on application state on all nodes.

Transactions are bundled into blocks, demarcated by BeginBlock and EndBlock
calls. One block will contain zero or more transactions, delivered via
DeliverTx. After EndBlock, Tendermint will always call Commit, which should
trigger the commit steps enumerated above. Tendermint is guaranteed to call
(BeginBlock, DeliverTx, DeliverTx, ..., EndBlock, Commit) in exactly the same
order, with exactly the same data, on all nodes.

All changes to state must occur via DeliverTx exclusively.

### Reading application state

The Tendermint ABCI method Query is used to read application state. Your
application has a lot of leeway to decide how to create, interpret, and service
Query requests. The only thing that Tendermint stipulates is that some query
paths (a string field in the query request) are reserved. Otherwise, the only
contract is that queries must not mutate state.

### Initialization

We've talked about how state is written to and read from. But how is the
state machine itself initialized?

The abstract, global state machine replicated by Tendermint is created in an
event known as genesis. Genesis involves creating a chain ID, which uniquely
identifies the state machine, as well as other parameters, like the initial set
of participating nodes. This configuration information is collected into a genesis
file, which must be securely distributed to each initial node in the Tendermint
network. All nodes must share exactly the same genesis file.

The concrete, specific state machine instance managed by a given Tendermint node 
is created at process start. If the node is starting for the first time, it will
have an empty application state; if the node has e.g. rebooted, it should load its
application state from persistent storage into memory.

Three ABCI methods manage state machine initialization.

InitChain is called once, when a node starts for the first time. It tells the
application about some aspects of the state machine. or chain, from Tendermint's
perspective, including the chain ID, consensus parameters, and any initial
application state that's been set by the network operator in the genesis file.
The application can use this information to make itself ready to receive
transactions.

SetOption may be called to set arbitrary application configuration parameters.
This is only done if by user request, and Tendermint doesn't interpret these
commands, or route them through its consensus system. This means that any
changes made via SetOption may be different on different nodes, and therefore
must not have any effect on how transactions or queries are processed. This is
sometimes referred to as being non-consensus-critical or non-deterministic.

Info is called at each process start, after the application has restored any
application state from persistent storage, so that Tendermint can know the last
block height (a.k.a. the commit count) and app state hash of the application.
Tendermint will calculate the diff between what your application reports in
Info, and what Tendermint knows the current state of the global state machine to
be, and will replay blocks of transactions to your application, until the block
height and app hash match.

### Connections

Tendermint speaks to your application exclusively through the ABCI interface,
but it does so through three independent connections. Calls are serialized on
each connection, but may be concurrent across different connections. Each
connection only calls a subset of the ABCI methods.

The **query** connection is responsible for read operations by calling Query. It
also handles initialization, calling InitChain, SetOption, and Info.

The **consensus** connection is responsible for write operations, calling
BeginBlock, DeliverTx, EndBlock, and Commit. 

There is a third connection which introduces a new concept to your application
state. When a transaction arrives at a Tendermint node, before it's given to the
consensus machinery and replicated to the rest of the network, it's first sent
to the local application, over the **mempool** connection, to the CheckTx
method. This is ultimately an optimization step, giving the application the
opportunity to validate the transaction (for example, checking that the
transaction body is properly encoded) and stop invalid transactions before
they're broadcast. If the application needs to implement replay protection (for
example, to protect against double-spend attacks) it should also perform that
accounting in CheckTx.

To support CheckTx and the mempool connection, it's recommended that
applications actually keep two separate in-memory representations of their
state: the consensus state, updated by DeliverTx; and the mempool state, updated
by CheckTx. The mempool state should be updated by CheckTx transactions in the
same way the consensus state is updated by DeliverTx transactions, with one important difference: when the consensus state is committed by Commit, it should be copied to
and fully overwrite the mempool state. This is because Tendermint may deliver the
same transaction via CheckTx more than once, though it will only do so if that
transaction is checked but doesn't make it in to the consensus block.

To be clear, this step is optional. Applications may choose to skip managing a
separate mempool state, and simply return an OK result for every CheckTx call.
This should not affect correctness, only efficiency.

### RPC

All user requests must be routed to the application through Tendermint via
Tendermint's RPC mechanism. Requests must never hit the application or its state
directly. To make RPC requests through Tendermint to our application, we use an
RPC client. The relevant part of that interface is [ABCIClient][abciclient].

[abciclient]: https://godoc.org/github.com/tendermint/tendermint/rpc/client#ABCIClient

```go
type ABCIClient interface {
	ABCIInfo() (*ctypes.ResultABCIInfo, error)
	ABCIQuery(path string, data cmn.HexBytes) (*ctypes.ResultABCIQuery, error)
	ABCIQueryWithOptions(path string, data cmn.HexBytes, opts ABCIQueryOptions) (*ctypes.ResultABCIQuery, error)

	BroadcastTxCommit(tx types.Tx) (*ctypes.ResultBroadcastTxCommit, error)
	BroadcastTxAsync(tx types.Tx) (*ctypes.ResultBroadcastTx, error)
	BroadcastTxSync(tx types.Tx) (*ctypes.ResultBroadcastTx, error)
}
```

We never need to implement an ABCIClient, we just need to construct one. That
client should be taken as a dependency to our user-facing API, and all user
requests should be proxied through it. Reads should go through ABCIQuery, and
writes should go through one of the BroadcastTx methods. The difference between
those methods relates to how long they block before returning a result.
BroadcastTxCommit blocks the longest, and waits until the transaction has been
committed into a block by a quorum of nodes in the network. BroadcastTxSync
waits until the transaction has been accepted by the consensus connection, but
not necessarily committed. BroadcastTxAsync only waits until the Tendermint
machinery has received the transaction, and returns before it's been received by
any node.


## ABCI methods

Now that we have a high-level understanding of Tendermint, let's get into detail
about each ABCI method.

### Info

RequestInfo

- Version: The version of Tendermint, e.g. "0.25.0".

ResponseInfo
- Data: An arbitrary string containing information about the application, not
  parsed by Tendermint. Optional.
- Version: An arbitrary string containing the version of the application, used
  in the [Tendermint version handshake][versionhandshake]. Optional.
- LastBlockHeight: The height of the blockchain (number of commits) on this
  node. Taken from persisted consensus state. Required.
- LastBlockAppHash: The SHA256 hash of the last committed application state on
  this node. Taken from persisted consensus state. Required.

[versionhandshake]: https://github.com/tendermint/tendermint/blob/master/docs/spec/p2p/peer.md#tendermint-version-handshake

See [Initialization](#initialization).

### SetOption

RequestSetOption

- Key: An arbitrary string defining the option key.
- Value: An arbitrary string defining the option value.

ResponseSetOption

- Code: Response code; zero for OK, non-zero for error. Required.
- Log: Arbitrary string containing non-deterministic data intended for literal
  output via the application's logger. Optional.
- Info: Arbitrary string containing non-deterministic data in addition to log.
  Optional.

See [Initialization](#initialization).

### InitChain

RequestInitChain

- Time: The timestamp in the genesis file.
- ChainId: The chain ID string in the genesis file.
- ConsensusParams: Parameters that govern Tendermint's consensus behavior.
- Validators: The current set of validator nodes in the network.
- AppStateBytes: Initial state, provided in the genesis file, that a node
  starting for the first time may need to make itself ready to receive
  transactions.

ResponseInitChain

- ConsensusParams: Any changes to the proposed consensus parameters that this
  node would like to propose. Optional.
- Validators: Any changes to the set of validators that this node would like to
  propose. Optional.

See [Initialization](#initialization). ConsensusParams and Validators are beyond
the scope of this document, see the official documentation for details.

### Query

RequestQuery

- Data: The byte array from the user request.
- Path: The path string from the user request.
- Height: The desired height of the blockchain (in effect, the version of the
  state) against which the query should be run. A height of zero means the most
  recent state. To support this parameter, state needs to be implemented using 
  a version-aware data structure, e.g. [this IAVL tree][iavl].
- Prove: If true, include a Merkle proof of the query results in the response.

[iavl]: https://github.com/tendermint/iavl

ResponseQuery

- Code: Response code; zero for OK, non-zero for error. Required.
- Log: Arbitrary string containing non-deterministic data intended for literal
  output via the application's logger. Optional.
- Info: Arbitrary string containing non-deterministic data in addition to log.
  Optional.
- Index: Related to the Merkle proof, if requested. Optional.
- Key: A byte array containing the key that's returned. Optional.
- Value: A byte array containing the data of the query response. Optional.
- Proof: A byte array containing the Merkle proof of the query, if requested.
  Optional.
- Height: The height of the blockchain (in effect, the version of the state)
  against which the query was run. Optional.

See [Reading application state](#reading-application-state) and
[Connections](#connections). Note that our demo application doesn't implement
Height, Prove, or Proof. Merkle proofs are beyond the scope of this document,
see the official documentation for details.

Query will probably want to read consensus state, for the most reliable and
up-to-date view of the world. In some cases, it may want to read committed
state, for example if a application-specific flag is defined in the query body,
in order to return data that is guaranteed to be persistent in case of node
failure. Or, it may want to read mempool state, to yield the most bleeding-edge
version of events, with some risk of that state being rendered invalid in the
future. These are all application decisions.

### BeginBlock

RequestBeginBlock

- TODO

ResponseBeginBlock

- TODO

See [Writing application state](#writing-application-state). See also
[Application Development Guide: BeginBlock][beginblock].

[beginblock]: https://www.tendermint.com/docs/app-dev/app-development.html#beginblock

### CheckTx

### DeliverTx

### EndBlock

### Commit


## Coda: abci-cli


## Architecture

Now you have a working ABCI application. How can you connect it to the
Tendermint machinery, so that it can communicate with other, identical nodes in
a network?

### yourapp.Application

### tendermint.Node

### Network topologies


## Operations

Now you have a working server binary. What else does it need to be successfully
configured and deployed?

### Genesis

### Configuration

