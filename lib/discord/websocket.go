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

func (s *Session) Connect() (err error) {
	s.Lock()
	defer s.Unlock()

	if s.conn != nil {
		return errors.New("Already connected")
	}

	gateway, err := s.GetGateway()
	if err != nil {
		return
	}

	url := gateway.Url + "?encoding=json&v=" + strconv.Itoa(GatewayVersion)
	s.conn, _, err = websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return
	}

	go s.listenRecv()
	go s.listenSend()
	return
}

func (s *Session) UpdateStatus(idleSince int, gameName string) error {
	s.Lock()
	defer s.Unlock()
	if s.conn == nil {
		return errors.New("Not connected")
	}

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

func (s *Session) listenRecv() {
	conn := s.conn
	for {
		payload := RawGatewayPayload{}
		if err := conn.ReadJSON(&payload); err != nil {
			log.Fatal(err)
		}
		log.Printf("Recv payload, OP: %d", payload.OpCode)
		go s.handlePayload(payload)
	}
}

func (s *Session) listenSend() {
	conn := s.conn
	sendChan := s.sendChan
	for {
		payload := <-sendChan
		if err := conn.WriteJSON(payload); err != nil {
			log.Fatal(err)
		}
		log.Printf("Send payload, OP: %d", payload.OpCode)
	}
}

func (s *Session) heartbeat() {
	ticker := time.NewTicker(s.heartbeatInterval * time.Millisecond)
	for {
		s.RLock()
		seqNum := s.seqNum
		s.RUnlock()

		payload := GatewayPayload{PAYLOAD_GATEWAY_HEARTBEAT, seqNum, nil, nil}
		s.sendPayload(payload)
		<-ticker.C
	}
}

func (s *Session) handlePayload(payload RawGatewayPayload) {
	switch payload.OpCode {
	case PAYLOAD_GATEWAY_HELLO:
		s.handlePayloadHello(payload)
	case PAYLOAD_GATEWAY_DISPATCH:
		s.handlePayloadDispatch(payload)
	}
}

func (s *Session) handlePayloadHello(payload RawGatewayPayload) {
	helloData := GatewayHelloData{}
	err := json.Unmarshal(payload.Data, &helloData)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Got Gateway Hello, HeartbeatInterval: %d ms", helloData.HeartbeatInterval)
	s.heartbeatInterval = helloData.HeartbeatInterval
	s.sendIdentify()
}

func (s *Session) handlePayloadDispatch(payload RawGatewayPayload) {
	eventName := *payload.EventName
	seqNumber := *payload.SeqNumber
	log.Printf("Got Gateway Dispatch, EventName: %s, SeqNum: %d", eventName, seqNumber)

	s.Lock()
	s.seqNum = &seqNumber
	s.Unlock()

	switch eventName {
	case "READY":
		s.handleEventReady(payload.Data)
	case "MESSAGE_CREATE":
		s.handleEventMessageCreate(payload.Data)
	}
}

func (s *Session) handleEventReady(data json.RawMessage) {
	ready := EventReady{}
	err := json.Unmarshal(data, &ready)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Got Event Ready, starting heartbeat")
	go s.heartbeat()
	s.onReady()
}

func (s *Session) handleEventMessageCreate(data json.RawMessage) {
	messageCreate := EventMessageCreate{}
	err := json.Unmarshal(data, &messageCreate)
	if err != nil {
		log.Fatal(err)
	}

	s.onMessage(messageCreate.Message)
}

func (s *Session) sendIdentify() {
	// Send our identify payload
	data := GatewayIdentifyData{
		Token:      s.token,
		Compress:   false,
		Properties: IdentifyDataProperties{"Windows", "Testing", "", "", ""},
	}
	payload := NewGatewayPayload(PAYLOAD_GATEWAY_IDENTIFY, data, nil, nil)
	s.sendPayload(payload)
}
