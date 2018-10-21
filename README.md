# Tendermint CAS demo

This repository is a tutorial for building a complete application on top of the
Tendermint ABCI, implementing a key-value store with a compare-and-swap API.

1. [The goal](#the-goal)
1. [Tendermint v Cosmos](#tendermint-v-cosmos)
1. [Tendermint concepts](#tendermint-concepts)
1. [ABCI methods](#abci-methods)
1. [Our demo application](#our-demo-application)
1. [The abci-cli](#the-abci-cli)
1. [System architecture](#system-architecture)
1. [Operations](#operations)
1. [Conclusion](#conclusion)


## The goal

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


## Tendermint v Cosmos

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
+------------+
| Basecoin   |
+------------+ +------------+
| Cosmos SDK | | This repo  |
+------------+-+------------+
| ABCI                      |
+---------------------------+
| Tendermint                |
+---------------------------+
```


## Tendermint concepts

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
application state that's been provided by the network operator. The application
can use this information to make itself ready to receive transactions.

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
same way the consensus state is updated by DeliverTx transactions, with one
important difference: when the consensus state is committed by Commit, it should
be copied to and fully overwrite the mempool state. This is because Tendermint
may deliver the same transaction via CheckTx more than once, though it will only
do so if that transaction is checked but doesn't make it in to the consensus
block.

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

- **Version**: The version of Tendermint, e.g. "0.25.0".

ResponseInfo
- **Data**: An arbitrary string containing information about the application,
  not parsed by Tendermint. Optional.
- **Version**: An arbitrary string containing the version of the application,
  used in the [Tendermint version handshake][versionhandshake]. Optional.
- **LastBlockHeight**: The height of the blockchain (number of commits) on this
  node. Taken from persisted consensus state. Required.
- **LastBlockAppHash**: The SHA256 hash of the last committed application state
  on this node. Taken from persisted consensus state. Required.

[versionhandshake]: https://github.com/tendermint/tendermint/blob/master/docs/spec/p2p/peer.md#tendermint-version-handshake

See [Initialization](#initialization).

### SetOption

RequestSetOption

- **Key**: An arbitrary string defining the option key.
- **Value**: An arbitrary string defining the option value.

ResponseSetOption

- **Code**: Response code; zero for OK, non-zero for error. Required.
- **Log**: Arbitrary string containing non-deterministic data intended for
  literal output via the application's logger. Optional.
- **Info**: Arbitrary string containing non-deterministic data in addition to
  log. Optional.

See [Initialization](#initialization).

### InitChain

RequestInitChain

- **Time**: The timestamp in the genesis file.
- **ChainId**: The chain ID string in the genesis file.
- **ConsensusParams**: Parameters that govern Tendermint's consensus behavior.
- **Validators**: The current set of validator nodes in the network.
- **AppStateBytes**: Initial state, provided in the genesis file, that a node
  starting for the first time may need to make itself ready to receive
  transactions.

ResponseInitChain

- **ConsensusParams**: Any changes to the proposed consensus parameters that
  this node would like to propose. Optional.
- **Validators**: Any changes to the set of validators that this node would like
  to propose. Optional.

See [Initialization](#initialization). ConsensusParams and Validators are beyond
the scope of this document, see the official documentation for details.

### Query

RequestQuery

- **Data**: The byte array from the user request.
- **Path**: The path string from the user request.
- **Height**: The desired height of the blockchain (in effect, the version of
  the state) against which the query should be run. A height of zero means the
  most recent state. To support this parameter, state needs to be implemented
  using a version-aware data structure, e.g. [this IAVL tree][iavl].
- **Prove**: If true, include a Merkle proof of the query results in the
  response.

[iavl]: https://github.com/tendermint/iavl

ResponseQuery

- **Code**: Response code; zero for OK, non-zero for error. Required.
- **Log**: Arbitrary string containing non-deterministic data intended for
  literal output via the application's logger. Optional.
- **Info**: Arbitrary string containing non-deterministic data in addition to
  log. Optional.
- **Index**: Related to the Merkle proof, if requested. Optional.
- **Key**: A byte array containing the key that's returned. Optional.
- **Value**: A byte array containing the data of the query response. Optional.
- **Proof**: A byte array containing the Merkle proof of the query, if
  requested. Optional.
- **Height**: The height of the blockchain (in effect, the version of the state)
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

- **Hash**: The hash of the block.
- **Header**: The header details for the block.
- **LastCommitInfo**: Details about the most recent (previous) commit.
- **ByzantineValidators**: Evidence of malicious validators, if any, during the
  most recent (previous) commit.

ResponseBeginBlock

- **Tags**: A set of key-value pairs that can be used to denote properties about
  this block, which can later be searched. Optional.

See [Writing application state](#writing-application-state). See also
[Application Development Guide: BeginBlock][beginblock].

[beginblock]: https://www.tendermint.com/docs/app-dev/app-development.html#beginblock

### CheckTx

The only argument is an opaque byte slice, proxied without modification from the
RPC connection DeliverTx method to the application.

ResponseCheckTx

- **Code**: Response code; zero for OK, non-zero for error. Required.
- **Data**: Arbitrary byte array containing any result from the transaction.
  Optional.
- **Log**: Arbitrary string containing non-deterministic data intended for
  literal output via the application's logger. Optional.
- **Info**: Arbitrary string containing non-deterministic data in addition to
  log. Optional.
- **GasWanted**: Amount of gas request for the transaction. Optional.
- **GasUsed**: Amount of gas consumed by the transaction. Optional.
- **Tags**: A set of key-value pairs that can be used to denote properties about
  this transaction, which can later be searched. Optional.

See [Connections](#connections). See also [Mempool Connection][mempoolconn].
Observe that CheckTx has exactly the same signature as DeliverTx; the only
difference is how to interpret the transaction body, i.e. which state (if any)
to update.

[mempoolconn]: https://www.tendermint.com/docs/app-dev/app-development.html#mempool-connection

### DeliverTx

The only argument is an opaque byte slice, proxied without modification from the
RPC connection DeliverTx method to the application.

ResponseDeliverTx

- **Code**: Response code; zero for OK, non-zero for error. Required.
- **Data**: Arbitrary byte array containing any result from the transaction.
  Optional.
- **Log**: Arbitrary string containing non-deterministic data intended for
  literal output via the application's logger. Optional.
- **Info**: Arbitrary string containing non-deterministic data in addition to
  log. Optional.
- **GasWanted**: Amount of gas request for the transaction. Optional.
- **GasUsed**: Amount of gas consumed by the transaction. Optional.
- **Tags**: A set of key-value pairs that can be used to denote properties about
  this transaction, which can later be searched. Optional.

See [Writing application state](#writing-application-state) and
[Connections](#connections). See also [DeliverTx][delivertx]. Observe that
DeliverTx has exactly the same signature as CheckTx; the only difference is how
to interpret the transaction body, i.e. which state (if any) to update.

[delivertx]: https://www.tendermint.com/docs/app-dev/app-development.html#delivertx

### EndBlock

RequestEndBlock

- **Height**: The height of the block.

ResponseEndBlock

- **ValidatorUpdates**: Updates to the set of validators, if any. Optional.
- **ConsensusParamsUpdate**: Updates to the consensus parameters, if any.
  Optional.
- **Tags**: A set of key-value pairs that can be used to denote properties about
  this block, which can later be searched. Optional.

See [Writing application state](#writing-application-state) and
[Connections](#connections). See also [EndBlock][endblock]. 

[endblock]: https://www.tendermint.com/docs/app-dev/app-development.html#endblock

### Commit

Commit requests have no parameters.

ResponseCommit

- **Data**: A deterministic (Merkle) hash of the state root of the application.
  Required.

See [Writing application state](#writing-application-state) and
[Connections](#connections). See also [Commit][commit]. It's expected that the
application persist its state to disk during commit.

[commit]: https://www.tendermint.com/docs/app-dev/app-development.html#commit


## Our demo application

The code for the ABCI application, implementing our key-value store with compare-and-swap semantics,
is available in [internal/cas/application.go][application]. The code for the state layer
is available in [internal/cas/state.go][state].

[application]: https://github.com/6thc/tendermint-cas-demo/blob/master/internal/cas/application.go
[state]: https://github.com/6thc/tendermint-cas-demo/blob/master/internal/cas/state.go


## The abci-cli

Once you have a type implementing the ABCI application interface, you can do
basic tests by wrapping it with an [abci/server.NewServer][abcinewserver] and
calling it with a tool called [`abci-cli`][abcicli]. 

[abcinewserver]: https://godoc.org/github.com/tendermint/tendermint/abci/server#NewServer
[abcicli]: https://tendermint.com/docs/app-dev/abci-cli.html

The code to mount your application will look something like this.

```go
func main() {
	app := newMyApplication()
	server, err := server.NewServer("127.0.0.1:8080", "socket", app)
	if err != nil {
		log.Fatal(err)
	}
	server.Start()
}
```

See [the `abci-cli` documentation][abcicli] for more details.


## System architecture

Now you have a working ABCI application. How can you connect it to the
Tendermint machinery, so that it can communicate with other, identical nodes in
a network?

Recall the original [Tendermint architecture diagram][diagram]. The Tendermint
node, the green box, handles the heavy work of consensus. The validator signer,
the purple box, can also be provided by Tendermint, and validates blocks, moving
consensus forward. Our ABCI application, the blue box, is connected to the node
exclusively. And our user API, in the diagram represented by Cosmos Voyager and
the Light Client Daemon, is also connected only to the node.

These are the logical components, and they can be deployed in different physical
arrangements depending on your needs. In the diagram, the Tendermint core node,
the ABCI application, and the validator signer are all co-located on the same
circle, representing a full node. The lines between them indicate communication,
but that communication can occur in different ways. The components can be built
into the same binary, executed as a single process, and the communication occur
exclusively in-memory. Or, the components can be built into separate binaries,
executed as different processes on the same machine, and the communication occur
over e.g. UNIX domain sockets. Or, the components could be deployed to different
physical machines, and the communication occur over e.g. TCP connections. 

Similarly, in the diagram, the user is expected to interact with the network by
using the Cosmos Voyager web application, deployed adjacent to a Light Client
Daemon on the user's machine, speaking REST (HTTP) to each other. The Light
Client Daemon connects to a Tendermint core node over HTTP, and performs the
e.g. ABCIQuery and BroadcastTx requests that way.

The diagram has things arranged in this way, but you don't necessarily need to
copy it. For example, a more secure deployment might move the validators onto
their own single-purpose machines, heavily firewalled from the rest of the
internet, with only a single connection to a different, larger set of full
nodes, running only Tendermint core and your ABCI application. 

For our demo, we'll model the user API as a separate HTTP API, but built into
the same binary as all the other components, and run in the same process. The
HTTP API is defined in [cmd/tendermint-cas-demo/cas_api.go][casapi], and all of
the components are wired together in [cmd/tendermint-cas-demo/main.go][main].

[casapi]: https://github.com/6thc/tendermint-cas-demo/blob/master/cmd/tendermint-cas-demo/cas_api.go
[main]: https://github.com/6thc/tendermint-cas-demo/blob/master/cmd/tendermint-cas-demo/main.go


## Operations

The Tendermint node requires quite a lot of configuration to successfully start,
including several files in well-defined locations on disk, such as the JSON
genesis file, a TOML configuration file, and cryptographic keys for the node
itself and its validators. The helper scripts [bootstrap_1][bootstrap1] and
[bootstrap_3][bootstrap3] create these file structures for one- and three-node
clusters on the local machine respectively. Studying them should give you a
good start toward scripting your own deployment.

[bootstrap1]: https://github.com/6thc/tendermint-cas-demo/blob/master/bootstrap_1.fish
[bootstrap3]: https://github.com/6thc/tendermint-cas-demo/blob/master/bootstrap_3.fish


## Conclusion

Thanks for your time and attention; I hope it's been helpful. If you have any
further questions, or things in this repo don't work as expected, please file an
issue.
