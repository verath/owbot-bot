package discord

import (
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"log"
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

	// Get and cache the socket gateway URL
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

func (s *Session) sendPayload(payload GatewayPayload) {
	s.sendChan <- payload
}

// listenRecv listens for data from Discord, and dispatches it on a new go-routine
func (s *Session) listenRecv() {
	for {
		payload := RawGatewayPayload{}

		s.RLock()
		conn := s.conn
		s.RUnlock()

		err := conn.ReadJSON(&payload)
		if err != nil {
			log.Fatal(err)
		}
		go s.handlePayload(payload)
	}
}

// Send listens for data to be sent to Discord, sent on the s.sendChan
func (s *Session) listenSend(sendChan <-chan GatewayPayload) {
	for {
		payload := <-sendChan

		s.RLock()
		conn := s.conn
		s.RUnlock()

		if err := conn.WriteJSON(payload); err != nil {
			log.Println("Error sending payload", err)
		} else {
			log.Printf("Sent payload, OP: %d, Name: %s",
				payload.OpCode, PayloadOpToName(payload.OpCode))
		}
	}
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
			log.Printf("Heartbeat interval set to: %d ms", interval)
		}
		<-ticker.C
	}
}

// "Entry point" for handling incoming payloads. Dispatches known payloads
// to their appropriate handler
func (s *Session) handlePayload(payload RawGatewayPayload) {
	log.Printf("Recv payload, OP: %d, Name: %s", payload.OpCode, PayloadOpToName(payload.OpCode))

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
		log.Fatal(err)
	}

	log.Printf("Got Gateway Hello, HeartbeatInterval: %d ms", helloData.HeartbeatInterval)

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

	log.Printf("Got Gateway Dispatch, EventName: %s, SeqNum: %d", eventName, seqNumber)

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
		log.Println("Got EventReady, starting heartbeat")
		go s.heartbeat()
		s.onReady()
	} else {
		log.Println("Got EventReady, but was already ready")
	}
}

// Handler for the MessageCreateEvent, the event sent when someone sends
// a new message to a channel.
func (s *Session) handleEventMessageCreate(data json.RawMessage) {
	messageCreate := EventMessageCreate{}
	err := json.Unmarshal(data, &messageCreate)
	if err != nil {
		log.Fatal(err)
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
