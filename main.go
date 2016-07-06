package main

import (
	"flag"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/verath/owbot-bot/lib/discord"
	"github.com/verath/owbot-bot/lib/overwatch"
	"os"
	"os/signal"
	"regexp"
	"strings"
)

var HELP_MSG_USAGE = strings.TrimSpace(`
**ow-bot usage:**
  - !ow - Shows Overwatch profile summary for your set BattleTag
  - !ow <BattleTag> - Shows Overwatch profile summary for <BattleTag>
  - !ow set <BattleTag> - Sets the BattleTag for your user to <BattleTag>`)
var MSG_HELP_SET_BATTLE_TAG = `I don't know your BattleTag. Use "!ow set <BattleTag>" to set it.`
var MSG_HELP_INVALID_BATTLE_TAG_FORMAT = `"%s" is not a valid BattleTag`
var MSG_HELP_UNKNOWN_COMMAND = `Sorry, but I don't know what you want. Type "!ow help" to show help.`

var MSG_OVERWATCH_PROFILE_FORMAT = `**%s** (Level: %d, Rank: %d)`
var MSG_ERROR_FETCHING_PROFILE_FORMAT = `Unable to fetch profile for %s.`

var BATTLE_TAG_REGEX = regexp.MustCompile(`^(?P<BattleTag>\w{3,12}#\d+)$`)

type Bot struct {
	logger               *logrus.Entry
	overwatch            *overwatch.Overwatch
	session              *discord.Session
	discordIdToBattleTag map[string]string
}

func (b *Bot) onSessionReady(s *discord.Session) {
	b.logger.Info("onSessionReady got Overwatch stats")
	s.UpdateStatus(-1, "!ow help")
}

func (b *Bot) fetchOverwatchProfile(battleTag string) string {
	stats, err := b.overwatch.GetStats(battleTag)
	if err != nil {
		b.logger.WithFields(logrus.Fields{
			"error":     err,
			"battleTag": battleTag,
		}).Warn("Could not get Overwatch stats")
		return fmt.Sprintf(MSG_ERROR_FETCHING_PROFILE_FORMAT, battleTag)
	} else {
		b.logger.WithField("battleTag", battleTag).Info("Successfully got Overwatch stats")
		return fmt.Sprintf(MSG_OVERWATCH_PROFILE_FORMAT,
			battleTag,
			stats.OverallStats.Level,
			stats.OverallStats.CompRank)
	}

}

func (b *Bot) onChannelMessage(s *discord.Session, msg *discord.Message) {
	if !strings.HasPrefix(msg.Content, "!ow") {
		return
	}
	args := strings.Split(msg.Content, " ")

	respMsg := ""
	mention := false

	if len(args) == 1 {
		if battleTag, ok := b.discordIdToBattleTag[msg.Author.Id]; ok {
			respMsg = b.fetchOverwatchProfile(battleTag)
		} else {
			mention = true
			respMsg = MSG_HELP_SET_BATTLE_TAG
		}
	} else if args[1] == "help" {
		respMsg = HELP_MSG_USAGE
	} else if BATTLE_TAG_REGEX.MatchString(args[1]) {
		battleTag := args[1]
		respMsg = b.fetchOverwatchProfile(battleTag)
	} else if args[1] == "set" && len(args) >= 3 {
		battleTag := args[2]
		if BATTLE_TAG_REGEX.MatchString(battleTag) {
			b.discordIdToBattleTag[msg.Author.Id] = battleTag
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
		err := s.CreateMessage(msg.ChannelId, respMsg)

		respLogEntry := b.logger.WithFields(logrus.Fields{
			"author":   msg.Author.Id,
			"response": respMsg,
		})
		if err != nil {
			respLogEntry.WithField("error", err).Warn("Failed sending response message")
		} else {
			respLogEntry.Info("Sent response message")
		}
	}
}

func (b *Bot) Run() error {

	b.session.AddReadyHandler(b.onSessionReady)
	b.session.AddMessageHandler(b.onChannelMessage)

	err := b.session.Connect()
	if err != nil {
		return err
	}

	// Run until asked to quit
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	<-c

	return nil
}

func NewBot(botId string, token string, logger *logrus.Logger) (*Bot, error) {
	overwatch, err := overwatch.NewOverwatch(logger)
	if err != nil {
		return nil, err
	}

	session, err := discord.NewSession(logger, botId, token)

	// Store the logger as an Entry, adding the module to all log calls
	botLogger := logger.WithField("module", "main")

	return &Bot{
		logger:               botLogger,
		session:              session,
		overwatch:            overwatch,
		discordIdToBattleTag: make(map[string]string),
	}, nil
}

func main() {
	var (
		botId string
		token string
		logFile string
	)
	flag.StringVar(&botId, "id", "", "The Bot ID of the bot")
	flag.StringVar(&token, "token", "", "The secret token for the bot")
	flag.StringVar(&logFile, "logfile", "", "A path to a file for writing logs (default is stdout)")
	flag.Parse()

	// TODO: This is not a great solution for required config...
	if botId == "" || token == "" {
		println("Both Bot ID and Bot Token is required")
		os.Exit(-1)
	}

	// Create a logrus instance (logger)
	logger := logrus.New()
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_WRONLY | os.O_CREATE, 0755)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"module":   "main",
				"filename": logFile,
			}).Fatal(err)
		}
		logger.Formatter = &logrus.TextFormatter{DisableColors: true}
		logger.Out = f
	}

	bot, err := NewBot(botId, token, logger)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Fatal(bot.Run())
}
