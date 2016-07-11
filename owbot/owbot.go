package owbot

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/verath/owbot-bot/owbot/discord"
	"github.com/verath/owbot-bot/owbot/overwatch"
	"regexp"
	"strings"
)

var (
	// The url to the project github page
	GithubUrl = "https://github.com/verath/owbot-bot"

	// The git revision currently running. This value is set during the build
	// using '-ldflags "-X github.com/verath/owbot-bot/owbot.GitRevision=..."'
	GitRevision = ""
)

// TODO: Change to using templates
var HELP_MSG_USAGE = strings.TrimSpace(fmt.Sprintf(`
**ow-bot (version %s, %s)**
  - !ow - Shows Overwatch profile summary for your set BattleTag
  - !ow <BattleTag> - Shows Overwatch profile summary for <BattleTag>
  - !ow set <BattleTag> - Sets the BattleTag for your user to <BattleTag>`,
	GitRevision, GithubUrl))
var MSG_HELP_SET_BATTLE_TAG = `I don't know your BattleTag. Use "!ow set <BattleTag>" to set it.`
var MSG_HELP_INVALID_BATTLE_TAG_FORMAT = `"%s" is not a valid BattleTag`
var MSG_HELP_UNKNOWN_COMMAND = `Sorry, but I don't know what you want. Type "!ow help" to show help.`

var MSG_OVERWATCH_PROFILE_FORMAT = `**%s** (Level: %d, Rank: %d)`
var MSG_ERROR_FETCHING_PROFILE_FORMAT = `Unable to fetch profile for %s.`

var BATTLE_TAG_REGEX = regexp.MustCompile(`^(?P<BattleTag>\w{3,12}#\d+)$`)

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
	overwatch, err := overwatch.NewOverwatch(logger)
	if err != nil {
		return nil, err
	}

	userAgent := fmt.Sprintf("DiscordBot (%s, %s)", GithubUrl, GitRevision)
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
		botLogger.Info("Using Bolt db user source")
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
