package myvocalsinglewss

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Callbacks struct {
	OnAudio         func([]byte)
	OnBinaryAudio   func([]byte)
	OnTimestamps    func(any)
	OnComplete      func()
	OnError         func(error)
	OnCallbackError func(error)
	OnClose         func(int, string)
}

type writeSocket interface {
	WriteJSON(any) error
	WriteControl(int, []byte, time.Time) error
	Close() error
}

type readSocket interface {
	writeSocket
	ReadMessage() (int, []byte, error)
}

type Client struct {
	apiKey    string
	options   ConnectionOptions
	callbacks Callbacks

	mu          sync.Mutex
	writeMu     sync.Mutex
	socket      writeSocket
	connected   bool
	initialized bool
	finishing   bool
	final       bool
	closed      bool
	sticky      error
	done        chan struct{}
	doneOnce    sync.Once
	closeOnce   sync.Once

	callbackQueue   chan func()
	callbackWG      sync.WaitGroup
	workerOnce      sync.Once
	callbackMu      sync.Mutex
	callbackStop    chan struct{}
	callbackDone    chan struct{}
	callbacksClosed bool
}

func NewClient(
	apiKey string,
	options ConnectionOptions,
	callbacks Callbacks,
) *Client {
	return &Client{
		apiKey:        apiKey,
		options:       options.normalized(),
		callbacks:     callbacks,
		done:          make(chan struct{}),
		callbackQueue: make(chan func(), 256),
		callbackStop:  make(chan struct{}),
		callbackDone:  make(chan struct{}),
	}
}

func (client *Client) Connect(ctx context.Context) error {
	if client.apiKey == "" {
		return &ValidationError{Message: "api_key must not be empty"}
	}
	rawURL, err := client.options.BuildURL()
	if err != nil {
		return err
	}
	client.mu.Lock()
	if client.socket != nil && !client.closed {
		client.mu.Unlock()
		return &StateError{Message: "client is already connected"}
	}
	client.mu.Unlock()

	header := http.Header{}
	header.Set("api-key", client.apiKey)
	conn, response, err := websocket.DefaultDialer.DialContext(
		ctx,
		rawURL,
		header,
	)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		if response != nil &&
			(response.StatusCode == http.StatusUnauthorized ||
				response.StatusCode == http.StatusForbidden) {
			return &AuthenticationError{
				Message: "WebSocket authentication failed",
			}
		}
		return &TransportError{
			Message: "WebSocket connection failed",
			Cause:   err,
		}
	}
	client.attachSocket(conn)
	go client.readLoop(conn)
	return nil
}

func (client *Client) attachSocket(socket writeSocket) {
	client.mu.Lock()
	client.socket = socket
	client.connected = true
	client.closed = false
	client.mu.Unlock()
	client.workerOnce.Do(func() { go client.callbackLoop() })
}

func (client *Client) Initialize(options InitializationOptions) error {
	value, err := options.Wire()
	if err != nil {
		return err
	}
	client.mu.Lock()
	if err := client.requireConnectedLocked(); err != nil {
		client.mu.Unlock()
		return err
	}
	if client.initialized {
		client.mu.Unlock()
		return &StateError{Message: "client is already initialized"}
	}
	client.mu.Unlock()
	if err := client.send(value); err != nil {
		return err
	}
	client.mu.Lock()
	client.initialized = true
	client.mu.Unlock()
	return nil
}

func (client *Client) SendText(text string, earlyGeneration bool) error {
	if text == "" {
		return &ValidationError{Message: "text must not be empty"}
	}
	if err := client.requireActive(); err != nil {
		return err
	}
	return client.send(map[string]any{
		"text":            text,
		"earlyGeneration": earlyGeneration,
	})
}

func (client *Client) Flush() error {
	if err := client.requireActive(); err != nil {
		return err
	}
	return client.send(map[string]any{"text": "", "flush": true})
}

func (client *Client) Finish(ctx context.Context) error {
	client.mu.Lock()
	if client.sticky != nil {
		err := client.sticky
		client.mu.Unlock()
		return err
	}
	if client.final {
		client.mu.Unlock()
		return nil
	}
	if err := client.requireConnectedLocked(); err != nil {
		client.mu.Unlock()
		return err
	}
	if !client.initialized {
		client.mu.Unlock()
		return &StateError{Message: "client is not initialized"}
	}
	shouldSend := !client.finishing
	client.finishing = true
	client.mu.Unlock()
	if shouldSend {
		if err := client.send(map[string]any{"text": ""}); err != nil {
			return err
		}
	}
	select {
	case <-client.done:
		client.mu.Lock()
		err := client.sticky
		client.mu.Unlock()
		return err
	case <-ctx.Done():
		err := &TransportError{
			Message: "final event was not received before timeout",
			Cause:   ctx.Err(),
		}
		client.fail(err)
		_ = client.Close()
		return err
	}
}

