package memes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	defaultSendPause     = 2 * time.Second
	memeFetchMaxAttempts = 5
	memeRecentLimit      = 20
)

// Config describes the dependencies and runtime knobs required by the meme worker.
type Config struct {
	KnownChatsPath string
	StatePath      string
	Subreddits     []string
	MinInterval    time.Duration
	MaxInterval    time.Duration
	RequestTimeout time.Duration
	DryRun         bool
}

// Worker periodically fetches and sends memes into known Telegram chats.
type Worker struct {
	bot        *tgbotapi.BotAPI
	cfg        Config
	httpClient *http.Client
	rng        *rand.Rand
	state      map[int64]chatMemeState
}

// NewWorker wires a Worker with all required infrastructure and loads persisted state.
func NewWorker(bot *tgbotapi.BotAPI, cfg Config) (*Worker, error) {
	if bot == nil {
		return nil, fmt.Errorf("bot client is required")
	}
	if len(cfg.Subreddits) == 0 {
		return nil, fmt.Errorf("at least one subreddit must be configured")
	}
	if cfg.MinInterval <= 0 || cfg.MaxInterval <= 0 {
		return nil, fmt.Errorf("min and max interval must be > 0")
	}
	if cfg.MinInterval > cfg.MaxInterval {
		cfg.MinInterval, cfg.MaxInterval = cfg.MaxInterval, cfg.MinInterval
	}
	state, err := loadMemeState(cfg.StatePath)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: cfg.RequestTimeout}
	return &Worker{
		bot:        bot,
		cfg:        cfg,
		httpClient: client,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		state:      state,
	}, nil
}

// RunLoop continually scans for due chats and sends memes at the configured interval.
func (w *Worker) RunLoop(ctx context.Context, checkInterval time.Duration) error {
	if checkInterval <= 0 {
		return fmt.Errorf("check interval must be > 0")
	}
	log.Printf("meme worker started: known=%s state=%s min=%s max=%s check=%s dry-run=%t",
		w.cfg.KnownChatsPath, w.cfg.StatePath, w.cfg.MinInterval, w.cfg.MaxInterval, checkInterval, w.cfg.DryRun)

	if _, _, err := w.RunCycle(ctx); err != nil {
		log.Printf("initial cycle failed: %v", err)
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("meme worker stopped")
			return nil
		case <-ticker.C:
			if _, _, err := w.RunCycle(ctx); err != nil {
				log.Printf("cycle failed: %v", err)
			}
		}
	}
}

// RunCycle scans known chats, sends memes to due chats, and persists the state.
func (w *Worker) RunCycle(ctx context.Context) (int, int, error) {
	knownChats, err := loadKnownChats(w.cfg.KnownChatsPath)
	if err != nil {
		return 0, 0, err
	}
	if len(knownChats) == 0 {
		return 0, 0, errors.New("no known chats")
	}

	if w.ensureChats(knownChats) {
		if err := saveMemeState(w.cfg.StatePath, w.state); err != nil {
			log.Printf("save meme state failed: %v", err)
		}
	}

	dueChats := w.collectDueChats(knownChats)
	if len(dueChats) == 0 {
		return len(knownChats), 0, nil
	}

	sent := 0
	for _, chatID := range dueChats {
		select {
		case <-ctx.Done():
			return len(knownChats), sent, ctx.Err()
		default:
		}
		title := knownChats[chatID]
		if title == "" {
			title = fmt.Sprintf("chat#%d", chatID)
		}
		if err := w.sendMeme(ctx, chatID, title); err != nil {
			log.Printf("send meme failed chat_id=%d title=%q err=%v", chatID, title, err)
			continue
		}
		if !w.cfg.DryRun {
			if err := w.markSent(chatID); err != nil {
				log.Printf("state update failed chat_id=%d err=%v", chatID, err)
			}
		}
		sent++
		time.Sleep(defaultSendPause)
	}
	if w.cfg.DryRun {
		return len(knownChats), sent, nil
	}
	if err := saveMemeState(w.cfg.StatePath, w.state); err != nil {
		return len(knownChats), sent, fmt.Errorf("persist meme state: %w", err)
	}
	return len(knownChats), sent, nil
}

