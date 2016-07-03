#OW-Bot
A simple Discord bot, showing Overwatch profile summaries.

Written in go. Discord client heavily inspired by [bwmarrin/discordgo](https://github.com/bwmarrin/discordgo).
Overwatch profile data via [SunDwarf/OWAPI](https://github.com/SunDwarf/OWAPI).

## Running the bot
First install:

```
go get github.com/verath/owbot-bot
go install github.com/verath/owbot-bot
```

Then run it, supplying a Discord Bot ID and a Bot Token:

```
owbot-bot -id "BOT_ID" -token "BOT_TOKEN"
```

## Adding the bot to a channel
The bot can be added to a channel by using the Discord OAuth flow:

[https://discordapp.com/oauth2/authorize?scope=bot&permissions=1051648&client_id=CLIENT_ID](https://discordapp.com/oauth2/authorize?scope=bot&permissions=1051648&client_id=<CLIENT_ID>)

Note that CLIENT_ID is the Discord Client/Application ID, and not the Bot ID.