package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"telegram-bot/internal/config"
	"telegram-bot/internal/infra/openai"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	defaultConfigPath     = "bot.config.yaml"
	defaultStatePath      = "data/daily_messages_state.json"
	defaultCheckInterval  = 15 * time.Hour
	defaultRequestTimeout = 30 * time.Second
)

type chatDailyState struct {
	Title        string `json:"title"`
	LastSentUnix int64  `json:"last_sent_unix"`
}

type dailyStateFile struct {
	Chats map[string]chatDailyState `json:"chats"`
}

func main() {
	var (
		configPath    string
		statePath     string
		minInterval   time.Duration
		checkInterval time.Duration
		dryRun        bool
		once          bool
	)

	flag.StringVar(&configPath, "config", defaultConfigPath, "path to bot config yaml")
	flag.StringVar(&statePath, "state", defaultStatePath, "path to daily state json")
	flag.DurationVar(&minInterval, "min-interval", 0, "minimum time between daily messages per chat (overrides bot_daily_message_interval)")
	flag.DurationVar(&checkInterval, "check-interval", defaultCheckInterval, "how often to scan state for due chats")
	flag.BoolVar(&dryRun, "dry-run", false, "print due chats without sending messages")
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
	if minInterval <= 0 {
		minInterval = botCfg.DailyMessageInterval
	}

	tgBot, err := tgbotapi.NewBotAPI(runtimeCfg.TelegramToken)
	if err != nil {
		log.Fatalf("create telegram client: %v", err)
	}

	oa := openai.NewClient(
		runtimeCfg.OpenAIAPIKey,
		botCfg.OpenAI.Model,
		botCfg.OpenAI.TTSModel,
		botCfg.OpenAI.TTSVoice,
		botCfg.OpenAI.TTSInstructions,
		botCfg.OpenAI.SystemPrompt,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if once {
		scanned, sent, err := runCycle(ctx, statePath, minInterval, dryRun, tgBot, oa)
		if err != nil {
			log.Fatalf("run cycle failed: %v", err)
		}
		log.Printf("cycle done: due=%d sent=%d", scanned, sent)
		return
	}

	if checkInterval <= 0 {
		log.Fatalf("check-interval must be > 0")
	}
	log.Printf("daily worker started: state=%s min-interval=%s check-interval=%s", statePath, minInterval, checkInterval)

	tickCh := make(chan time.Time, 1)
	sendCh := make(chan int64, 256)
	doneCh := make(chan struct{})

	go startTicker(ctx, checkInterval, tickCh)
	go sendWorker(ctx, sendCh, doneCh, statePath, dryRun, tgBot, oa)

	tickCh <- time.Now()
	for {
		select {
		case <-ctx.Done():
			close(sendCh)
			<-doneCh
			log.Printf("daily worker stopped")
			return
		case <-tickCh:
			due, err := findDueChats(statePath, minInterval)
			if err != nil {
				log.Printf("scan failed: %v", err)
				continue
			}
			if len(due) == 0 {
				log.Printf("no due chats")
				continue
			}
			log.Printf("due chats: %d", len(due))
			for _, chatID := range due {
				select {
				case sendCh <- chatID:
				case <-ctx.Done():
					close(sendCh)
					<-doneCh
					log.Printf("daily worker stopped")
					return
				}
			}
		}
	}
}

func runCycle(ctx context.Context, statePath string, minInterval time.Duration, dryRun bool, tgBot *tgbotapi.BotAPI, oa *openai.Client) (int, int, error) {
	due, err := findDueChats(statePath, minInterval)
	if err != nil {
		return 0, 0, err
	}
	if len(due) == 0 {
		log.Printf("no due chats")
		return 0, 0, nil
	}
	log.Printf("due chats: %d", len(due))
	sentCount := 0
	for _, chatID := range due {
		if err := sendToChat(ctx, statePath, chatID, dryRun, tgBot, oa); err != nil {
			log.Printf("send failed chat_id=%d err=%v", chatID, err)
			continue
		}
		sentCount++
	}
	return len(due), sentCount, nil
}

func startTicker(ctx context.Context, interval time.Duration, tickCh chan<- time.Time) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			select {
			case tickCh <- t:
			default:
			}
		}
	}
}

