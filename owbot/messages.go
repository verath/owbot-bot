package owbot

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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
	MentionID string
	BattleTag string
}

var tmplInvalidBattleTag = template.Must(template.New("InvalidBattleTag").
	Parse(`<@{{ .MentionID }}>: "{{ .BattleTag }}" is not a valid BattleTag`))

type cannotOverrideOwnerData struct {
	MentionID string
}

var tmplCannotOverrideOwner = template.Must(template.New("CannotOverrideOwner").
	Parse(`<@{{ .MentionID }}>: Cannot change BattleTag set by the user themselves`))

type unknownDiscordUserData struct {
	MentionID string
}

var tmplUnknownDiscordUser = template.Must(template.New("UnknownDiscordUser").
	Parse(`No BattleTag for <@{{ .MentionID }}>, use "!ow set <@{{ .MentionID }}> <BattleTag>" to set one`))

type fetchErrorData struct {
	BattleTag string
}

var tmplFetchError = template.Must(template.New("FetchError").
	Parse(`Unable to fetch competitive stats for "{{ .BattleTag }}"`))

type battleTagUpdatedData struct {
	MentionID string
	BattleTag string
}

var tmplBattleTagUpdated = template.Must(template.New("BattleTagUpdated").
	Parse(`BattleTag for <@{{ .MentionID }}> is now "{{ .BattleTag }}"`))

