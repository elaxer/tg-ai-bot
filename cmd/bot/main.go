package main

import (
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"telegram-bot/internal/bot"
	"telegram-bot/internal/config"
	"telegram-bot/internal/logging"
	"telegram-bot/internal/openai"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	log.SetFlags(log.LstdFlags)

	if err := config.LoadDotEnv(".env"); err != nil {
		log.Fatalf("[ERROR] failed to load .env: %v", err)
	}

	configPath := strings.TrimSpace(os.Getenv("BOT_CONFIG_PATH"))
	if configPath == "" {
		configPath = "bot.config.yaml"
	}

	botCfg, err := config.LoadBot(configPath)
	if err != nil {
		log.Fatalf("[ERROR] failed to load config %q: %v", configPath, err)
	}
	if err := logging.Init(logging.Config{
		FilePath:   botCfg.LogFilePath,
		Level:      botCfg.LogLevel,
		MaxSizeMB:  botCfg.LogMaxSizeMB,
		MaxBackups: botCfg.LogMaxBackups,
		MaxAgeDays: botCfg.LogMaxAgeDays,
		Compress:   botCfg.LogCompress,
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

	openaiClient := openai.NewClient(
		runtimeCfg.OpenAIAPIKey,
		botCfg.OpenAIModel,
		botCfg.OpenAITTSModel,
		botCfg.OpenAITTSVoice,
		botCfg.OpenAITTSInstructions,
		botCfg.OpenAISystemPrompt,
	)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	processor := bot.NewProcessor(tgBot, botCfg, runtimeCfg, openaiClient, rng)

	logging.Infow(
		"bot started",
		"config_path", configPath,
		"username", "@"+tgBot.Self.UserName,
		"bot_id", tgBot.Self.ID,
		"model", botCfg.OpenAIModel,
		"tts_model", botCfg.OpenAITTSModel,
		"tts_voice", botCfg.OpenAITTSVoice,
		"tts_chance", botCfg.TTSReplyChance,
		"debug", tgBot.Debug,
		"random_reply_chance", botCfg.RandomReplyChance,
		"stickers_count", len(botCfg.StickerFileIDs),
		"sticker_chance", botCfg.RandomStickerChance,
		"reaction_chance", botCfg.ReactionChance,
		"delay_mode", "message_length",
	)

	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = 30

	updates := tgBot.GetUpdatesChan(updateCfg)
	for update := range updates {
		processor.HandleUpdate(update)
	}
}
