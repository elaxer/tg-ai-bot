package chatbot

import (
	"math/rand"
	"sync"
	"time"

	"telegram-bot/internal/config"
	"telegram-bot/internal/infra/openai"
	"telegram-bot/internal/storage/history"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type MessageTrigger string

const (
	TriggerTag        MessageTrigger = "tag"
	TriggerReplyToBot MessageTrigger = "reply_to_bot"
	TriggerRandom     MessageTrigger = "random"
	TriggerPrivate    MessageTrigger = "private"
)

const (
	dailyStatePath      = "data/daily_messages_state.json"
	stickerDelayMin     = 3 * time.Second
	stickerDelayMax     = 5 * time.Second
	chatHistoryMaxTurns = 20
	chatRoleUser        = "user"
	chatRoleAssistant   = "assistant"
)

const DefaultChatHistoryTurns = chatHistoryMaxTurns

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

type chatDailyState struct {
	Title        string `json:"title"`
	LastSentUnix int64  `json:"last_sent_unix"`
}

type dailyStateFile struct {
	Chats map[string]chatDailyState `json:"chats"`
}

// Processor is the main orchestrator for handling Telegram updates.
type Processor struct {
	bot       *tgbotapi.BotAPI
	cfg       config.Bot
	runtime   config.Runtime
	openai    *openai.Client
	rng       *rand.Rand
	reactions []string
	stateMu   sync.Mutex
	chats     map[int64]chatDailyState
	history   *history.Store
}
