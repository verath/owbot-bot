package discord

import (
	"encoding/json"
	"errors"
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
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
	sync.RWMutex

	logger *logrus.Entry

	handlersLock  sync.RWMutex
	readyHandlers []ReadyHandler
	msgHandlers   []MessageHandler

	botId      string // The botId of the account
	token      string // The secret token of the account
	gatewayUrl string // A cached value of the Discord wss url.

	sendChan chan GatewayPayload // A channel used for queuing payloads to send
	conn     *websocket.Conn     // The websocket connection to Discord

	isReady           bool          // A flag used to know if the ready event has been received
	heartbeatInterval time.Duration // The heartbeat interval to use for heartbeats
	seqNum            *int          // The latest sequence number received, used in heartbeats
}

func NewWebSocketClient(logger *logrus.Logger, botId string, token string, gatewayUrl string) (*WebSocketClient, error) {
	// Store the logger as an Entry, adding the module to all log calls
	discordLogger := logger.WithField("module", "discord-websocket")

	// Make sure we connect to correct protocol
	gatewayUrl += "?encoding=json&v=" + strconv.Itoa(GATEWAY_VERSION)

	return &WebSocketClient{
		logger:     discordLogger,
		botId:      botId,
		token:      token,
		gatewayUrl: gatewayUrl,
		sendChan:   make(chan GatewayPayload),
	}, nil
}

func (sc *WebSocketClient) AddReadyHandler(handler ReadyHandler) {
	sc.handlersLock.Lock()
	sc.readyHandlers = append(sc.readyHandlers, handler)
	sc.handlersLock.Unlock()
}

func (sc *WebSocketClient) AddMessageHandler(handler MessageHandler) {
	sc.handlersLock.Lock()
	sc.msgHandlers = append(sc.msgHandlers, handler)
	sc.handlersLock.Unlock()
}

// Connects to the Discord websocket server, starting
// to listen for events.
func (sc *WebSocketClient) Connect() error {
	sc.Lock()
	defer sc.Unlock()

	if sc.conn != nil {
		return errors.New("Already connected")
	}

	conn, _, err := websocket.DefaultDialer.Dial(sc.gatewayUrl, nil)
	if err != nil {
		return err
	}
	sc.conn = conn

	go sc.listenRecv()
	go sc.listenSend(sc.sendChan)
	return nil
}

