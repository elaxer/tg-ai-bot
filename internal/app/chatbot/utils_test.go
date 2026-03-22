package chatbot

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/elaxer/tg-ai-bot/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestUtilityHelpers(t *testing.T) {
	t.Parallel()

	msg := &tgbotapi.Message{
		Text:    "caption should not win",
		Caption: "caption",
		From: &tgbotapi.User{
			ID:        7,
			UserName:  "alice",
			FirstName: "Alice",
			LastName:  "Smith",
		},
		Photo: []tgbotapi.PhotoSize{
			{FileID: "small"},
			{FileID: "large"},
		},
	}

	if got := getMessageText(msg); got != "caption should not win" {
		t.Fatalf("getMessageText() = %q", got)
	}
	if got := extractBestPhotoFileID(msg); got != "large" {
		t.Fatalf("extractBestPhotoFileID() = %q", got)
	}

	sender := extractSenderInfo(msg)
	if sender.Username != "alice" || sender.DisplayName != "Alice Smith" || sender.ID != 7 {
		t.Fatalf("extractSenderInfo() = %#v", sender)
	}

	if got := extractPromptText(" hi @mybot there ", "@mybot", true); got != "hi  there" {
		t.Fatalf("extractPromptText() = %q", got)
	}
	if got := shorten("abcdef", 3); got != "abc..." {
		t.Fatalf("shorten() = %q", got)
	}
	if got := delayFromMessageLength("hello"); got != 525*time.Millisecond {
		t.Fatalf("delayFromMessageLength() = %v", got)
	}
}

func TestChatNameAndReplyContext(t *testing.T) {
	t.Parallel()

	chat := &tgbotapi.Chat{Title: "My Group"}
	if got := chatName(chat); got != "My Group" {
		t.Fatalf("chatName() = %q", got)
	}
	chat.Title = ""
	chat.UserName = "channel"
	if got := chatName(chat); got != "@channel" {
		t.Fatalf("chatName() = %q", got)
	}

	msg := &tgbotapi.Message{
		From:    &tgbotapi.User{ID: 8, FirstName: "Bob"},
		Caption: strings.Repeat("x", 600),
	}
	context := buildReplyContext(msg)
	if !strings.Contains(context, `id=8`) || !strings.Contains(context, `display_name="Bob"`) {
		t.Fatalf("buildReplyContext() = %q", context)
	}
	if !strings.Contains(context, "...") {
		t.Fatalf("buildReplyContext() did not shorten long text: %q", context)
	}
}

func TestUserLabels(t *testing.T) {
	t.Parallel()

	if got := getUserLabel(SenderInfo{Username: "alice"}); got != "@alice" {
		t.Fatalf("getUserLabel(username) = %q", got)
	}
	if got := getUserLabel(SenderInfo{DisplayName: "Alice Smith"}); got != "Alice Smith" {
		t.Fatalf("getUserLabel(display) = %q", got)
	}
	if got := getUserLabel(SenderInfo{ID: 55}); got != "user#55" {
		t.Fatalf("getUserLabel(id) = %q", got)
	}
	if got := getUserLabel(SenderInfo{}); got != "user" {
		t.Fatalf("getUserLabel(empty) = %q", got)
	}
	if got := formatUserTurn(SenderInfo{Username: "alice"}, " hello "); got != "@alice: hello" {
		t.Fatalf("formatUserTurn() = %q", got)
	}
}

func TestProcessorDecisionHelpers(t *testing.T) {
	t.Parallel()

	p := &Processor{
		bot: &tgbotapi.BotAPI{
			Self: tgbotapi.User{ID: 42, UserName: "MyBot"},
		},
		cfg: config.Bot{
			RandomReplyChance:   0,
			RandomStickerChance: 1,
			StickerFileIDs:      []string{"sticker"},
		},
		runtime: config.Runtime{BotTag: "@mybot"},
		//nolint:gosec // Deterministic test fixture RNG.
		rng: rand.New(rand.NewSource(1)),
	}

	if !p.shouldIgnoreMessage(nil) {
		t.Fatal("shouldIgnoreMessage(nil) = false, want true")
	}

	groupMsg := &tgbotapi.Message{
		Text: "@mybot hello there",
		Chat: &tgbotapi.Chat{ID: 10, Type: chatTypeGroup},
		From: &tgbotapi.User{ID: 7, UserName: "alice"},
	}
	decision, ok := p.buildGroupDecision(groupMsg)
	if !ok {
		t.Fatal("buildGroupDecision() ok = false, want true")
	}
	if decision.Trigger != TriggerTag {
		t.Fatalf("Trigger = %q, want %q", decision.Trigger, TriggerTag)
	}
	if decision.PromptText != "hello there" {
		t.Fatalf("PromptText = %q", decision.PromptText)
	}
	if !decision.ShouldSendSticker {
		t.Fatal("ShouldSendSticker = false, want true")
	}

	privateMsg := &tgbotapi.Message{
		Caption: "look",
		Chat:    &tgbotapi.Chat{ID: 11, Type: chatTypePrivate},
		From:    &tgbotapi.User{ID: 9, FirstName: "Bob"},
		Photo:   []tgbotapi.PhotoSize{{FileID: "img1"}},
	}
	privateDecision, ok := p.buildPrivateDecision(privateMsg)
	if !ok {
		t.Fatal("buildPrivateDecision() ok = false, want true")
	}
	if privateDecision.Trigger != TriggerPrivate {
		t.Fatalf("Trigger = %q, want %q", privateDecision.Trigger, TriggerPrivate)
	}
	if privateDecision.PhotoFileID != "img1" {
		t.Fatalf("PhotoFileID = %q", privateDecision.PhotoFileID)
	}
	if privateDecision.ShouldSendSticker {
		t.Fatal("ShouldSendSticker = true with photo, want false")
	}
}
