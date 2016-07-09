package owbot

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/verath/owbot-bot/owbot/discord"
	"github.com/verath/owbot-bot/owbot/overwatch"
	"os"
	"os/signal"
	"regexp"
	"strings"
)

var (
	GITHUB_URL = "https://github.com/verath/owbot-bot"
	REVISION   = ""
)

// TODO: Change to using templates
var HELP_MSG_USAGE = strings.TrimSpace(fmt.Sprintf(`
**ow-bot (version %s, %s)**
  - !ow - Shows Overwatch profile summary for your set BattleTag
  - !ow <BattleTag> - Shows Overwatch profile summary for <BattleTag>
  - !ow set <BattleTag> - Sets the BattleTag for your user to <BattleTag>`,
	REVISION, GITHUB_URL))
var MSG_HELP_SET_BATTLE_TAG = `I don't know your BattleTag. Use "!ow set <BattleTag>" to set it.`
var MSG_HELP_INVALID_BATTLE_TAG_FORMAT = `"%s" is not a valid BattleTag`
var MSG_HELP_UNKNOWN_COMMAND = `Sorry, but I don't know what you want. Type "!ow help" to show help.`

var MSG_OVERWATCH_PROFILE_FORMAT = `**%s** (Level: %d, Rank: %d)`
var MSG_ERROR_FETCHING_PROFILE_FORMAT = `Unable to fetch profile for %s.`

var BATTLE_TAG_REGEX = regexp.MustCompile(`^(?P<BattleTag>\w{3,12}#\d+)$`)

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

func (bot *Bot) fetchOverwatchProfile(battleTag string) string {
	stats, err := bot.overwatch.GetStats(battleTag)
	logrus.New()
	if err != nil {
		bot.logger.WithFields(logrus.Fields{
			"error":     err,
			"battleTag": battleTag,
		}).Warn("Could not get Overwatch stats")
		return fmt.Sprintf(MSG_ERROR_FETCHING_PROFILE_FORMAT, battleTag)
	} else {
		bot.logger.WithField("battleTag", battleTag).Info("Successfully got Overwatch stats")
		return fmt.Sprintf(MSG_OVERWATCH_PROFILE_FORMAT,
			battleTag,
			stats.OverallStats.Level,
			stats.OverallStats.CompRank)
	}

}

func (bot *Bot) onChannelMessage(msg *discord.Message) {
	if !strings.HasPrefix(msg.Content, "!ow") {
		return
	}
	args := strings.Split(msg.Content, " ")

	respMsg := ""
	mention := false

	if len(args) == 1 {
		user, err := bot.userSource.Get(msg.Author.Id)
		if err != nil {
			bot.logger.WithField("error", err).Error("Failed getting user from source")
			return
		}
		if user == nil {
			mention = true
			respMsg = MSG_HELP_SET_BATTLE_TAG
		} else {
			respMsg = bot.fetchOverwatchProfile(user.BattleTag)
		}
	} else if args[1] == "help" {
		respMsg = HELP_MSG_USAGE
	} else if BATTLE_TAG_REGEX.MatchString(args[1]) {
		battleTag := args[1]
		respMsg = bot.fetchOverwatchProfile(battleTag)
	} else if args[1] == "set" && len(args) >= 3 {
		battleTag := args[2]
		if BATTLE_TAG_REGEX.MatchString(battleTag) {
			user := &User{msg.Author.Id, battleTag, msg.Author.Id}
			err := bot.userSource.Save(user)
			if err != nil {
				bot.logger.WithFields(logrus.Fields{
					"error": err,
					"user":  user,
				}).Error("Failed saving user to datasource")
			}
		} else {
			mention = true
			respMsg = fmt.Sprintf(MSG_HELP_INVALID_BATTLE_TAG_FORMAT, battleTag)
		}
	} else {
		mention = true
		respMsg = MSG_HELP_UNKNOWN_COMMAND
	}

	if respMsg != "" {
		if mention {
			respMsg = fmt.Sprintf("<@%s>: %s", msg.Author.Id, respMsg)
		}
		err := bot.discord.CreateMessage(msg.ChannelId, respMsg)

		respLogEntry := bot.logger.WithFields(logrus.Fields{
			"authorId":            msg.Author.Id,
			"authorUsername":      msg.Author.Username,
			"authorDiscriminator": msg.Author.Discriminator,
			"response":            respMsg,
		})
		if err != nil {
			respLogEntry.WithField("error", err).Warn("Failed sending response message")
		} else {
			respLogEntry.Info("Sent response message")
		}
	}
}

func (bot *Bot) Run() {
	bot.logger.WithField("revision", REVISION).Info("Bot starting, connecting...")
	bot.discord.AddReadyHandler(bot.onSessionReady)
	bot.discord.AddMessageHandler(bot.onChannelMessage)

	if err := bot.discord.Connect(); err != nil {
		bot.logger.WithField("error", err).Fatal("Could not connect to Discord")
	}
	bot.logger.Debug("Connected to discord")

	// Run until asked to quit
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, os.Kill)
	<-interruptChan

	bot.logger.Info("Interrupted, disconnecting from discord...")
	if err := bot.discord.Disconnect(); err != nil {
		bot.logger.WithField("error", err).Fatal("Failed to disconnect")
	}
	bot.logger.Info("Disconnected")
}

func NewBot(botId string, token string, logger *logrus.Logger) (*Bot, error) {
	overwatch, err := overwatch.NewOverwatch(logger)
	if err != nil {
		return nil, err
	}

	userAgent := fmt.Sprintf("DiscordBot (%s, %s)", GITHUB_URL, REVISION)
	discord, err := discord.NewDiscord(logger, botId, token, userAgent)
	if err != nil {
		return nil, err
	}

	userSource := NewMemoryUserSource()

	// Store the logger as an Entry, adding the module to all log calls
	botLogger := logger.WithField("module", "main")

	return &Bot{
		logger:     botLogger,
		discord:    discord,
		overwatch:  overwatch,
		userSource: userSource,
	}, nil
}
