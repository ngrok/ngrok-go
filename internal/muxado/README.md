# muxado - Stream multiplexing for Go [![godoc reference](https://godoc.org/github.com/inconshreveable/muxado?status.png)](https://godoc.org/github.com/inconshreveable/muxado)

muxado implements a general purpose stream-multiplexing protocol. muxado allows clients applications
to multiplex any io.ReadWriteCloser (like a net.Conn) into multiple, independent full-duplex byte streams.

muxado is a useful protocol for any two communicating processes. It is an excellent base protocol
for implementing lightweight RPC. It eliminates the need for custom async/pipeling code from your peers
in order to support multiple simultaneous inflight requests between peers. For the same reason, it also
eliminates the need to build connection pools for your clients. It enables servers to initiate streams
to clients without building any NAT traversal. muxado can also yield performance improvements (especially
latency) for protocols that require rapidly opening many concurrent connections.

muxado's API is designed to make it seamless to integrate into existing Go programs. muxado.Session
implements the net.Listener interface and muxado.Stream implements net.Conn.

## Example

Here's an example client which responds to simple JSON requests from a server.

```go
    conn, _ := net.Dial("tcp", "example.net:1234")
    sess := muxado.Client(conn)
    for {
        stream, _ := sess.Accept()
        go func(str net.Conn) {
            defer str.Close()
            var req Request
            json.NewDecoder(str).Decode(&req)
            response := handleRequest(&req)
            json.NewEncoder(str).Encode(response)
        }(stream)
    }
```

Maybe the client wants to make a request to the server instead of just responding. This is easy as well:

```go
    stream, _ := sess.Open()
    req := Request{
        Query: "What is the meaning of life, the universe and everything?",
    }
    json.NewEncoder(stream).Encode(&req)
    var resp Response
    json.dec.Decode(&resp)
    if resp.Answer != "42" {
        panic("wrong answer to the ultimate question!")
    }
```

## Terminology
muxado defines the following terms for clarity of the documentation:

A "Transport" is an underlying stream (typically TCP) that is multiplexed by sending frames between muxado peers over this transport.

A "Stream" is any of the full-duplex byte-streams multiplexed over the transport

A "Session" is two peers running the muxado protocol over a single transport

## Implementation Design
muxado's design is influenced heavily by the framing layer of HTTP2 and SPDY. However, instead
of being specialized for a higher-level protocol, muxado is designed in a protocol agnostic way
with simplicity and speed in mind. More advanced features are left to higher-level libraries and protocols.

## Extended functionality
muxado ships with two wrappers that add commonly used functionality. The first is a TypedStreamSession
which allows a client application to open streams with a type identifier so that the remote peer
can identify the protocol that will be communicated on that stream.

The second wrapper is a simple Heartbeat which issues a callback to the application informing it
of round-trip latency and heartbeat failure.

## Performance
XXX: add perf numbers and comparisons

Any stream-multiplexing library over TCP will suffer from head-of-line blocking if the next packet to service gets dropped.
muxado is also a poor choice when sending many large payloads concurrently.
It shines best when the application workload needs to quickly open a large number of small-payload streams.

## Status
Most of muxado's features are implemented (and tested!), but there are many that are still rough or could be improved. See the TODO file for suggestions on what needs to improve.

## License
Apache