func (client *Client) HandleMessage(payload []byte, binary bool) error {
	if binary && !looksLikeJSON(payload) {
		return client.dispatch(func() {
			if client.callbacks.OnBinaryAudio != nil {
				client.callbacks.OnBinaryAudio(append([]byte(nil), payload...))
			}
		})
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		protocol := &ProtocolError{
			Code:    "INVALID_JSON",
			Message: err.Error(),
		}
		client.fail(protocol)
		return protocol
	}

	if protocol := protocolError(value); protocol != nil {
		client.fail(protocol)
		return protocol
	}

	if encoded, ok := value["audio"].(string); ok && encoded != "" {
		audio, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			protocol := &ProtocolError{
				Code:    "INVALID_AUDIO",
				Message: err.Error(),
			}
			client.fail(protocol)
			return protocol
		}
		if err := client.dispatch(func() {
			if client.callbacks.OnAudio != nil {
				client.callbacks.OnAudio(audio)
			}
		}); err != nil {
			return err
		}
	}

	for _, key := range []string{
		"timestamps",
		"alignment",
		"normalizedAlignment",
		"normalized_alignment",
	} {
		if timestamp, exists := value[key]; exists && timestamp != nil {
			captured := timestamp
			if err := client.dispatch(func() {
				if client.callbacks.OnTimestamps != nil {
					client.callbacks.OnTimestamps(captured)
				}
			}); err != nil {
				return err
			}
		}
	}

	final := value["isFinal"] == true
	if value["isFinal"] == nil {
		final = value["is_final"] == true
	}
	if final {
		client.mu.Lock()
		wasFinal := client.final
		client.final = true
		client.mu.Unlock()
		client.doneOnce.Do(func() { close(client.done) })
		if !wasFinal {
			return client.dispatch(func() {
				if client.callbacks.OnComplete != nil {
					client.callbacks.OnComplete()
				}
			})
		}
	}
	return nil
}

func (client *Client) HandleClose(code int, reason string) {
	client.mu.Lock()
	wasFinal := client.final
	wasClosed := client.closed
	client.connected = false
	client.mu.Unlock()
	if !wasFinal && !wasClosed {
		client.fail(&TransportError{
			Message: fmt.Sprintf(
				"WebSocket closed before final (code %d): %s",
				code,
				reason,
			),
		})
	}
	_ = client.dispatch(func() {
		if client.callbacks.OnClose != nil {
			client.callbacks.OnClose(code, reason)
		}
	})
}

func (client *Client) handleTransportFailure(err error) {
	client.mu.Lock()
	wasFinal := client.final
	wasClosed := client.closed
	client.connected = false
	client.mu.Unlock()
	if wasFinal || wasClosed {
		return
	}
	client.fail(&TransportError{
		Message: "WebSocket receive failed",
		Cause:   err,
	})
}

func (client *Client) DrainCallbacks(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		client.callbackWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (client *Client) Close() error {
	var closeError error
	client.closeOnce.Do(func() {
		client.mu.Lock()
		client.closed = true
		client.connected = false
		socket := client.socket
		client.socket = nil
		client.mu.Unlock()
		if socket != nil {
			_ = socket.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(1000, ""),
				time.Now().Add(time.Second),
			)
			closeError = socket.Close()
		}
		client.callbackMu.Lock()
		if !client.callbacksClosed {
			client.callbacksClosed = true
			close(client.callbackStop)
		}
		client.callbackMu.Unlock()
	})
	return closeError
}

func (client *Client) send(value any) error {
	client.mu.Lock()
	if err := client.requireConnectedLocked(); err != nil {
		client.mu.Unlock()
		return err
	}
	socket := client.socket
	client.mu.Unlock()
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	if err := socket.WriteJSON(value); err != nil {
		transport := &TransportError{
			Message: "WebSocket send failed",
			Cause:   err,
		}
		client.fail(transport)
		return transport
	}
	return nil
}

