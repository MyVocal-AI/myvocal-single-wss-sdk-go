# MyVocal Single WSS SDK for Go

Go 1.20+ client for one enterprise Single WebSocket TTS transaction per
connection.

## Install

```bash
go get github.com/MyVocal-AI/myvocal-single-wss-sdk-go@v1.0.1
```

To use the delivery ZIP instead, extract it and run:

```bash
go test ./...
go run ./examples/basic
```

## Lifecycle

```text
Connect -> Initialize -> SendText -> Finish -> final -> Close
```

`Finish` sends an empty text message and waits for final. `Flush` only forces
buffered output and does not mean completion. The SDK never reconnects and
replays submitted text automatically.

After a valid final event, a transport close without a normal Close Frame does
not turn the completed transaction into an SDK error.

The service may close with code `1006` or without a Close Frame after final.
When `Finish` has already succeeded, an error returned by a later `Close` call
only describes cleanup of an already closed socket and does not invalidate the
completed synthesis. Do not replay the text based on that cleanup error.
