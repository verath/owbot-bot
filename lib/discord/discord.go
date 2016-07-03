package discord

import (
	"github.com/gorilla/websocket"
	"sync"
	"time"
)

type ReadyHandler func(session *Session)
type MessageHandler func(session *Session, message *Message)

type Session struct {
	sync.RWMutex

	handlersLock  sync.RWMutex
	readyHandlers []ReadyHandler
	msgHandlers   []MessageHandler

	botId string
	token string

	sendChan          chan GatewayPayload
	conn              *websocket.Conn
	heartbeatInterval time.Duration
	seqNum            *int
}

func (s *Session) AddReadyHandler(handler ReadyHandler) {
	s.handlersLock.Lock()
	s.readyHandlers = append(s.readyHandlers, handler)
	s.handlersLock.Unlock()
}

func (s *Session) AddMessageHandler(handler MessageHandler) {
	s.handlersLock.Lock()
	s.msgHandlers = append(s.msgHandlers, handler)
	s.handlersLock.Unlock()
}

func (s *Session) onReady() {
	s.handlersLock.RLock()
	defer s.handlersLock.RUnlock()
	for _, handler := range s.readyHandlers {
		go handler(s)
	}
}

func (s *Session) onMessage(msg *Message) {
	if msg.Author.Id == s.botId {
		// Don't notify on our own messages
		return
	}

	s.handlersLock.RLock()
	defer s.handlersLock.RUnlock()
	for _, handler := range s.msgHandlers {
		go handler(s, msg)
	}
}

func NewSession(botId string, token string) Session {
	s := Session{
		botId:    botId,
		token:    token,
		sendChan: make(chan GatewayPayload),
	}

	return s
}
