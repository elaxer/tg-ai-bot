package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"telegram-bot/internal/app/memes"
	"telegram-bot/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	defaultConfigPath     = "bot.config.yaml"
	defaultKnownChatsPath = "data/daily_messages_state.json"
	defaultMemeStatePath  = "data/meme_messages_state.json"
	defaultCheckInterval  = 15 * time.Minute
	defaultRequestTimeout = 20 * time.Second
)

func main() {
	var (
		configPath     string
		knownChatsPath string
		statePath      string
		checkInterval  time.Duration
		requestTimeout time.Duration
		dryRun         bool
		once           bool
	)

	flag.StringVar(&configPath, "config", defaultConfigPath, "path to bot config yaml")
	flag.StringVar(&knownChatsPath, "known-chats", defaultKnownChatsPath, "path to JSON file with known chats")
	flag.StringVar(&statePath, "state", defaultMemeStatePath, "path to meme sender state json")
	flag.DurationVar(&checkInterval, "check-interval", defaultCheckInterval, "how often to scan for due chats")
	flag.DurationVar(&requestTimeout, "request-timeout", defaultRequestTimeout, "timeout per subreddit request")
	flag.BoolVar(&dryRun, "dry-run", false, "log outgoing memes without sending them")
	flag.BoolVar(&once, "once", false, "run a single scan/send cycle and exit")
	flag.Parse()

	if err := config.LoadDotEnv(".env"); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	botCfg, err := config.LoadBot(configPath)
	if err != nil {
		log.Fatalf("load bot config: %v", err)
	}
	runtimeCfg, err := config.LoadRuntimeFromEnv()
	if err != nil {
		log.Fatalf("load runtime env: %v", err)
	}
	if len(botCfg.Memes.Subreddits) == 0 {
		log.Fatalf("bot config must define at least one bot_meme_subreddits entry")
	}
	if checkInterval <= 0 {
		log.Fatalf("check-interval must be > 0")
	}

	tgBot, err := tgbotapi.NewBotAPI(runtimeCfg.TelegramToken)
	if err != nil {
		log.Fatalf("create telegram client: %v", err)
	}

	workerCfg := memes.Config{
		KnownChatsPath: knownChatsPath,
		StatePath:      statePath,
		Subreddits:     append([]string(nil), botCfg.Memes.Subreddits...),
		MinInterval:    botCfg.Memes.IntervalMin,
		MaxInterval:    botCfg.Memes.IntervalMax,
		RequestTimeout: requestTimeout,
		DryRun:         dryRun,
	}
	worker, err := memes.NewWorker(tgBot, workerCfg)
	if err != nil {
		log.Fatalf("init meme worker: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if once {
		due, sent, err := worker.RunCycle(ctx)
		if err != nil {
			log.Fatalf("run cycle failed: %v", err)
		}
		log.Printf("cycle done: scanned=%d sent=%d", due, sent)
		return
	}

	if err := worker.RunLoop(ctx, checkInterval); err != nil && !strings.Contains(err.Error(), "context canceled") {
		fmt.Fprintf(os.Stderr, "worker stopped with error: %v\n", err)
		os.Exit(1)
	}
}
