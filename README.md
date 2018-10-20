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

### RPC


## ABCI methods

Now that we have a high-level understanding of Tendermint, let's get into detail
about each ABCI method.

### Info

### SetOption

### InitChain

### Query

Query will probably want to read consensus state, for the most reliable and
up-to-date view of the world. In some cases, itt may want to read committed
state, for example if a application-specific flag is defined in the query body,
in order to return data that is guaranteed to be persistent in case of node
failure. Or, it may want to read mempool state, to yield the most bleeding-edge
version of events, with some risk of that state being rendered invalid in the
future. These are all application decisions.

### BeginBlock

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

