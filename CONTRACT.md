# MyVocal Single WSS SDK Contract

All three SDKs implement one WebSocket connection per TTS transaction.

```text
connect -> initialize -> send text fragment(s) -> finish -> final -> close
```

`finish` sends `{"text":""}` and waits for `isFinal=true` or
`is_final=true`. `flush` sends `{"text":"","flush":true}` and does not imply
completion. SDKs never reconnect and replay submitted text automatically.

The final event is the transaction success boundary. If the service ends the
transport without a normal Close Frame after final, the SDK must preserve the
successful result and must not emit a late transport error.

The full machine-readable contract is in `sdk-contract.json`.