func sendWorker(ctx context.Context, sendCh <-chan int64, doneCh chan<- struct{}, statePath string, dryRun bool, tgBot *tgbotapi.BotAPI, oa *openai.Client) {
	defer close(doneCh)
	for {
		select {
		case <-ctx.Done():
			return
		case chatID, ok := <-sendCh:
			if !ok {
				return
			}
			if err := sendToChat(ctx, statePath, chatID, dryRun, tgBot, oa); err != nil {
				log.Printf("send failed chat_id=%d err=%v", chatID, err)
			}
		}
	}
}

func findDueChats(statePath string, minInterval time.Duration) ([]int64, error) {
	state, err := loadState(statePath)
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	if len(state.Chats) == 0 {
		return nil, nil
	}
	return dueChats(state, time.Now(), minInterval), nil
}

func sendToChat(parentCtx context.Context, statePath string, chatID int64, dryRun bool, tgBot *tgbotapi.BotAPI, oa *openai.Client) error {
	state, err := loadState(statePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	key := strconv.FormatInt(chatID, 10)
	chat, ok := state.Chats[key]
	if !ok {
		return nil
	}

	log.Printf("due chat_id=%d title=%q last_sent=%d", chatID, chat.Title, chat.LastSentUnix)
	if dryRun {
		return nil
	}

	ctx, cancel := context.WithTimeout(parentCtx, defaultRequestTimeout)
	replyText, err := oa.GeneratePromptReply(ctx, buildDailyPrompt(chat.Title))
	cancel()
	if err != nil {
		return fmt.Errorf("openai: %w", err)
	}
	if strings.TrimSpace(replyText) == "" {
		return nil
	}

	msg := tgbotapi.NewMessage(chatID, replyText)
	sent, err := tgBot.Send(msg)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}

	chat.LastSentUnix = time.Now().Unix()
	if strings.TrimSpace(chat.Title) == "" {
		chat.Title = strings.TrimSpace(chatName(sent.Chat))
	}
	state.Chats[key] = chat
	if err := saveState(statePath, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	log.Printf("sent daily message chat_id=%d msg_id=%d", sent.Chat.ID, sent.MessageID)
	return nil
}

func dueChats(state dailyStateFile, now time.Time, minInterval time.Duration) []int64 {
	due := make([]int64, 0, len(state.Chats))
	for rawID, chat := range state.Chats {
		chatID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			continue
		}
		last := time.Unix(chat.LastSentUnix, 0)
		if now.Sub(last) >= minInterval {
			due = append(due, chatID)
		}
	}
	return due
}

func buildDailyPrompt(groupName string) string {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		groupName = "unknown"
	}
	return fmt.Sprintf(
		"Write one short, fresh, standalone Telegram group message for group %q. "+
			"Be casual, friendly, natural, and varied day-to-day. "+
			"Do not mention being an AI, a bot, prompts, schedules, or that this message is automatic.",
		groupName,
	)
}

func loadState(path string) (dailyStateFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return dailyStateFile{Chats: map[string]chatDailyState{}}, nil
		}
		return dailyStateFile{}, err
	}

	var state dailyStateFile
	if err := json.Unmarshal(b, &state); err != nil {
		return dailyStateFile{}, err
	}
	if state.Chats == nil {
		state.Chats = map[string]chatDailyState{}
	}
	return state, nil
}

func saveState(path string, state dailyStateFile) error {
	if state.Chats == nil {
		state.Chats = map[string]chatDailyState{}
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func chatName(chat *tgbotapi.Chat) string {
	if chat == nil {
		return ""
	}
	if strings.TrimSpace(chat.Title) != "" {
		return chat.Title
	}
	if strings.TrimSpace(chat.UserName) != "" {
		return "@" + chat.UserName
	}
	return ""
}
