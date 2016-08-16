package discord

import (
	"context"
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
	ctx := context.Background()
	gateway, err := rest.GetGateway(ctx)
	if err != nil {
		return nil, err
	}

	discordLogger.WithField("gateway", gateway).Debug("Fetched gateway")

	ws, err := NewWebSocketClient(logger, botId, token, gateway)
	if err != nil {
		return nil, err
	}

	return &DiscordClient{
		logger:          discordLogger,
		RestClient:      rest,
		WebSocketClient: ws,
	}, nil
}
