package discord

import (
	"github.com/Sirupsen/logrus"
)

type DiscordClient struct {
	logger *logrus.Entry

	RestClient      *RestClient
	WebSocketClient *WebSocketClient
}

func NewDiscord(logger *logrus.Logger, botId string, token string) (*DiscordClient, error) {
	discordLogger := logger.WithField("module", "discord")

	rest, err := NewRestClient(logger, token)
	if err != nil {
		return nil, err
	}

	// Fetch the websocket gateway url
	gateway, err := rest.GetGateway()
	if err != nil {
		return nil, err
	}

	discordLogger.WithField("gateway", gateway.Url).Debug("Fetched gateway URL")

	ws, err := NewWebSocketClient(logger, botId, token, gateway.Url)
	if err != nil {
		return nil, err
	}

	return &DiscordClient{
		logger:          discordLogger,
		RestClient:      rest,
		WebSocketClient: ws,
	}, nil
}

func (discord *DiscordClient) AddReadyHandler(readyHandler ReadyHandler) {
	discord.WebSocketClient.AddReadyHandler(readyHandler)
}

func (discord *DiscordClient) AddMessageHandler(messageHandler MessageHandler) {
	discord.WebSocketClient.AddMessageHandler(messageHandler)
}

func (discord *DiscordClient) Connect() error {
	return discord.WebSocketClient.Connect()
}

func (discord *DiscordClient) UpdateStatus(idleSince int, gameName string) error {
	return discord.WebSocketClient.UpdateStatus(idleSince, gameName)
}

func (discord *DiscordClient) CreateMessage(channelId string, content string) error {
	return discord.RestClient.CreateMessage(channelId, content)
}

func (discord *DiscordClient) GetGateway() (*Gateway, error) {
	return discord.RestClient.GetGateway()
}
