package discord

import (
	"github.com/Sirupsen/logrus"
)

type DiscordClient struct {
	*RestClient
	*WebSocketClient

	logger *logrus.Entry
}

func NewDiscord(logger *logrus.Logger, botId string, token string, userAgent string) (*DiscordClient, error) {
	discordLogger := logger.WithField("module", "discord")

	rest, err := NewRestClient(logger, token, userAgent)
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
