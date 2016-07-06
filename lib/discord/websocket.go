package discord

import (
	"encoding/json"
	"errors"
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
	"strconv"
	"time"
)

const (
	GatewayVersion = 5
)

// Connects to the Discord websocket server, starting
// to listen for events.
func (s *Session) Connect() error {
	s.Lock()
	defer s.Unlock()

	if s.conn != nil {
		return errors.New("Already connected")
	}

	// Get and cache the socket gateway URL, the cached value is used
	// for reconnecting
	if s.gatewayUrl == "" {
		gateway, err := s.GetGateway()
		if err != nil {
			return err
		}
		s.gatewayUrl = gateway.Url + "?encoding=json&v=" + strconv.Itoa(GatewayVersion)
	}

	conn, _, err := websocket.DefaultDialer.Dial(s.gatewayUrl, nil)
	if err != nil {
		return err
	}
	s.conn = conn

	go s.listenRecv()
	go s.listenSend(s.sendChan)
	return nil
}

// Updates the current user's status.
// If idleSince >= 0, the user's idle time is set to the time specified
// If gameName != "", the value is set as the currently playing game for the user
// https://discordapp.com/developers/docs/topics/gateway#gateway-status-update
func (s *Session) UpdateStatus(idleSince int, gameName string) error {
	s.Lock()
	defer s.Unlock()

	data := GatewayStatusUpdateData{}
	if idleSince >= 0 {
		data.IdleSince = &idleSince
	}
	if gameName != "" {
		data.Game = &StatusUpdateGame{gameName}
	}
	payload := NewGatewayPayload(PAYLOAD_GATEWAY_STATUS_UPDATE, data, nil, nil)
	s.sendPayload(payload)

	return nil
}

// listenRecv listens for data from Discord, and dispatches it on a new go-routine.
// listenRecv stops itself, and calls reconnect, when failing to read from the socket.
func (s *Session) listenRecv() {
	s.RLock()
	conn := s.conn
	s.RUnlock()
	for {
		// Read raw message from websocket conn. On error, stop the current
		// listenRecv loop and start trying to reconnect
		_, r, err := conn.NextReader()
		if err != nil {
			s.logger.WithField("error", err).Error("Error reading from socket")
			s.reconnect()
			break
		}

		// Parse the message as json and dispatch
		payload := RawGatewayPayload{}
		if err = json.NewDecoder(r).Decode(&payload); err != nil {
			s.logger.Error("Error decoding received payload", err)
			continue
		}
		go s.handlePayload(payload)
	}
}

