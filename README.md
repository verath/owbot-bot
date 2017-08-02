# OW-Bot 

[![CircleCI](https://circleci.com/gh/verath/owbot-bot.svg?style=svg)](https://circleci.com/gh/verath/owbot-bot)
[![Code Climate](https://codeclimate.com/github/verath/owbot-bot/badges/gpa.svg)](https://codeclimate.com/github/verath/owbot-bot)

A simple Discord bot, showing Overwatch profile summaries.

Written in go. Discord client uses [bwmarrin/discordgo](https://github.com/bwmarrin/discordgo).
Overwatch profile data via [SunDwarf/OWAPI](https://github.com/SunDwarf/OWAPI).

## Running the bot
Install the bot via standard go commands:

```
go get -u github.com/verath/owbot-bot
```

Then run it, supplying it the Discord app bot user token (obtained by
registering a new [Discord application](https://discordapp.com/developers/applications/me)):

```
owbot-bot -token "BOT_TOKEN"
```

### Running as a Docker container
It is also possible to run the bot as a Docker container:

```
docker build . -t vearth/owbot-bot
docker run -d vearth/owbot-bot -token "BOT_TOKEN"
```

## Persisting BattleTag mappings
By default the bot stores the BattleTag for users in memory, which means
that the mappings will be gone when the bot quits. To instead persist
this data to disc, specify `-dbfile` when running the bot:

```
owbot-bot -dbfile ./owbot-bot.boltdb -token "BOT_TOKEN"
```

`-dbfile` is set to `/db/owbot.boltdb` by default when running
the bot as a Docker container. To persist data between runs, map
the /db volume to a volume on the host:

```
docker run -d -v /tmp/owbot-db:/db vearth/owbot-bot -token "BOT_TOKEN"
```

## Adding the bot to a channel
The bot can be added to a channel by using the Discord OAuth flow
with the `READ_MESSAGES` and `SEND_MESSAGES` permissions:

```
https://discordapp.com/oauth2/authorize?scope=bot&permissions=3072&client_id=CLIENT_ID
```

Replace `CLIENT_ID` with the Discord application Client ID.