func (client *Client) requireActive() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.requireConnectedLocked(); err != nil {
		return err
	}
	if !client.initialized {
		return &StateError{Message: "client is not initialized"}
	}
	if client.finishing || client.final {
		return &StateError{Message: "transaction is finishing or complete"}
	}
	return nil
}

func (client *Client) requireConnectedLocked() error {
	if client.sticky != nil {
		return client.sticky
	}
	if !client.connected || client.closed || client.socket == nil {
		return &StateError{Message: "client is not connected"}
	}
	return nil
}

func (client *Client) fail(err error) {
	client.mu.Lock()
	first := client.sticky == nil
	if client.sticky == nil {
		client.sticky = err
	}
	sticky := client.sticky
	client.mu.Unlock()
	client.doneOnce.Do(func() { close(client.done) })
	if !first || client.callbacks.OnError == nil {
		return
	}
	job := func() {
		if client.callbacks.OnError != nil {
			client.callbacks.OnError(sticky)
		}
	}
	queued, closed := client.enqueue(job)
	if !queued && !closed {
		go client.runCallbackSafely(job)
	}
}

func (client *Client) dispatch(job func()) error {
	if job == nil {
		return nil
	}
	queued, closed := client.enqueue(job)
	if queued || closed {
		return nil
	}
	err := &ProtocolError{
		Code:    "CALLBACK_BACKPRESSURE",
		Message: "callback queue capacity exceeded",
	}
	client.mu.Lock()
	if client.sticky == nil {
		client.sticky = err
	}
	client.mu.Unlock()
	client.doneOnce.Do(func() { close(client.done) })
	return err
}

func (client *Client) enqueue(job func()) (bool, bool) {
	client.callbackMu.Lock()
	defer client.callbackMu.Unlock()
	if client.callbacksClosed {
		return false, true
	}
	client.callbackWG.Add(1)
	select {
	case client.callbackQueue <- job:
		return true, false
	default:
		client.callbackWG.Done()
		return false, false
	}
}

func (client *Client) callbackLoop() {
	defer close(client.callbackDone)
	run := func(job func()) {
		func() {
			defer client.callbackWG.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					client.reportCallbackError(
						fmt.Errorf("callback panic: %v", recovered),
					)
				}
			}()
			job()
		}()
	}
	for {
		select {
		case job := <-client.callbackQueue:
			run(job)
		case <-client.callbackStop:
			for {
				select {
				case job := <-client.callbackQueue:
					run(job)
				default:
					return
				}
			}
		}
	}
}

func (client *Client) reportCallbackError(err error) {
	if client.callbacks.OnCallbackError == nil {
		return
	}
	func() {
		defer func() { _ = recover() }()
		client.callbacks.OnCallbackError(err)
	}()
}

func (client *Client) runCallbackSafely(job func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			client.reportCallbackError(
				fmt.Errorf("callback panic: %v", recovered),
			)
		}
	}()
	job()
}

func (client *Client) readLoop(socket readSocket) {
	for {
		messageType, payload, err := socket.ReadMessage()
		if err != nil {
			if closeError, ok := err.(*websocket.CloseError); ok {
				client.HandleClose(closeError.Code, closeError.Text)
			} else {
				client.handleTransportFailure(err)
			}
			return
		}
		_ = client.HandleMessage(
			payload,
			messageType == websocket.BinaryMessage,
		)
	}
}

func looksLikeJSON(payload []byte) bool {
	trimmed := bytes.TrimSpace(payload)
	return len(trimmed) > 0 &&
		(trimmed[0] == '{' || trimmed[0] == '[')
}

func protocolError(value map[string]any) *ProtocolError {
	var code string
	var message string
	if raw, ok := value["error"].(string); ok && raw != "" {
		code = raw
	}
	if nested, ok := value["error"].(map[string]any); ok {
		if raw, ok := nested["code"].(string); ok {
			code = raw
		}
		if raw, ok := nested["message"].(string); ok {
			message = raw
		}
	}
	for _, key := range []string{"code", "error_code"} {
		if code == "" {
			if raw, ok := value[key].(string); ok {
				code = raw
			}
		}
	}
	for _, key := range []string{"message", "error_message"} {
		if message == "" {
			if raw, ok := value[key].(string); ok {
				message = raw
			}
		}
	}
	if code == "" {
		return nil
	}
	return &ProtocolError{Code: code, Message: message}
}
