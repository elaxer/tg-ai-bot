package main

import (
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"telegram-bot/internal/bot"
	"telegram-bot/internal/config"
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
	runtimeCfg, err := config.LoadRuntimeFromEnv()
	if err != nil {
		log.Fatalf("[ERROR] %v", err)
	}

	tgBot, err := tgbotapi.NewBotAPI(runtimeCfg.TelegramToken)
	if err != nil {
		log.Fatalf("[ERROR] failed to create telegram bot client: %v", err)
	}
	tgBot.Debug = botCfg.Debug

	openaiClient := openai.NewClient(
		runtimeCfg.OpenAIAPIKey,
		botCfg.OpenAIModel,
		botCfg.OpenAITTSModel,
		botCfg.OpenAITTSVoice,
		botCfg.OpenAISystemPrompt,
	)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	processor := bot.NewProcessor(tgBot, botCfg, runtimeCfg, openaiClient, rng)

	log.Printf(
		"[INFO] bot started config=%s username=@%s id=%d model=%s tts_model=%s tts_voice=%s tts_chance=%.2f debug=%t random_reply=%.2f stickers=%d sticker_chance=%.2f reaction_chance=%.2f delay_mode=message_length",
		configPath,
		tgBot.Self.UserName,
		tgBot.Self.ID,
		botCfg.OpenAIModel,
		botCfg.OpenAITTSModel,
		botCfg.OpenAITTSVoice,
		botCfg.TTSReplyChance,
		tgBot.Debug,
		botCfg.RandomReplyChance,
		len(botCfg.StickerFileIDs),
		botCfg.RandomStickerChance,
		botCfg.ReactionChance,
	)

	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = 30

	updates := tgBot.GetUpdatesChan(updateCfg)
	for update := range updates {
		processor.HandleUpdate(update)
	}
}