// Send listens for data to be sent to Discord, sent on the s.sendChan.
// If a message can not be sent due to any reason, the message is discarded.
func (s *Session) listenSend(sendChan <-chan GatewayPayload) {
	for {
		payload := <-sendChan

		s.RLock()
		conn := s.conn
		s.RUnlock()

		// TODO: Ensure we don't write during reconnection
		err := conn.WriteJSON(payload)

		logFields := s.logger.WithFields(logrus.Fields{
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

func (s *Session) reconnect() {
	s.RLock()
	s.conn.Close()
	s.RUnlock()

	var conn *websocket.Conn
	var err error
	var backoff time.Duration = 1 * time.Second
	for {
		conn, _, err = websocket.DefaultDialer.Dial(s.gatewayUrl, nil)
		if err != nil {
			// Try again after backoff, capped at 15 minutes
			if backoff *= 2; backoff > 15*time.Minute {
				backoff = 15 * time.Minute
			}

			s.logger.WithField("backoff", backoff).Warn("Failed to reconnect, trying again")
			time.Sleep(backoff)
		} else {
			break
		}
	}

	s.Lock()
	s.conn = conn
	s.Unlock()

	go s.listenRecv()
	s.logger.Info("Reconnected!")
}

// sendPayload takes a payload and sends it on the sendChan channel.
// The method will block until the message is received by the listenSend
// func. However, sendPayload does not in any way guarantee that any message
// is actually sent.
func (s *Session) sendPayload(payload GatewayPayload) {
	s.sendChan <- payload
}

func (s *Session) heartbeat() {
	var interval time.Duration
	var ticker *time.Ticker

	for {
		s.RLock()
		seqNum := s.seqNum
		newInterval := s.heartbeatInterval
		s.RUnlock()

		payload := GatewayPayload{PAYLOAD_GATEWAY_HEARTBEAT, seqNum, nil, nil}
		s.sendPayload(payload)

		// Adjust ticker if heartbeatInterval changed
		if interval != newInterval {
			if ticker != nil {
				ticker.Stop()
			}
			ticker = time.NewTicker(newInterval * time.Millisecond)
			interval = newInterval

			s.logger.WithField("interval", interval).Info("Heartbeat interval updated")
		}
		<-ticker.C
	}
}

// "Entry point" for handling incoming payloads. Dispatches known payloads
// to their appropriate handler
func (s *Session) handlePayload(payload RawGatewayPayload) {
	s.logger.WithFields(logrus.Fields{
		"op":      payload.OpCode,
		"op-name": PayloadOpToName(payload.OpCode),
	}).Debug("Received payload")

	switch payload.OpCode {
	case PAYLOAD_GATEWAY_HELLO:
		s.handlePayloadHello(payload)
	case PAYLOAD_GATEWAY_DISPATCH:
		s.handlePayloadDispatch(payload)
	}
}

// Handler for GatewayHello payloads
func (s *Session) handlePayloadHello(payload RawGatewayPayload) {
	helloData := GatewayHelloData{}
	if err := json.Unmarshal(payload.Data, &helloData); err != nil {
		s.logger.WithField("error", err).Error("Failed unmarshaling GatewayHello")
	}

	s.logger.WithField("HeartbeatInterval", helloData.HeartbeatInterval).Info("Got Gateway Hello")

	s.Lock()
	s.heartbeatInterval = helloData.HeartbeatInterval
	s.Unlock()

	s.sendIdentify()
}

// Handler for GatewayDispatch payloads (i.e. payloads that "holds" events)
// Reads the event name and dispatches known events to the appropriate handler
func (s *Session) handlePayloadDispatch(payload RawGatewayPayload) {
	eventName := *payload.EventName
	seqNumber := *payload.SeqNumber

	s.logger.WithFields(logrus.Fields{
		"event": eventName,
		"seq":   seqNumber,
	}).Debug("Got Gateway Dispatch")

	// Update the sequence number, if new number is higher
	s.Lock()
	if s.seqNum == nil || *s.seqNum < seqNumber {
		s.seqNum = &seqNumber
	}
	s.Unlock()

	switch eventName {
	case "READY":
		s.handleEventReady(payload.Data)
	case "MESSAGE_CREATE":
		s.handleEventMessageCreate(payload.Data)
	}
}

// Handler for the EventReady event
// Starts the heartbeat and notifies listeners on the initial EventReady
func (s *Session) handleEventReady(data json.RawMessage) {
	s.Lock()
	wasReady := s.isReady
	s.isReady = true
	s.Unlock()

	// Only act on the first EventReady, as following such events
	// are likely due to us reconnecting. For now, reconnection
	// is left hidden as an implementation detail.
	if !wasReady {
		s.logger.Info("Got EventReady, starting heartbeat")
		go s.heartbeat()
		s.onReady()
	} else {
		s.logger.Info("Got EventReady, but was already ready")
	}
}

// Handler for the MessageCreateEvent, the event sent when someone sends
// a new message to a channel.
func (s *Session) handleEventMessageCreate(data json.RawMessage) {
	messageCreate := EventMessageCreate{}
	err := json.Unmarshal(data, &messageCreate)
	if err != nil {
		s.logger.WithField("error", err).Error("Failed unmarshaling EventMessageCreate")
		return
	}

	s.onMessage(messageCreate.Message)
}

// Sends our identify payload to the server
func (s *Session) sendIdentify() {
	s.RLock()
	token := s.token
	s.RUnlock()

	data := GatewayIdentifyData{
		Token:      token,
		Compress:   false,
		Properties: IdentifyDataProperties{"Windows", "Testing", "", "", ""},
	}
	payload := NewGatewayPayload(PAYLOAD_GATEWAY_IDENTIFY, data, nil, nil)
	s.sendPayload(payload)
}
