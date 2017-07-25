package main

import (
	"context"
	"flag"
	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/pkg/errors"
	"github.com/verath/owbot-bot/owbot"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

func main() {
	var (
		debug  bool
		token  string
		dbFile string
	)
	flag.BoolVar(&debug, "debug", false, "Optional. Enables logging of debug messages.")
	flag.StringVar(&token, "token", "", "The secret discord token for the bot.")
	flag.StringVar(&dbFile, "dbfile", "", "Optional. Path to a file to be used for bolt database. ")
	flag.Parse()

	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{}
	if debug {
		logger.Level = logrus.DebugLevel
	}
	if token == "" {
		logger.Fatal("The token argument is required.")
	}
	userSource, err := createUserSource(logger, dbFile)
	if err != nil {
		logger.Fatalf("Could not create user source: %+v", err)
	}
	defer userSource.Close()
	bot, err := owbot.New(logger, token, userSource)
	if err != nil {
		logger.Fatalf("Error creating bot instance: %+v", err)
	}
	ctx := lifetimeContext(logger)
	err = bot.Run(ctx)
	if errors.Cause(err) == context.Canceled {
		logger.Debugf("Error caught in main: %+v", err)
	} else {
		logger.Fatalf("Error caught in main: %+v", err)
	}
}

func createUserSource(logger *logrus.Logger, dbFile string) (owbot.UserSource, error) {
	if dbFile != "" {
		path, err := filepath.Abs(dbFile)
		if err != nil {
			return nil, errors.Wrap(err, "Could not determine absolute dbFile path")
		}
		logger.Infof("Using Bolt db user source: %s", path)
		db, err := bolt.Open(dbFile, 0600, &bolt.Options{Timeout: 5 * time.Second})
		if err != nil {
			return nil, errors.Wrap(err, "Could not open bolt db")
		}
		return owbot.NewBoltUserSource(logger, db)
	} else {
		return owbot.NewMemoryUserSource(), nil
	}
}

// lifetimeContext returns a context that is cancelled on the first SIGINT or
// SIGKILL signal received. The application is force closed if more than
// one signal is received.
func lifetimeContext(logger *logrus.Logger) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	stopSigs := make(chan os.Signal, 2)
	signal.Notify(stopSigs, os.Interrupt, os.Kill)
	go func() {
		<-stopSigs
		logger.Info("Caught interrupt, shutting down")
		cancel()
		<-stopSigs
		logger.Fatal("Caught second interrupt, force closing")
	}()
	return ctx
}