// Updates the current user's status.
// If idleSince >= 0, the user's idle time is set to the time specified
// If gameName != "", the value is set as the currently playing game for the user
// https://discordapp.com/developers/docs/topics/gateway#gateway-status-update
func (sc *WebSocketClient) UpdateStatus(idleSince int, gameName string) error {
	sc.Lock()
	defer sc.Unlock()

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

// listenRecv listens for data from Discord, and dispatches it on a new go-routine.
// listenRecv stops itself, and calls reconnect, when failing to read from the socket.
func (sc *WebSocketClient) listenRecv() {
	sc.RLock()
	conn := sc.conn
	sc.RUnlock()
	for {
		// Read raw message from websocket conn. On error, stop the current
		// listenRecv loop and start trying to reconnect
		_, r, err := conn.NextReader()
		if err != nil {
			sc.logger.WithField("error", err).Error("Error reading from socket")
			sc.reconnect()
			break
		}

		// Parse the message as json and dispatch
		payload := RawGatewayPayload{}
		if err = json.NewDecoder(r).Decode(&payload); err != nil {
			sc.logger.Error("Error decoding received payload", err)
			continue
		}
		go sc.handlePayload(payload)
	}
}

// Send listens for data to be sent to Discord, sent on the s.sendChan.
// If a message can not be sent due to any reason, the message is discarded.
func (sc *WebSocketClient) listenSend(sendChan <-chan GatewayPayload) {
	for {
		payload := <-sendChan

		sc.RLock()
		conn := sc.conn
		sc.RUnlock()

		// TODO: Ensure we don't write during reconnection
		err := conn.WriteJSON(payload)

		logFields := sc.logger.WithFields(logrus.Fields{
			"op":      payload.OpCode,
			"op-name": PayloadOpToName(payload.OpCode),
		})
		if err != nil {
			logFields.WithField("error", err).Error("Error sending payload")
		} else {
			logFields.Debug("Sent payload")
		}
	}
}

func (sc *WebSocketClient) reconnect() {
	sc.RLock()
	sc.conn.Close()
	sc.RUnlock()

	var conn *websocket.Conn
	var err error
	var backoff time.Duration = 1 * time.Second
	for {
		conn, _, err = websocket.DefaultDialer.Dial(sc.gatewayUrl, nil)
		if err != nil {
			// Try again after backoff, capped at 15 minutes
			if backoff *= 2; backoff > 15*time.Minute {
				backoff = 15 * time.Minute
			}

			sc.logger.WithField("backoff", backoff).Warn("Failed to reconnect, trying again")
			time.Sleep(backoff)
		} else {
			break
		}
	}

	sc.Lock()
	sc.conn = conn
	sc.Unlock()

	go sc.listenRecv()
	sc.logger.Info("Reconnected!")
}

// sendPayload takes a payload and sends it on the sendChan channel.
// The method will block until the message is received by the listenSend
// func. However, sendPayload does not in any way guarantee that any message
// is actually sent.
func (sc *WebSocketClient) sendPayload(payload GatewayPayload) {
	sc.sendChan <- payload
}

func (sc *WebSocketClient) heartbeat() {
	var interval time.Duration
	var ticker *time.Ticker

	for {
		sc.RLock()
		seqNum := sc.seqNum
		newInterval := sc.heartbeatInterval
		sc.RUnlock()

		payload := GatewayPayload{PAYLOAD_GATEWAY_HEARTBEAT, seqNum, nil, nil}
		sc.sendPayload(payload)

		// Adjust ticker if heartbeatInterval changed
		if interval != newInterval {
			if ticker != nil {
				ticker.Stop()
			}
			ticker = time.NewTicker(newInterval)
			interval = newInterval

			sc.logger.WithField("interval", interval).Info("Heartbeat interval updated")
		}
		<-ticker.C
	}
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

	sc.Lock()
	sc.heartbeatInterval = heartbeatInterval
	sc.Unlock()

	sc.sendIdentify()
}

// Sends our identify payload to the server
func (sc *WebSocketClient) sendIdentify() {
	sc.RLock()
	token := sc.token
	sc.RUnlock()

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
	sc.Lock()
	if sc.seqNum == nil || *sc.seqNum < seqNumber {
		sc.seqNum = &seqNumber
	}
	sc.Unlock()

	switch eventName {
	case "READY":
		sc.handleEventReady(payload.Data)
	case "MESSAGE_CREATE":
		sc.handleEventMessageCreate(payload.Data)
	}
}

// Handler for the EventReady event
// Starts the heartbeat and notifies listeners on the initial EventReady
func (sc *WebSocketClient) handleEventReady(data json.RawMessage) {
	sc.Lock()
	wasReady := sc.isReady
	sc.isReady = true
	sc.Unlock()

	// Only act on the first EventReady, as following such events
	// are likely due to us reconnecting. For now, reconnection
	// is left hidden as an implementation detail.
	if !wasReady {
		sc.logger.Info("Got EventReady, starting heartbeat")
		go sc.heartbeat()
		sc.onReady()
	} else {
		sc.logger.Info("Got EventReady, but was already ready")
	}
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

func (sc *WebSocketClient) onReady() {
	sc.handlersLock.RLock()
	defer sc.handlersLock.RUnlock()
	for _, handler := range sc.readyHandlers {
		go handler()
	}
}

func (sc *WebSocketClient) onMessage(msg *Message) {
	if msg.Author.Id == sc.botId {
		// Don't notify on our own messages
		return
	}

	sc.handlersLock.RLock()
	defer sc.handlersLock.RUnlock()
	for _, handler := range sc.msgHandlers {
		go handler(msg)
	}
}
