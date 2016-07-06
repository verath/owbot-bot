package discord

import (
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
	"sync"
	"time"
)

type ReadyHandler func(session *Session)
type MessageHandler func(session *Session, message *Message)

type Session struct {
	sync.RWMutex

	logger *logrus.Entry

	handlersLock  sync.RWMutex
	readyHandlers []ReadyHandler
	msgHandlers   []MessageHandler

	botId string // The botId of the account
	token string // The secret token of the account

	sendChan chan GatewayPayload // A channel used for queuing payloads to send
	conn     *websocket.Conn     // The websocket connection to Discord

	isReady           bool          // A flag used to know if the ready event has been received
	gatewayUrl        string        // A cached value of the Discord wss url.
	heartbeatInterval time.Duration // The heartbeat interval to use for heartbeats
	seqNum            *int          // The latest sequence number received, used in heartbeats
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

func NewSession(logger *logrus.Logger, botId string, token string) (*Session, error) {
	// Store the logger as an Entry, adding the module to all log calls
	discordLogger := logger.WithField("module", "discord")

	return &Session{
		logger:   discordLogger,
		botId:    botId,
		token:    token,
		sendChan: make(chan GatewayPayload),
	}, nil
}