var tmplOverwatchProfile = template.Must(template.New("OverwatchProfile").Parse(strings.TrimSpace(`
__**{{ .BattleTag }} (competitive, {{ .Region }})**__
**Level:** {{ .OverallStats.Level }} +{{ .OverallStats.Prestige }}
**Rank:** {{ .OverallStats.CompRank }}
**K/D:** {{ .GameStats.Eliminations -}} / {{- .GameStats.Deaths }}  ({{ .GameStats.KPD }} KPD)
**Win Rate:** {{ printf "%.2f" .OverallStats.WinRate }}%
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
	gitHubURL)

var msgVersion = fmt.Sprintf(strings.TrimSpace(`
Version: %s`),
	gitHubURL+"/commit/"+gitRevision)

var msgUnknownCommand = `Sorry, but I don't know what you want. Type "!ow help" to show usage help.`

// A BattleTag is 3-12 characters, followed by "#", followed by digits
var regexBattleTag = regexp.MustCompile(`^\w{3,12}#\d+$`)

// A discord mention is either "<@USER_SNOWFLAKE_ID>" or "<@!USER_SNOWFLAKE_ID>"
// https://discordapp.com/developers/docs/resources/channel#message-formatting
var regexMention = regexp.MustCompile(`^<@!?(\d+)>$`)

func (bot *Bot) sendMessage(ctx context.Context, channelID string, msg string) error {
	_, err := bot.discordSession.ChannelMessageSend(channelID, msg)
	if err != nil {
		return errors.Wrapf(err, "Failed sending message '%s' to channelID '%s'", msg, channelID)
	}
	bot.logger.WithFields(logrus.Fields{"channelID": channelID, "message": msg}).Debug("Sent message")
	return nil
}

func (bot *Bot) sendTemplateMessage(ctx context.Context, channelID string, template *template.Template, data interface{}) error {
	var msg bytes.Buffer
	err := template.Execute(&msg, data)
	if err != nil {
		return errors.Wrapf(err, "Failed executing template: %s", template.Name)
	}
	return bot.sendMessage(ctx, channelID, msg.String())
}

func (bot *Bot) handleDiscordMessage(chanMessage *discordgo.Message) error {
	args := strings.Fields(chanMessage.Content)
	if len(args) == 0 || args[0] != "!ow" {
		return nil
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
		return bot.setBattleTag(ctx, args[2:], chanMessage)
	case "help":
		return bot.showUsage(ctx, args[2:], chanMessage)
	case "profile":
		return bot.showProfile(ctx, args[2:], chanMessage)
	case "version":
		return bot.showVersion(ctx, args[2:], chanMessage)
	default:
		return bot.showProfile(ctx, args[1:], chanMessage)
	}
}

func (bot *Bot) showProfile(ctx context.Context, args []string, chanMessage *discordgo.Message) error {
	channelID := chanMessage.ChannelID

	var battleTag string
	if len(args) == 1 && regexBattleTag.MatchString(args[0]) {
		// !ow profile <BattleTag>
		battleTag = args[0]
	} else {
		var discordID string
		if len(args) == 0 {
			// !ow profile
			discordID = chanMessage.Author.ID
		} else if len(args) == 1 && regexMention.MatchString(args[0]) {
			// !ow profile @username
			matches := regexMention.FindStringSubmatch(args[0])
			discordID = matches[1]
		} else {
			return bot.sendMessage(ctx, channelID, msgUnknownCommand)
		}

		user, err := bot.userSource.Get(discordID)
		if err != nil {
			return errors.Wrapf(err, "Could not get user '%s' from data source", discordID)
		}
		if user == nil {
			data := unknownDiscordUserData{MentionID: discordID}
			return bot.sendTemplateMessage(ctx, channelID, tmplUnknownDiscordUser, data)
		}
		battleTag = user.BattleTag
	}

	battleTagFields := bot.logger.WithField("battleTag", battleTag)
	stats, err := bot.owAPIClient.GetStats(ctx, battleTag)
	if err != nil {
		battleTagFields.WithError(err).Warn("Could not get Overwatch stats")
		data := fetchErrorData{BattleTag: battleTag}
		return bot.sendTemplateMessage(ctx, channelID, tmplFetchError, data)
	} else {
		battleTagFields.Debug("Successfully got Overwatch stats")
		return bot.sendTemplateMessage(ctx, channelID, tmplOverwatchProfile, stats)
	}
}

func (bot *Bot) setBattleTag(ctx context.Context, args []string, chanMessage *discordgo.Message) error {
	if len(args) == 0 {
		return bot.sendMessage(ctx, chanMessage.ChannelID, msgUnknownCommand)
	}

	var userID string
	if len(args) >= 2 {
		// !ow <@user> tag#123
		userMention := args[0]
		args = args[1:]
		if matches := regexMention.FindStringSubmatch(userMention); matches != nil {
			// To validate the userID, we make sure the id we extracted is
			// also included in the Mentioned users of the discord message
			for _, user := range chanMessage.Mentions {
				if user.ID == matches[1] {
					userID = matches[1]
					break
				}
			}
		}
		if userID == "" {
			return bot.sendMessage(ctx, chanMessage.ChannelID, msgUnknownCommand)
		}
	} else {
		userID = chanMessage.Author.ID
	}

	// If we get here, we should only have to handle !ow battleTag#123
	// as the optional user mention is handled above
	if len(args) > 1 {
		return bot.sendMessage(ctx, chanMessage.ChannelID, msgUnknownCommand)
	}

	// Make sure the argument is a "valid" battleTag
	battleTag := args[0]
	if !regexBattleTag.MatchString(battleTag) {
		data := invalidBattleTagData{MentionID: chanMessage.Author.ID, BattleTag: battleTag}
		return bot.sendTemplateMessage(ctx, chanMessage.ChannelID, tmplInvalidBattleTag, data)
	}

	// Only allowed to update a user object if the author of the message
	// is the owner, or if the user has not been set by the owner yet
	currUser, err := bot.userSource.Get(userID)
	if err != nil {
		return errors.Wrapf(err, "Could not get userID '%s' from user source", userID)
	}
	if currUser != nil && currUser.ID != chanMessage.Author.ID && currUser.CreatedBy == currUser.ID {
		bot.logger.WithFields(logrus.Fields{
			"currUser": currUser,
			"authorID": chanMessage.Author.ID,
		}).Debug("Not allowed to change data set by owner")
		data := cannotOverrideOwnerData{MentionID: chanMessage.Author.ID}
		return bot.sendTemplateMessage(ctx, chanMessage.ChannelID, tmplCannotOverrideOwner, data)
	}
	// Update the user object and store it
	user := &User{ID: userID, BattleTag: battleTag, CreatedBy: chanMessage.Author.ID}
	if err := bot.userSource.Save(user); err != nil {
		return errors.Wrapf(err, "Failed saving user (%+v) to data source", user)
	}
	data := battleTagUpdatedData{MentionID: userID, BattleTag: battleTag}
	return bot.sendTemplateMessage(ctx, chanMessage.ChannelID, tmplBattleTagUpdated, data)
}

func (bot *Bot) showVersion(ctx context.Context, args []string, chanMessage *discordgo.Message) error {
	return bot.sendMessage(ctx, chanMessage.ChannelID, msgVersion)
}

func (bot *Bot) showUsage(ctx context.Context, args []string, chanMessage *discordgo.Message) error {
	return bot.sendMessage(ctx, chanMessage.ChannelID, msgUsage)
}
