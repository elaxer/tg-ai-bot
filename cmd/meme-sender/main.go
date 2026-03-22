package main

import (
	"context"
	"errors"
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

var (
	errMissingMemeSubreddits = errors.New("bot config must define at least one bot_meme_subreddits entry")
	errInvalidCheckInterval  = errors.New("check-interval must be > 0")
)

type cliConfig struct {
	configPath     string
	knownChatsPath string
	statePath      string
	checkInterval  time.Duration
	requestTimeout time.Duration
	dryRun         bool
	once           bool
}

func main() {
	cfg := parseFlags()

	worker, err := buildWorker(cfg)
	if err != nil {
		log.Fatalf("init meme worker: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	if cfg.once {
		if err := runOnce(ctx, worker); err != nil {
			log.Printf("run cycle failed: %v", err)
			stop()

			return
		}
		stop()

		return
	}

	if err := worker.RunLoop(ctx, cfg.checkInterval); err != nil && !strings.Contains(err.Error(), "context canceled") {
		fmt.Fprintf(os.Stderr, "worker stopped with error: %v\n", err)
	}
	stop()
}

func buildWorker(cfg cliConfig) (*memes.Worker, error) {
	if err := config.LoadDotEnv(".env"); err != nil {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	botCfg, err := config.LoadBot(cfg.configPath)
	if err != nil {
		return nil, fmt.Errorf("load bot config: %w", err)
	}
	runtimeCfg, err := config.LoadRuntimeFromEnv()
	if err != nil {
		return nil, fmt.Errorf("load runtime env: %w", err)
	}
	if len(botCfg.Memes.Subreddits) == 0 {
		return nil, errMissingMemeSubreddits
	}
	if cfg.checkInterval <= 0 {
		return nil, errInvalidCheckInterval
	}

	tgBot, err := tgbotapi.NewBotAPI(runtimeCfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("create telegram client: %w", err)
	}

	workerCfg := memes.Config{
		KnownChatsPath: cfg.knownChatsPath,
		StatePath:      cfg.statePath,
		Subreddits:     append([]string(nil), botCfg.Memes.Subreddits...),
		MinInterval:    botCfg.Memes.IntervalMin,
		MaxInterval:    botCfg.Memes.IntervalMax,
		RequestTimeout: cfg.requestTimeout,
		DryRun:         cfg.dryRun,
	}

	return memes.NewWorker(tgBot, workerCfg)
}

func parseFlags() cliConfig {
	cfg := cliConfig{}
	flag.StringVar(&cfg.configPath, "config", defaultConfigPath, "path to bot config yaml")
	flag.StringVar(&cfg.knownChatsPath, "known-chats", defaultKnownChatsPath, "path to JSON file with known chats")
	flag.StringVar(&cfg.statePath, "state", defaultMemeStatePath, "path to meme sender state json")
	flag.DurationVar(&cfg.checkInterval, "check-interval", defaultCheckInterval, "how often to scan for due chats")
	flag.DurationVar(&cfg.requestTimeout, "request-timeout", defaultRequestTimeout, "timeout per subreddit request")
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "log outgoing memes without sending them")
	flag.BoolVar(&cfg.once, "once", false, "run a single scan/send cycle and exit")
	flag.Parse()

	return cfg
}

func runOnce(ctx context.Context, worker *memes.Worker) error {
	due, sent, err := worker.RunCycle(ctx)
	if err != nil {
		return err
	}
	log.Printf("cycle done: scanned=%d sent=%d", due, sent)

	return nil
}
