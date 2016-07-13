package owbot

import (
	"bytes"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/verath/owbot-bot/owbot/discord"
	"regexp"
	"strings"
	"text/template"
)

type invalidBattleTagData struct {
	MentionId string
	BattleTag string
}

var tmplInvalidBattleTag = template.Must(template.New("InvalidBattleTag").
	Parse(`<@{{ .MentionId }}> "{{ .BattleTag }}" is not a valid BattleTag`))

type unknownDiscordUserData struct {
	MentionId string
}

var tmplUnknownDiscordUser = template.Must(template.New("UnknownDiscordUser").
	Parse(`No BattleTag set for <@{{ .MentionId }}>`))

type fetchErrorData struct {
	BattleTag string
}

var tmplFetchError = template.Must(template.New("FetchError").
	Parse(`Unable to fetch profile for {{ .BattleTag }}`))

var tmplOverwatchProfile = template.Must(template.New("OverwatchProfile").
	Parse(`**{{ .BattleTag }}** (Level: {{ .OverallStats.Level }}, Rank: {{ .OverallStats.CompRank }})`))

// Not using template here as the strings do not update
var msgUsage = fmt.Sprintf(strings.TrimSpace(`
**ow-bot (r%s, %s)**
  - !ow profile <DiscordUser> - Shows overwatch profile summary for <DiscordUser>
  - !ow profile <BattleTag> - Shows Overwatch profile summary for <BattleTag>
  - !ow set <BattleTag> - Sets your BattleTag to <BattleTag>.

<DiscordUser>: A Discord user mention (@username)
<BattleTag>: A Battle.net BattleTag (username#12345)`),
	GitRevision, GitHubUrl)

var msgSetBattleTag = `I don't know your BattleTag. Use "!ow set <BattleTag>" to set it.`
var msgUnknownCommand = `Sorry, but I don't know what you want. Type "!ow help" to show usage help.`

// A BattleTag is 3-12 characters, followed by "#", followed by digits
var regexBattleTag = regexp.MustCompile(`^\w{3,12}#\d+$`)

// A discord mention is either "<@USER_SNOWFLAKE_ID>" or "<@!USER_SNOWFLAKE_ID>"
// https://discordapp.com/developers/docs/resources/channel#message-formatting
var regexDiscordMention = regexp.MustCompile(`^<@!?(\d+)>$`)

func (bot *Bot) sendMessage(channelId string, msg string) {
	err := bot.discord.CreateMessage(channelId, msg)
	respLogEntry := bot.logger.WithFields(logrus.Fields{"channelId": channelId, "message": msg})
	if err != nil {
		respLogEntry.WithError(err).Warn("Failed sending message")
	} else {
		respLogEntry.Debug("Sent message")
	}
}

func (bot *Bot) sendTemplateMessage(channelId string, template *template.Template, data interface{}) {
	var msg bytes.Buffer
	err := template.Execute(&msg, data)
	if err != nil {
		bot.logger.WithFields(logrus.Fields{
			"error":    err,
			"template": template.Name,
		}).Error("Failed executing template")
		return
	}
	bot.sendMessage(channelId, msg.String())
}

func (bot *Bot) onChannelMessage(chanMessage *discord.Message) {
	args := strings.Split(chanMessage.Content, " ")
	if args[0] != "!ow" {
		return
	}

	if len(args) == 1 {
		// Expand "!ow" -> "!ow profile", makes it easier to
		// handle the profile command
		args = append(args, "profile")
	}

	switch args[1] {
	case "set":
		bot.setBattleTag(args[2:], chanMessage)
	case "help":
		bot.showUsage(args[2:], chanMessage)
	case "profile":
		fallthrough
	default:
		bot.showProfile(args[2:], chanMessage)
	}
}

func (bot *Bot) showProfile(args []string, chanMessage *discord.Message) {
	chanId := chanMessage.ChannelId

	var battleTag string
	if len(args) == 1 && regexBattleTag.MatchString(args[0]) {
		// !ow profile <BattleTag>
		battleTag = args[0]
	} else {
		var discordId string
		if len(args) == 0 {
			// !ow profile
			discordId = chanMessage.Author.Id
		} else if len(args) == 1 && regexDiscordMention.MatchString(args[0]) {
			// !ow profile @username
			matches := regexDiscordMention.FindStringSubmatch(args[0])
			discordId = matches[1]
		} else {
			bot.sendMessage(chanId, msgUnknownCommand)
			return
		}

		user, err := bot.userSource.Get(discordId)
		if err != nil {
			bot.logger.WithField("error", err).Error("Failed getting user from source")
			return
		}
		if user == nil {
			data := unknownDiscordUserData{discordId}
			bot.sendTemplateMessage(chanId, tmplUnknownDiscordUser, data)
			return
		}
		battleTag = user.BattleTag
	}

	battleTagFields := bot.logger.WithField("battleTag", battleTag)
	stats, err := bot.overwatch.GetStats(battleTag)
	if err != nil {
		battleTagFields.WithError(err).Warn("Could not get Overwatch stats")
		data := fetchErrorData{battleTag}
		bot.sendTemplateMessage(chanId, tmplFetchError, data)
	} else {
		battleTagFields.Debug("Successfully got Overwatch stats")
		bot.sendTemplateMessage(chanId, tmplOverwatchProfile, stats)
	}

}

func (bot *Bot) setBattleTag(args []string, chanMessage *discord.Message) {
	if len(args) == 0 {
		bot.sendMessage(chanMessage.ChannelId, msgUnknownCommand)
		return
	}

	battleTag := args[0]
	if regexBattleTag.MatchString(battleTag) {
		user := &User{chanMessage.Author.Id, battleTag, chanMessage.Author.Id}
		err := bot.userSource.Save(user)
		if err != nil {
			bot.logger.WithFields(logrus.Fields{
				"error": err,
				"user":  user,
			}).Error("Failed saving user to datasource")
		}
	} else {
		data := invalidBattleTagData{chanMessage.Author.Id, battleTag}
		bot.sendTemplateMessage(chanMessage.ChannelId, tmplInvalidBattleTag, data)
	}
}

func (bot *Bot) showUsage(args []string, chanMessage *discord.Message) {
	bot.sendMessage(chanMessage.ChannelId, msgUsage)
}
