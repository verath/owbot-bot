package owbot

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/verath/owbot-bot/owbot/discord"
	"regexp"
	"strings"
	"text/template"
	"time"
)

const (
	// Longest amount of time a command is processed until given up on
	commandTimeout = 15 * time.Second
)

type invalidBattleTagData struct {
	MentionId string
	BattleTag string
}

var tmplInvalidBattleTag = template.Must(template.New("InvalidBattleTag").
	Parse(`<@{{ .MentionId }}>: "{{ .BattleTag }}" is not a valid BattleTag`))

type unknownDiscordUserData struct {
	MentionId string
}

var tmplUnknownDiscordUser = template.Must(template.New("UnknownDiscordUser").
	Parse(`No BattleTag for <@{{ .MentionId }}>, use "!ow set <@{{ .MentionId }}> <BattleTag>" to set one`))

type fetchErrorData struct {
	BattleTag string
}

var tmplFetchError = template.Must(template.New("FetchError").
	Parse(`Unable to fetch competitive stats for "{{ .BattleTag }}"`))

type battleTagUpdatedData struct {
	MentionId string
	BattleTag string
}

var tmplBattleTagUpdated = template.Must(template.New("BattleTagUpdated").
	Parse(`BattleTag for <@{{ .MentionId }}> is now "{{ .BattleTag }}"`))

var tmplOverwatchProfile = template.Must(template.New("OverwatchProfile").Parse(strings.TrimSpace(`
__**{{ .BattleTag }} (competitive, {{ .Region }})**__
**Level:** {{ .OverallStats.Level }} +{{ .OverallStats.Prestige }}
**Rank:** {{ .OverallStats.CompRank }}
**K/D:** {{ .GameStats.Eliminations -}} / {{- .GameStats.Deaths }}  ({{ .GameStats.KPD }} KPD)
**Matches W/L:** {{ .OverallStats.Wins -}} / {{- .OverallStats.Losses }} ({{ .OverallStats.Games }} total)
**Medals G/S/B:** {{ .GameStats.MedalsGold -}} / {{- .GameStats.MedalsSilver -}} / {{- .GameStats.MedalsBronze }} ({{ .GameStats.Medals }} total)
**Time Played:** {{ .GameStats.TimePlayed }} hours
`)))

// Not using template here as the strings do not update
var msgUsage = fmt.Sprintf(strings.TrimSpace(`
__**ow-bot (%s)**__
- **!ow profile <DiscordUser>** - Shows Overwatch profile summary
- **!ow profile <BattleTag>** - Shows Overwatch profile summary
- **!ow set <BattleTag>** - Sets your BattleTag
- **!ow set <DiscordUser> <BattleTag>** - Sets the BattleTag of a user
- **!ow help** - Shows this message

**<DiscordUser>**: A Discord user mention (@username)
**<BattleTag>**: A Battle.net BattleTag (username#12345)`),
	GitHubUrl)

var msgUnknownCommand = `Sorry, but I don't know what you want. Type "!ow help" to show usage help.`

// A BattleTag is 3-12 characters, followed by "#", followed by digits
var regexBattleTag = regexp.MustCompile(`^\w{3,12}#\d+$`)

// A discord mention is either "<@USER_SNOWFLAKE_ID>" or "<@!USER_SNOWFLAKE_ID>"
// https://discordapp.com/developers/docs/resources/channel#message-formatting
var regexMention = regexp.MustCompile(`^<@!?(\d+)>$`)

func (bot *Bot) sendMessage(ctx context.Context, channelId string, msg string) {
	err := bot.discord.CreateMessage(ctx, channelId, msg)
	respLogEntry := bot.logger.WithFields(logrus.Fields{"channelId": channelId, "message": msg})
	if err != nil {
		respLogEntry.WithError(err).Warn("Failed sending message")
	} else {
		respLogEntry.Debug("Sent message")
	}
}

func (bot *Bot) sendTemplateMessage(ctx context.Context, channelId string, template *template.Template, data interface{}) {
	var msg bytes.Buffer
	err := template.Execute(&msg, data)
	if err != nil {
		bot.logger.WithFields(logrus.Fields{
			"error":    err,
			"template": template.Name,
		}).Error("Failed executing template")
		return
	}
	bot.sendMessage(ctx, channelId, msg.String())
}

