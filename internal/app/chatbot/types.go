// Package chatbot handles Telegram update processing and response generation.
package chatbot

import (
	"math/rand"
	"time"

	"github.com/elaxer/tg-ai-bot/internal/config"
	"github.com/elaxer/tg-ai-bot/internal/infra/openai"
	"github.com/elaxer/tg-ai-bot/internal/storage/history"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	stickerDelayMin     = 3 * time.Second
	stickerDelayMax     = 5 * time.Second
	chatHistoryMaxTurns = 20
	chatRoleUser        = "user"
	chatRoleAssistant   = "assistant"

	TriggerTag        MessageTrigger = "tag"
	TriggerReplyToBot MessageTrigger = "reply_to_bot"
	TriggerRandom     MessageTrigger = "random"
	TriggerPrivate    MessageTrigger = "private"
)

const DefaultChatHistoryTurns = chatHistoryMaxTurns

type MessageTrigger string

type SenderInfo struct {
	ID          int64
	Username    string
	DisplayName string
}

type ResponseDecision struct {
	Trigger             MessageTrigger
	PromptText          string
	Sender              SenderInfo
	ReplyContext        string
	PhotoFileID         string
	ShouldSendSticker   bool
	ConversationContext string
	SenderPersona       string
}

// Processor is the main orchestrator for handling Telegram updates.
type Processor struct {
	bot       *tgbotapi.BotAPI
	cfg       config.Bot
	runtime   config.Runtime
	openai    *openai.Client
	rng       *rand.Rand
	reactions []string
	history   *history.Store
}
