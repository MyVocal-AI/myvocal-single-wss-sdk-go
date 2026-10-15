# Single Connection TTS usage guide

Start with the installation and example commands in [README](README.md).

## Connect

Use an enterprise TTS API Key with dedicated concurrency and a voice ID available to your account. Authentication uses the `api-key` handshake header. Keep the Key in server-side configuration or an environment variable; do not put it in a URL, public source code, or logs.

```text
wss://api.voicelibrary.co/enterprise/v1/tts/{voice_id}/websocket
```

Supported models are `flash_v2_5` and `multilingual_v2`. These ordinary TTS WebSocket SDKs do not support `myvocal_v4` or `myvocal_v3_turbo`; use the [dialogue SDKs](https://api-docs.myvocal.ai/3892090w0) for those models.

## Connection options

| Purpose | Python | Java builder | Go |
| --- | --- | --- | --- |
| Model | `model_id` | `modelId(...)` | `ModelID` |
| Audio format | `output_format` | `outputFormat(...)` | `OutputFormat` |
| Language | `language_code` | `languageCode(...)` | `LanguageCode` |
| Idle timeout, seconds | `idle_timeout` | `idleTimeout(...)` | `IdleTimeout` |
| Timestamps | `timestamps` | `timestamps(...)` | `Timestamps` |
| Direct streaming | `direct_streaming` | `directStreaming(...)` | `DirectStreaming` |
| Seed | `seed` | `seed(...)` | `Seed` |
| Automatic generation | `auto_mode` | `autoMode(...)` | `AutoMode` |

Choose a model and audio format before connecting. Supported format values are `mp3_44100_64`, `mp3_44100_96`, `mp3_44100_128`, `mp3_44100_192`, `pcm_8000`, `pcm_16000`, `pcm_22050`, `pcm_24000`, `pcm_44100`, and `ulaw_8000`. The examples use `flash_v2_5` and `pcm_16000`; configure your player or output file for the selected encoding and sample rate.

`idleTimeout` accepts `1..180` seconds. `seed` accepts `0..4294967295`; a fixed seed does not guarantee identical audio. Language and advanced option availability depend on the model; consult the [API reference](https://api-docs.myvocal.ai/3687873w0).

The endpoint uses `priority=dedicated_concurrency`. The request audio parameter is `format`; use the language-specific option in the table above.

## Send text and finish

Each connection handles one synthesis transaction:

```text
connect -> initialize -> send text fragments -> finish -> final -> close
```

| Step | Python | Java | Go |
| --- | --- | --- | --- |
| Connect | `connect()` | `connect()` | `Connect(ctx)` |
| Initialize | `initialize(...)` | `initialize(...)` | `Initialize(...)` |
| Add text | `send_text(...)` | `sendText(...)` | `SendText(...)` |
| Generate buffered text now | `flush()` | `flush()` | `Flush()` |
| Finish and wait for final | `finish(timeout=60)` | `finish(60, TimeUnit.SECONDS)` | `Finish(ctx)` |
| Wait for pending callbacks | `drain_callbacks()` | `drainCallbacks(5, TimeUnit.SECONDS)` | `DrainCallbacks(ctx)` |
| Close | `close()` | `close()` | `Close()` |

Initialize once before sending text. `InitializationOptions` accepts optional voice settings and surrounding text. The `stability`, `similarity_boost`, and `style` settings accept `0..1`; `use_speaker_boost` is boolean. Leave settings unspecified to use their defaults.

Flush generates buffered audio and keeps the transaction open. Finish sends an empty text message and waits for the final event (`isFinal` or `is_final`). Do not send more text after finishing. Open a new client connection for another transaction.

## Receive audio and confirm completion

Register `on_audio` / `onAudio` / `OnAudio` for decoded JSON audio and `on_binary_audio` / `onBinaryAudio` / `OnBinaryAudio` for direct binary audio. The supplied example handles both and reports the byte count. Save or play chunks in arrival order using the selected format. If you need all callback output before processing the result, drain pending callbacks after finish succeeds.

A final event is the synthesis success signal. After a successful finish, a missing normal Close frame or a subsequent cleanup error does not invalidate the completed synthesis. In Go, a later `Close()` error can describe an already closed connection. Do not replay text solely because of that cleanup error.

If the connection ends before final, treat the synthesis as incomplete. A normal Close code by itself does not prove completion.

## Errors, timeouts, and retries

Keep callbacks short and avoid waiting for blocking SDK operations inside a callback. Move slow file processing or playback to your application. Handle callback errors separately from connection or synthesis errors.

- For validation errors, check model, format, language, option ranges, and call order.
- For authentication errors, check the enterprise Key and account access.
- For server errors, inspect the available error code, message, and context ID.
- For transport errors or timeouts before completion, clean up the affected connection and decide whether to start a new request.

Python wait timeouts are in seconds. Java wait methods take an explicit `TimeUnit`. Go blocking methods use `context.Context` deadlines. Set a timeout appropriate for the text length; a timeout is not a completion signal.

The SDK does not automatically reconnect or replay text. Retrying completed or partly processed text may produce duplicate audio and additional character usage.

## Usage and support

Use [Query primary key usage](https://api-docs.myvocal.ai/336818975e0) to view character usage. For support, provide the SDK version, operation, model, and error code. Do not include API Keys or private audio/text in public logs.
