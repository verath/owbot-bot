package owbot

import (
	"context"
	"github.com/bwmarrin/discordgo"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/verath/owbot-bot/owbot/owapi"
	"strings"
	"time"
)

const (
	// The url to the project github page
	gitHubURL = "https://github.com/verath/owbot-bot"

	// statusMessageHello is an initial message set when the
	// bot is first ready.
	statusMessageHello = "Hello!"
	// statusMessageHelloDuration determines for how long the
	// hello message should be displayed before showing the
	// default message
	statusMessageHelloDuration = 5 * time.Minute
	// statusMessageDefault is the default status message of
	// the bot, displayed as the "game playing" in Discord.
	statusMessageDefault = "!ow help"
)

// The git revision of the bot. The default value is overridden via:
// -ldflags during build.
var gitRevision = "master"

// Bot is the main component of the ow-bot. It handles events
// from Discord and uses the owapi client to respond to queries.
type Bot struct {
	logger         *logrus.Logger
	discordSession *discordgo.Session
	owAPIClient    *owapi.Client
	userSource     UserSource
}

func New(logger *logrus.Logger, discordToken string, userSource UserSource) (*Bot, error) {
	// Make sure the token is prefixed by "Bot "
	// see https://github.com/hammerandchisel/discord-api-docs/issues/119
	if !strings.HasPrefix(discordToken, "Bot ") {
		discordToken = "Bot " + discordToken
	}
	discordSession, err := discordgo.New(discordToken)
	if err != nil {
		return nil, errors.Wrap(err, "Error creating discordgo session")
	}
	owAPIClient, err := owapi.NewClient(logger)
	if err != nil {
		return nil, errors.Wrap(err, "Error creating owapi client")
	}
	return &Bot{
		logger:         logger,
		discordSession: discordSession,
		owAPIClient:    owAPIClient,
		userSource:     userSource,
	}, nil
}

func (bot *Bot) Run(ctx context.Context) error {
	defer bot.discordSession.AddHandler(bot.onReadyHandler)()
	defer bot.discordSession.AddHandler(bot.onMessageCreateHandler)()
	if err := bot.discordSession.Open(); err != nil {
		return errors.Wrap(err, "Error connecting to Discord")
	}
	<-ctx.Done()
	if err := bot.discordSession.Close(); err != nil {
		return errors.Wrap(err, "Error closing Discord connection")
	}
	return ctx.Err()
}

func (bot *Bot) onReadyHandler(s *discordgo.Session, m *discordgo.Ready) {
	bot.logger.Info("On ready, setting hello status message")
	err := s.UpdateStatus(-1, statusMessageHello)
	if err != nil {
		bot.logger.Errorf("Failed setting status message: %+v", err)
	}
	<-time.After(statusMessageHelloDuration)
	bot.logger.Info("On ready, setting default status message")
	err = s.UpdateStatus(-1, statusMessageDefault)
	if err != nil {
		bot.logger.Errorf("Failed setting status message: %+v", err)
	}
}

func (bot *Bot) onMessageCreateHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	err := bot.handleDiscordMessage(m.Message)
	if err != nil {
		bot.logger.Errorf("Error handling discord message: %+v", err)
	}
}
