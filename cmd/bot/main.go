package main

import (
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/elaxer/tg-ai-bot/internal/app/chatbot"
	"github.com/elaxer/tg-ai-bot/internal/config"
	"github.com/elaxer/tg-ai-bot/internal/infra/openai"
	"github.com/elaxer/tg-ai-bot/internal/logging"
	"github.com/elaxer/tg-ai-bot/internal/storage/history"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const defaultBotConfigPath = "bot.config.yaml"

func main() {
	log.SetFlags(log.LstdFlags)

	if err := config.LoadDotEnv(".env"); err != nil {
		log.Fatalf("[ERROR] failed to load .env: %v", err)
	}

	configPath := strings.TrimSpace(os.Getenv("BOT_CONFIG_PATH"))
	if configPath == "" {
		configPath = defaultBotConfigPath
	}

	botCfg, err := config.LoadBot(configPath)
	if err != nil {
		log.Fatalf("[ERROR] failed to load config %q: %v", configPath, err)
	}
	if err := logging.Init(logging.Config{
		FilePath:   botCfg.Log.FilePath,
		Level:      botCfg.Log.Level,
		MaxSizeMB:  botCfg.Log.MaxSizeMB,
		MaxBackups: botCfg.Log.MaxBackups,
		MaxAgeDays: botCfg.Log.MaxAgeDays,
		Compress:   botCfg.Log.Compress,
	}); err != nil {
		log.Fatalf("[ERROR] failed to init logger: %v", err)
	}
	defer logging.Sync()

	runtimeCfg, err := config.LoadRuntimeFromEnv()
	if err != nil {
		logging.Fatalw("missing required runtime env", "err", err)
	}

	tgBot, err := tgbotapi.NewBotAPI(runtimeCfg.TelegramToken)
	if err != nil {
		logging.Fatalw("failed to create telegram bot client", "err", err)
	}
	tgBot.Debug = botCfg.Debug

	historyStore, err := history.Open(botCfg.DBPath, chatbot.DefaultChatHistoryTurns)
	if err != nil {
		logging.Fatalw("failed to open history store", "path", botCfg.DBPath, "err", err)
	}
	defer func() {
		if err := historyStore.Close(); err != nil {
			logging.Errorw("failed to close history store", "err", err)
		}
	}()

	openaiClient := openai.NewClient(
		runtimeCfg.OpenAIAPIKey,
		botCfg.OpenAI.Model,
		botCfg.OpenAI.TTSModel,
		botCfg.OpenAI.TTSVoice,
		botCfg.OpenAI.TTSInstructions,
		botCfg.OpenAI.SystemPrompt,
	)
	//nolint:gosec // Non-cryptographic randomness is sufficient for conversational variation.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	processor := chatbot.NewProcessor(tgBot, botCfg, runtimeCfg, openaiClient, rng, historyStore)

	logging.Infow(
		"bot started",
		"config_path", configPath,
		"username", "@"+tgBot.Self.UserName,
		"bot_id", tgBot.Self.ID,
		"model", botCfg.OpenAI.Model,
		"tts_model", botCfg.OpenAI.TTSModel,
		"tts_voice", botCfg.OpenAI.TTSVoice,
		"tts_chance", botCfg.TTSReplyChance,
		"debug", tgBot.Debug,
		"random_reply_chance", botCfg.RandomReplyChance,
		"stickers_count", len(botCfg.StickerFileIDs),
		"sticker_chance", botCfg.RandomStickerChance,
		"reaction_chance", botCfg.ReactionChance,
		"delay_mode", "message_length",
		"conversation_db", botCfg.DBPath,
	)
	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = 30

	updates := tgBot.GetUpdatesChan(updateCfg)
	for update := range updates {
		processor.HandleUpdate(update)
	}
}
