package discord

import (
	"encoding/json"
	"errors"
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	GATEWAY_VERSION = 5
)

type ReadyHandler func()
type MessageHandler func(message *Message)

type WebSocketClient struct {
	mu     sync.RWMutex
	logger *logrus.Entry

	// A WaitGroup used when Disconnecting to block until all
	// running goroutines have finished
	waitGroup *sync.WaitGroup

	botId      string // The botId of the account
	token      string // The secret token of the account
	gatewayUrl string // A cached value of the Discord wss url

	conn  *websocket.Conn  // The websocket connection to Discord
	close chan interface{} // A channel used to signal that the connection should be closed
	ready chan interface{} // A channel used to signal when the ready event is received

	seqNum            *int          // The latest sequence number received, used in heartbeats
	heartbeatInterval time.Duration // The interval in which heartbeats should be sent

	handlersMu    sync.RWMutex
	readyHandlers []ReadyHandler
	msgHandlers   []MessageHandler
}

func NewWebSocketClient(logger *logrus.Logger, botId string, token string, gateway *Gateway) (*WebSocketClient, error) {
	// Store the logger as an Entry, adding the module to all log calls
	discordLogger := logger.WithField("module", "discord-websocket")

	// Add Gateway version and encoding params to websocket url
	// https://discordapp.com/developers/docs/topics/gateway#gateway-url-params
	gatewayUrl, err := url.Parse(gateway.Url)
	if err != nil {
		return nil, err
	}
	query := gatewayUrl.Query()
	query.Set("encoding", "json")
	query.Set("v", strconv.Itoa(GATEWAY_VERSION))
	gatewayUrl.RawQuery = query.Encode()
	logger.WithField("gatewayUrl", gatewayUrl).Debug("Parsed GatewayURL")

	return &WebSocketClient{
		logger:     discordLogger,
		waitGroup:  &sync.WaitGroup{},
		botId:      botId,
		token:      token,
		gatewayUrl: gatewayUrl.String(),
	}, nil
}

// Connects to the Discord websocket server, starts
// to listen for events.
func (sc *WebSocketClient) Connect() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.connect()
}

func (sc *WebSocketClient) connect() error {
	if sc.conn != nil {
		return errors.New("Already connected")
	}

	conn, _, err := websocket.DefaultDialer.Dial(sc.gatewayUrl, nil)
	if err != nil {
		return err
	}
	sc.conn = conn

	// Create channels for close and ready signals
	sc.close = make(chan interface{})
	sc.ready = make(chan interface{})

	sc.waitGroup.Add(2)
	go sc.listen(sc.conn, sc.close)
	go sc.heartbeat(sc.conn, sc.close, sc.ready)
	return nil
}

// Disconnects from the Discord websocket server, stops
// listening for events
func (sc *WebSocketClient) Disconnect() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.disconnect()
}

func (sc *WebSocketClient) disconnect() error {
	if sc.conn == nil {
		return errors.New("Not connected")
	}

	// Signaling our goroutines to stop. We also "force close"
	// the conn here, to interrupt the listen goroutine
	close(sc.close)
	sc.close = nil
	err := sc.conn.Close()
	sc.conn = nil

	// Wait until all the goroutines are closed before returning
	sc.waitGroup.Wait()
	return err
}

// Updates the current user's status.
// If idleSince >= 0, the user's idle time is set to the time specified
// If gameName != "", the value is set as the currently playing game for the user
// https://discordapp.com/developers/docs/topics/gateway#gateway-status-update
func (sc *WebSocketClient) UpdateStatus(idleSince int, gameName string) error {
	data := GatewayStatusUpdateData{}
	if idleSince >= 0 {
		data.IdleSince = &idleSince
	}
	if gameName != "" {
		data.Game = &StatusUpdateGame{gameName}
	}
	payload := NewGatewayPayload(PAYLOAD_GATEWAY_STATUS_UPDATE, data, nil, nil)

	sc.sendPayload(payload)
	return nil
}

