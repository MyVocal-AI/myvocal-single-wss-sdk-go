package myvocalsinglewss

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type fakeSocket struct {
	mu     sync.Mutex
	sent   [][]byte
	closed int
}

type scriptedRead struct {
	messageType int
	payload     []byte
	err         error
}

type scriptedReadSocket struct {
	fakeSocket
	reads []scriptedRead
	index int
}

func (socket *scriptedReadSocket) ReadMessage() (int, []byte, error) {
	if socket.index >= len(socket.reads) {
		return 0, nil, io.EOF
	}
	read := socket.reads[socket.index]
	socket.index++
	return read.messageType, read.payload, read.err
}

func (socket *fakeSocket) WriteJSON(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	socket.mu.Lock()
	socket.sent = append(socket.sent, data)
	socket.mu.Unlock()
	return nil
}

func (socket *fakeSocket) WriteControl(_ int, _ []byte, _ time.Time) error {
	return nil
}

func (socket *fakeSocket) Close() error {
	socket.mu.Lock()
	socket.closed++
	socket.mu.Unlock()
	return nil
}

func newTestClient(callbacks Callbacks) (*Client, *fakeSocket) {
	socket := &fakeSocket{}
	client := NewClient("test-key", validOptions(), callbacks)
	client.attachSocket(socket)
	return client, socket
}

func TestLifecycleWireContract(t *testing.T) {
	client, socket := newTestClient(Callbacks{})
	if err := client.Initialize(InitializationOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.SendText("Hello", false); err != nil {
		t.Fatal(err)
	}
	if err := client.Flush(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Finish(ctx) }()
	time.Sleep(10 * time.Millisecond)
	if err := client.HandleMessage([]byte(`{"isFinal":true}`), false); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	var payloads []map[string]any
	for _, data := range socket.sent {
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, payload)
	}
	if len(payloads) != 4 {
		t.Fatalf("got %d payloads", len(payloads))
	}
	if payloads[2]["flush"] != true || payloads[3]["text"] != "" {
		t.Fatalf("unexpected finish wire: %#v", payloads)
	}
}

func TestAudioTimestampAndBinaryRouting(t *testing.T) {
	var mu sync.Mutex
	var audio [][]byte
	var timestamps []any
	client, _ := newTestClient(Callbacks{
		OnAudio: func(data []byte) {
			mu.Lock()
			audio = append(audio, data)
			mu.Unlock()
		},
		OnBinaryAudio: func(data []byte) {
			mu.Lock()
			audio = append(audio, data)
			mu.Unlock()
		},
		OnTimestamps: func(value any) {
			mu.Lock()
			timestamps = append(timestamps, value)
			mu.Unlock()
		},
	})
	if err := client.Initialize(InitializationOptions{}); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("one"))
	if err := client.HandleMessage(
		[]byte(`{"audio":"`+encoded+`","alignment":{"chars":["o"]}}`),
		false,
	); err != nil {
		t.Fatal(err)
	}
	if err := client.HandleMessage([]byte("two"), true); err != nil {
		t.Fatal(err)
	}
	if err := client.DrainCallbacks(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(audio) != 2 || string(audio[0]) != "one" || string(audio[1]) != "two" {
		t.Fatalf("unexpected audio: %#v", audio)
	}
	if len(timestamps) != 1 {
		t.Fatalf("timestamps not delivered")
	}
}