func (w *Worker) markSent(chatID int64) error {
	nextDelay := randomInterval(w.rng, w.cfg.MinInterval, w.cfg.MaxInterval)
	next := time.Now().Add(nextDelay).Unix()
	state := w.state[chatID]
	state.NextDueUnix = next
	w.state[chatID] = state
	return nil
}

func (w *Worker) ensureChats(known map[int64]string) bool {
	if w.state == nil {
		w.state = make(map[int64]chatMemeState)
	}
	changed := false
	for chatID, title := range known {
		state, exists := w.state[chatID]
		cleanTitle := strings.TrimSpace(title)
		if !exists {
			state = chatMemeState{}
			changed = true
		}
		if cleanTitle != "" && state.Title != cleanTitle {
			state.Title = cleanTitle
			changed = true
		}
		w.state[chatID] = state
	}
	return changed
}

func (w *Worker) collectDueChats(known map[int64]string) []int64 {
	nowUnix := time.Now().Unix()
	due := make([]int64, 0, len(known))
	for chatID := range known {
		state := w.state[chatID]
		if state.NextDueUnix == 0 || nowUnix >= state.NextDueUnix {
			due = append(due, chatID)
		}
	}
	return due
}

func (w *Worker) sendMeme(parentCtx context.Context, chatID int64, title string) error {
	var (
		meme      memePost
		memeID    string
		lastErr   error
		subreddit string
	)
	for attempt := 1; attempt <= memeFetchMaxAttempts; attempt++ {
		select {
		case <-parentCtx.Done():
			return parentCtx.Err()
		default:
		}
		subreddit = w.cfg.Subreddits[w.rng.Intn(len(w.cfg.Subreddits))]
		meme, lastErr = fetchRandomMeme(parentCtx, w.httpClient, subreddit, w.cfg.RequestTimeout, w.rng)
		if lastErr == nil {
			memeID = memeIdentifier(meme)
			switch {
			case memeID == "":
				lastErr = errors.New("meme without identifier")
			case w.hasRecentlySent(chatID, memeID):
				lastErr = fmt.Errorf("duplicate meme id=%s", memeID)
				log.Printf("duplicate meme skipped chat_id=%d title=%q subreddit=%s id=%s", chatID, title, subreddit, memeID)
			default:
				break
			}
		}
		if lastErr == nil {
			break
		}
		log.Printf("fetch meme attempt %d/%d failed chat_id=%d title=%q subreddit=%s err=%v", attempt, memeFetchMaxAttempts, chatID, title, subreddit, lastErr)
		time.Sleep(redditRetryInterval)
	}
	if lastErr != nil {
		return fmt.Errorf("fetch meme subreddit=%s: %w", subreddit, lastErr)
	}
	if w.cfg.DryRun {
		log.Printf("dry-run meme chat_id=%d title=%q subreddit=%s image=%s", chatID, title, subreddit, meme.ImageURL)
		return nil
	}

	msg := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(meme.ImageURL))
	if _, err := w.bot.Send(msg); err != nil {
		return fmt.Errorf("send telegram photo: %w", err)
	}
	if memeID != "" {
		w.rememberMeme(chatID, memeID)
	}
	log.Printf("sent meme chat_id=%d title=%q subreddit=%s", chatID, title, subreddit)
	return nil
}

func (w *Worker) hasRecentlySent(chatID int64, memeID string) bool {
	if memeID == "" {
		return false
	}
	state, ok := w.state[chatID]
	if !ok {
		return false
	}
	for _, existing := range state.RecentMemes {
		if existing == memeID {
			return true
		}
	}
	return false
}

func (w *Worker) rememberMeme(chatID int64, memeID string) {
	if memeID == "" {
		return
	}
	state := w.state[chatID]
	filtered := make([]string, 0, memeRecentLimit-1)
	for _, existing := range state.RecentMemes {
		if existing == memeID {
			continue
		}
		if len(filtered) >= memeRecentLimit-1 {
			break
		}
		filtered = append(filtered, existing)
	}
	state.RecentMemes = append([]string{memeID}, filtered...)
	w.state[chatID] = state
}

func randomInterval(rng *rand.Rand, min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	span := max - min
	if span <= 0 {
		return min
	}
	return min + time.Duration(rng.Int63n(int64(span)))
}