func (bot *Bot) onChannelMessage(chanMessage *discord.Message) {
	args := strings.Split(chanMessage.Content, " ")
	if args[0] != "!ow" {
		return
	}

	// Set up a context for this request
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	if len(args) == 1 {
		// Expand "!ow" -> "!ow profile", makes it easier to
		// handle the profile command
		args = append(args, "profile")
	}

	switch args[1] {
	case "set":
		bot.setBattleTag(ctx, args[2:], chanMessage)
	case "help":
		bot.showUsage(ctx, args[2:], chanMessage)
	case "profile":
		bot.showProfile(ctx, args[2:], chanMessage)
	default:
		bot.showProfile(ctx, args[1:], chanMessage)
	}
}

func (bot *Bot) showProfile(ctx context.Context, args []string, chanMessage *discord.Message) {
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
		} else if len(args) == 1 && regexMention.MatchString(args[0]) {
			// !ow profile @username
			matches := regexMention.FindStringSubmatch(args[0])
			discordId = matches[1]
		} else {
			bot.sendMessage(ctx, chanId, msgUnknownCommand)
			return
		}

		user, err := bot.userSource.Get(discordId)
		if err != nil {
			bot.logger.WithField("error", err).Error("Failed getting user from source")
			return
		}
		if user == nil {
			data := unknownDiscordUserData{MentionId: discordId}
			bot.sendTemplateMessage(ctx, chanId, tmplUnknownDiscordUser, data)
			return
		}
		battleTag = user.BattleTag
	}

	battleTagFields := bot.logger.WithField("battleTag", battleTag)
	stats, err := bot.overwatch.GetStats(ctx, battleTag)
	if err != nil {
		battleTagFields.WithError(err).Warn("Could not get Overwatch stats")
		data := fetchErrorData{BattleTag: battleTag}
		bot.sendTemplateMessage(ctx, chanId, tmplFetchError, data)
	} else {
		battleTagFields.Debug("Successfully got Overwatch stats")
		bot.sendTemplateMessage(ctx, chanId, tmplOverwatchProfile, stats)
	}

}

func (bot *Bot) setBattleTag(ctx context.Context, args []string, chanMessage *discord.Message) {
	if len(args) == 0 {
		bot.sendMessage(ctx, chanMessage.ChannelId, msgUnknownCommand)
		return
	}

	var userId string
	if len(args) >= 2 {
		// !ow <@user> tag#123
		userMention := args[0]
		args = args[1:]
		if matches := regexMention.FindStringSubmatch(userMention); matches != nil {
			// To validate the userId, we make sure the id we extracted is
			// also included in the Mentioned users of the discord message
			for _, user := range chanMessage.Mentions {
				if user.Id == matches[1] {
					userId = matches[1]
					break
				}
			}
		}
		if userId == "" {
			bot.sendMessage(ctx, chanMessage.ChannelId, msgUnknownCommand)
			return
		}
	} else {
		userId = chanMessage.Author.Id
	}

	// If we get here, we should only have to handle !ow battleTag#123
	// as the optional user mention is handled above
	if len(args) > 1 {
		bot.sendMessage(ctx, chanMessage.ChannelId, msgUnknownCommand)
		return
	}

	// Make sure the argument is a "valid" battleTag
	battleTag := args[0]
	if !regexBattleTag.MatchString(battleTag) {
		data := invalidBattleTagData{MentionId: chanMessage.Author.Id, BattleTag: battleTag}
		bot.sendTemplateMessage(ctx, chanMessage.ChannelId, tmplInvalidBattleTag, data)
		return
	}

	// Only allowed to update a user object if the author of the message
	// is the owner, or if the user has not been set by the owner yet
	currUser, err := bot.userSource.Get(userId)
	if err != nil {
		bot.logger.WithError(err).WithField("userId", userId).Error("Failed getting user to datasource")
		return
	}
	if currUser != nil && currUser.Id != chanMessage.Author.Id && currUser.CreatedBy == currUser.Id {
		bot.logger.WithFields(logrus.Fields{
			"currUser": currUser,
			"authorId": chanMessage.Author.Id,
		}).Debug("Not allowed to change data set by owner")
		return
	}

	// Update the user object and store it
	user := &User{Id: userId, BattleTag: battleTag, CreatedBy: chanMessage.Author.Id}
	if err := bot.userSource.Save(user); err != nil {
		bot.logger.WithError(err).WithField("user", user).Error("Failed saving user to datasource")
		return
	}

	data := battleTagUpdatedData{MentionId: userId, BattleTag: battleTag}
	bot.sendTemplateMessage(ctx, chanMessage.ChannelId, tmplBattleTagUpdated, data)
}

func (bot *Bot) showUsage(ctx context.Context, args []string, chanMessage *discord.Message) {
	bot.sendMessage(ctx, chanMessage.ChannelId, msgUsage)
}
