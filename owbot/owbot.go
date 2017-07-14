package owbot

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/verath/owbot-bot/owbot/discord"
	"github.com/verath/owbot-bot/owbot/overwatch"
	"path/filepath"
)

var (
	// The url to the project github page
	GitHubUrl = "https://github.com/verath/owbot-bot"

	// The git revision currently running. This value is set during the build
	// using '-ldflags "-X github.com/verath/owbot-bot/owbot.GitRevision=..."'
	GitRevision = ""
)

// The bot is the main component of the ow-bot. It handles events
// from Discord and uses the overwatch client to respond to queries.
type Bot struct {
	logger     *logrus.Entry
	overwatch  *overwatch.OverwatchClient
	discord    *discord.DiscordClient
	userSource UserSource
}

func (bot *Bot) onSessionReady() {
	bot.logger.Info("onSessionReady, setting status message")
	bot.discord.UpdateStatus(-1, "!ow help")
}

// Starts the bot, connects to Discord and starts listening for events
func (bot *Bot) Start() error {
	// TODO: Check that we are not started

	bot.logger.WithField("revision", GitRevision).Info("Bot starting, connecting...")

	bot.discord.AddReadyHandler(bot.onSessionReady)
	bot.discord.AddMessageHandler(bot.onChannelMessage)

	if err := bot.discord.Connect(); err != nil {
		bot.logger.WithField("error", err).Error("Could not connect to Discord")
		return err
	}
	bot.logger.Debug("Connected to Discord")

	return nil
}

// Stops the bot, disconnecting from Discord
func (bot *Bot) Stop() error {
	// TODO: Check that we are started

	bot.logger.Info("Bot stopping, disconnecting...")
	if err := bot.discord.Disconnect(); err != nil {
		bot.logger.WithField("error", err).Error("Failed to disconnect from Discord")
		return err
	}
	bot.logger.Info("Disconnected from Discord")

	// TODO: Remove added handlers

	return nil
}

// Creates a new bot.
func NewBot(logger *logrus.Logger, db *bolt.DB, botId string, token string) (*Bot, error) {
	overwatch, err := overwatch.NewOverwatchClient(logger)
	if err != nil {
		return nil, err
	}

	userAgent := fmt.Sprintf("DiscordBot (%s, %s)", GitHubUrl, GitRevision)
	discord, err := discord.NewDiscord(logger, botId, token, userAgent)
	if err != nil {
		return nil, err
	}

	// Store the logger as an Entry, adding the module to all log calls
	botLogger := logger.WithField("module", "main")

	// If we have a bolt database, use the BoltUserSource. Else fallback
	// to an in memory user source
	var userSource UserSource
	if db == nil {
		botLogger.Info("No db provided, using in-memory user source")
		userSource = NewMemoryUserSource()
	} else {
		path, err := filepath.Abs(db.Path())
		if err != nil {
			return nil, err
		}
		botLogger.WithField("Path", path).Info("Using Bolt db user source")
		userSource, err = NewBoltUserSource(logger, db)
		if err != nil {
			return nil, err
		}
	}

	return &Bot{
		logger:     botLogger,
		discord:    discord,
		overwatch:  overwatch,
		userSource: userSource,
	}, nil
}