func (sc *WebSocketClient) listen(conn *websocket.Conn, close <-chan interface{}) {
	defer sc.waitGroup.Done()

	for {
		select {
		case <-close:
			sc.logger.Info("Close signal in listen, stopping")
			return
		default:
		}

		// Read raw message from websocket. This is used over e.g. ReadJson
		// so that know that any error was from reading from the socket
		_, r, err := conn.NextReader()
		if err != nil {
			// On error, stop listening and try reconnect. Reconnect is run
			// on a new goroutine so that the current one can quit
			sc.logger.WithField("error", err).Error("Error reading from socket")
			go sc.reconnect(conn)
			return
		}

		payload := RawGatewayPayload{}
		if err := json.NewDecoder(r).Decode(&payload); err != nil {
			sc.logger.WithField("error", err).Error("Error decoding received payload")
		} else {
			go sc.handlePayload(payload)
		}
	}
}

func (sc *WebSocketClient) heartbeat(conn *websocket.Conn, close <-chan interface{}, ready <-chan interface{}) {
	defer sc.waitGroup.Done()

	select {
	case <-ready:
	case <-close:
		sc.logger.Info("Close signal in heartbeat while awaiting ready, stopping")
		return
	}

	sc.mu.RLock()
	ticker := time.NewTicker(sc.heartbeatInterval)
	sc.mu.RUnlock()
	for {
		sc.mu.RLock()
		seqNum := sc.seqNum
		sc.mu.RUnlock()

		payload := GatewayPayload{PAYLOAD_GATEWAY_HEARTBEAT, seqNum, nil, nil}

		go sc.sendPayload(payload)

		select {
		case <-close:
			sc.logger.Info("Close signal in heartbeat, stopping")
			return
		case <-ticker.C:
		}

	}
}

func (sc *WebSocketClient) sendPayload(payload GatewayPayload) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// TODO: how to handle errors when sending?
	logFields := sc.logger.WithFields(logrus.Fields{
		"op":      payload.OpCode,
		"op-name": PayloadOpToName(payload.OpCode),
	})

	if sc.conn == nil {
		err := errors.New("No connection")
		logFields.WithField("error", err).Error("Error sending payload")
		return
	}
	if err := sc.conn.WriteJSON(payload); err != nil {
		logFields.WithField("error", err).Error("Error sending payload")
		return
	}
	logFields.Debug("Sent payload")
}

func (sc *WebSocketClient) reconnect(conn *websocket.Conn) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Make sure the connection we had when trying to reconnect is still
	// the current connection. If it is not, it means a connect/disconnect
	// was already performed, and we should not continue with the reconnection
	if sc.conn != conn {
		sc.logger.Info("Connection changed in reconnect, aborting")
		return
	}

	err := sc.disconnect()
	if err != nil {
		sc.logger.WithField("error", err).Error("Failed to disconnect")
	}

	backoff := 1 * time.Second
	for {
		err := sc.connect()
		if err == nil {
			break
		}
		// Try again with backoff, capped at 15 minutes
		if backoff *= 2; backoff > 15*time.Minute {
			backoff = 15 * time.Minute
		}
		sc.logger.WithFields(logrus.Fields{
			"backoff": backoff,
			"error":   err,
		}).Warn("Failed to reconnect")
		time.Sleep(backoff)
	}
	sc.logger.Info("Reconnected")
}

// "Entry point" for handling incoming payloads. Dispatches known payloads
// to their appropriate handler
func (sc *WebSocketClient) handlePayload(payload RawGatewayPayload) {
	sc.logger.WithFields(logrus.Fields{
		"op":      payload.OpCode,
		"op-name": PayloadOpToName(payload.OpCode),
	}).Debug("Received payload")

	switch payload.OpCode {
	case PAYLOAD_GATEWAY_HELLO:
		sc.handlePayloadHello(payload)
	case PAYLOAD_GATEWAY_DISPATCH:
		sc.handlePayloadDispatch(payload)
	}
}

