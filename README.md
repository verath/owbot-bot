# OW-Bot 

[![Build Status](https://travis-ci.org/verath/owbot-bot.svg?branch=master)](https://travis-ci.org/verath/owbot-bot)
[![Code Climate](https://codeclimate.com/github/verath/owbot-bot/badges/gpa.svg)](https://codeclimate.com/github/verath/owbot-bot)

A simple Discord bot, showing Overwatch profile summaries.

Written in go. Discord client uses [bwmarrin/discordgo](https://github.com/bwmarrin/discordgo).
Overwatch profile data via [SunDwarf/OWAPI](https://github.com/SunDwarf/OWAPI).

## Running the bot
First install:

```
go get github.com/verath/owbot-bot
go install github.com/verath/owbot-bot
```

Then run it, supplying a Discord bot token:

```
owbot-bot -token "BOT_TOKEN"
```

## Adding the bot to a channel
The bot can be added to a channel by using the Discord OAuth flow
with the `READ_MESSAGES` and `SEND_MESSAGES` permissions:

https://discordapp.com/oauth2/authorize?scope=bot&permissions=3072&client_id=CLIENT_ID

Note that CLIENT_ID is the Discord Client/Application ID, and not the Bot ID.
