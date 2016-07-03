package main

import (
	"flag"
	"fmt"
	"github.com/verath/owbot-bot/lib/discord"
	"github.com/verath/owbot-bot/lib/overwatch"
	"log"
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
	botId                string
	token                string
	discordIdToBattleTag map[string]string
	overwatch            overwatch.Overwatch
	session              discord.Session
}

func (b *Bot) onSessionReady(s *discord.Session) {
	s.UpdateStatus(-1, "!ow help")
}

func (b *Bot) fetchOverwatchProfile(battleTag string) string {
	stats, err := b.overwatch.GetStats(battleTag)
	if err != nil {
		log.Println(err)
		return fmt.Sprintf(MSG_ERROR_FETCHING_PROFILE_FORMAT, battleTag)
	} else {
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
		if err != nil {
			log.Println(err)
		}
	}
}

func (b *Bot) Run() {
	b.overwatch = overwatch.NewOverwatch()
	b.session = discord.NewSession(b.botId, b.token)

	b.session.AddReadyHandler(b.onSessionReady)
	b.session.AddMessageHandler(b.onChannelMessage)

	err := b.session.Connect()
	if err != nil {
		log.Fatal(err)
	}

	// Run until asked to quit
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	<-c
}

func NewBot(botId string, token string) *Bot {
	return &Bot{
		botId:                botId,
		token:                token,
		discordIdToBattleTag: make(map[string]string),
	}
}

func main() {
	var (
		botId string
		token string
	)
	flag.StringVar(&botId, "id", "", "The Bot ID of the bot")
	flag.StringVar(&token, "token", "", "The secret token for the bot")

	flag.Parse()

	// TODO: This is not a great solution for required config...
	if botId == "" || token == "" {
		println("Both Bot ID and Bot Token is required")
		os.Exit(-1)
	}

	bot := NewBot(botId, token)
	bot.Run()
}
