package main

import (
	"flag"
	"github.com/Sirupsen/logrus"
	"os"
	"github.com/verath/owbot-bot/owbot"
)

func main() {
	var (
		botId   string
		token   string
		logFile string
		debug   bool
	)
	flag.StringVar(&botId, "id", "", "The Bot ID of the bot")
	flag.StringVar(&token, "token", "", "The secret token for the bot")
	flag.StringVar(&logFile, "logfile", "", "A path to a file for writing logs (default is stdout)")
	flag.BoolVar(&debug, "debug", false, "Set to true to log debug messages")
	flag.Parse()

	// TODO: This is not a great solution for required config...
	if botId == "" || token == "" {
		println("Both Bot ID and Bot Token is required")
		os.Exit(-1)
	}

	// Create a logrus instance (logger)
	logger := logrus.New()
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"module":   "main",
				"filename": logFile,
			}).Fatal(err)
		}
		logger.Formatter = &logrus.TextFormatter{ForceColors: true}
		logger.Out = f
	}

	// If debug is true, log debug messages
	if debug {
		logger.Level = logrus.DebugLevel
	}

	bot, err := owbot.NewBot(botId, token, logger)
	if err != nil {
		logger.WithField("error", err).Fatal("Error when creating bot")
	}
	bot.Run()
}