func TestTerminalErrorIsSticky(t *testing.T) {
	client, _ := newTestClient(Callbacks{})
	if err := client.Initialize(InitializationOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.HandleMessage(
		[]byte(`{"error":"INVALID_PARAMETER","message":"bad"}`),
		false,
	); err == nil {
		t.Fatal("expected terminal error")
	}
	if err := client.SendText("late", false); err == nil {
		t.Fatal("sticky error expected")
	}
}

func TestCloseAfterFinalDoesNotReplaceSuccess(t *testing.T) {
	client, _ := newTestClient(Callbacks{})
	if err := client.Initialize(InitializationOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Finish(ctx) }()
	time.Sleep(10 * time.Millisecond)
	_ = client.HandleMessage([]byte(`{"is_final":true}`), false)
	client.HandleClose(1000, "normal")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReadLoopTransportFailureAfterFinalDoesNotReplaceSuccess(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{name: "EOF", err: io.EOF},
		{
			name: "connection reset",
			err:  errors.New("read tcp: wsarecv: connection forcibly closed"),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var errorCount int
			socket := &scriptedReadSocket{reads: []scriptedRead{
				{
					messageType: websocket.TextMessage,
					payload:     []byte(`{"isFinal":true}`),
				},
				{err: testCase.err},
			}}
			client := NewClient(
				"test-key",
				validOptions(),
				Callbacks{OnError: func(error) { errorCount++ }},
			)
			client.attachSocket(socket)
			defer client.Close()

			client.readLoop(socket)
			if err := client.Finish(context.Background()); err != nil {
				t.Fatalf("final success was replaced: %T %v", err, err)
			}
			if err := client.DrainCallbacks(context.Background()); err != nil {
				t.Fatal(err)
			}
			if errorCount != 0 {
				t.Fatalf("unexpected error callbacks: %d", errorCount)
			}
		})
	}
}

func TestReadLoopTransportFailureBeforeFinalRemainsError(t *testing.T) {
	var callbackError error
	socket := &scriptedReadSocket{reads: []scriptedRead{{err: io.EOF}}}
	client := NewClient(
		"test-key",
		validOptions(),
		Callbacks{OnError: func(err error) { callbackError = err }},
	)
	client.attachSocket(socket)
	defer client.Close()

	client.readLoop(socket)
	err := client.Finish(context.Background())
	var transport *TransportError
	if !errors.As(err, &transport) {
		t.Fatalf("expected TransportError, got %T %v", err, err)
	}
	if err := client.DrainCallbacks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !errors.As(callbackError, &transport) {
		t.Fatalf("expected TransportError callback, got %T %v", callbackError, callbackError)
	}
}

func TestCallbackPanicIsIsolated(t *testing.T) {
	var callbackError error
	client, _ := newTestClient(Callbacks{
		OnAudio:         func([]byte) { panic("boom") },
		OnCallbackError: func(err error) { callbackError = err },
	})
	if err := client.Initialize(InitializationOptions{}); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("one"))
	_ = client.HandleMessage([]byte(`{"audio":"`+encoded+`"}`), false)
	if err := client.DrainCallbacks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if callbackError == nil {
		t.Fatal("callback panic was not reported")
	}
}

func TestDoubleCloseIsIdempotent(t *testing.T) {
	client, socket := newTestClient(Callbacks{})
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if socket.closed != 1 {
		t.Fatalf("socket closed %d times", socket.closed)
	}
	select {
	case <-client.callbackDone:
	case <-time.After(time.Second):
		t.Fatal("callback worker did not stop")
	}
}

func TestFinishTimeoutIsTransportError(t *testing.T) {
	client, _ := newTestClient(Callbacks{})
	if err := client.Initialize(InitializationOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := client.Finish(ctx)
	var transport *TransportError
	if !errors.As(err, &transport) {
		t.Fatalf("expected TransportError, got %T %v", err, err)
	}
}

func TestCallbackQueueOverflowReturnsExplicitErrorWithoutDeadlock(t *testing.T) {
	block := make(chan struct{})
	client, _ := newTestClient(Callbacks{
		OnAudio: func([]byte) { <-block },
		OnError: func(error) {},
	})
	if err := client.Initialize(InitializationOptions{}); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("one"))
	var terminal error
	for index := 0; index < 300; index++ {
		terminal = client.HandleMessage(
			[]byte(`{"audio":"`+encoded+`"}`),
			false,
		)
		if terminal != nil {
			break
		}
	}
	close(block)
	if terminal == nil {
		t.Fatal("expected callback backpressure error")
	}
	var protocol *ProtocolError
	if !errors.As(terminal, &protocol) ||
		protocol.Code != "CALLBACK_BACKPRESSURE" {
		t.Fatalf("unexpected terminal error: %T %v", terminal, terminal)
	}
}