// Handler for GatewayHello payloads
func (sc *WebSocketClient) handlePayloadHello(payload RawGatewayPayload) {
	helloData := GatewayHelloData{}
	if err := json.Unmarshal(payload.Data, &helloData); err != nil {
		sc.logger.WithField("error", err).Error("Failed unmarshaling GatewayHello")
	}

	heartbeatInterval := helloData.HeartbeatInterval * time.Millisecond
	sc.logger.WithField("HeartbeatInterval", heartbeatInterval).Info("Got Gateway Hello")

	sc.mu.Lock()
	sc.heartbeatInterval = heartbeatInterval
	sc.mu.Unlock()

	sc.sendIdentify()
}

// Sends our identify payload to the server
func (sc *WebSocketClient) sendIdentify() {
	sc.mu.RLock()
	token := sc.token
	sc.mu.RUnlock()

	data := GatewayIdentifyData{
		Token:      token,
		Compress:   false,
		Properties: IdentifyDataProperties{"Windows", "Testing", "", "", ""},
	}
	payload := NewGatewayPayload(PAYLOAD_GATEWAY_IDENTIFY, data, nil, nil)
	sc.sendPayload(payload)
}

// Handler for GatewayDispatch payloads (i.e. payloads that "holds" events)
// Reads the event name and dispatches known events to the appropriate handler
func (sc *WebSocketClient) handlePayloadDispatch(payload RawGatewayPayload) {
	eventName := *payload.EventName
	seqNumber := *payload.SeqNumber

	sc.logger.WithFields(logrus.Fields{
		"event": eventName,
		"seq":   seqNumber,
	}).Debug("Got Gateway Dispatch")

	// Update the sequence number, if new number is higher
	sc.mu.Lock()
	if sc.seqNum == nil || *sc.seqNum < seqNumber {
		sc.seqNum = &seqNumber
	}
	sc.mu.Unlock()

	switch eventName {
	case "READY":
		sc.handleEventReady(payload.Data)
	case "MESSAGE_CREATE":
		sc.handleEventMessageCreate(payload.Data)
	}
}

// Handler for the EventReady event
func (sc *WebSocketClient) handleEventReady(data json.RawMessage) {
	// Close the ready channel, signaling to the heartbeat goroutine to start
	sc.mu.RLock()
	close(sc.ready)
	sc.mu.RUnlock()

	// Dispatch the onReady event to external listeners
	sc.onReady()
}

// Handler for the MessageCreateEvent, the event sent when someone sends
// a new message to a channel.
func (sc *WebSocketClient) handleEventMessageCreate(data json.RawMessage) {
	messageCreate := EventMessageCreate{}
	err := json.Unmarshal(data, &messageCreate)
	if err != nil {
		sc.logger.WithField("error", err).Error("Failed unmarshaling EventMessageCreate")
		return
	}

	sc.onMessage(messageCreate.Message)
}

//
// External event handler registration and dispatching
//

func (sc *WebSocketClient) AddReadyHandler(handler ReadyHandler) {
	sc.handlersMu.Lock()
	sc.readyHandlers = append(sc.readyHandlers, handler)
	sc.handlersMu.Unlock()
}

func (sc *WebSocketClient) AddMessageHandler(handler MessageHandler) {
	sc.handlersMu.Lock()
	sc.msgHandlers = append(sc.msgHandlers, handler)
	sc.handlersMu.Unlock()
}

func (sc *WebSocketClient) onReady() {
	sc.handlersMu.RLock()
	defer sc.handlersMu.RUnlock()
	for _, handler := range sc.readyHandlers {
		go handler()
	}
}

func (sc *WebSocketClient) onMessage(msg *Message) {
	if msg.Author.Bot {
		// Discard message from bots (including us)
		sc.logger.WithFields(logrus.Fields{
			"username":      msg.Author.Username,
			"discriminator": msg.Author.Discriminator,
		}).Debug("Ignoring message sent by bot")
		return
	}
	sc.handlersMu.RLock()
	defer sc.handlersMu.RUnlock()
	for _, handler := range sc.msgHandlers {
		go handler(msg)
	}
}
