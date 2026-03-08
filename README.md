# Telegram Bot (Go)

Simple Telegram chatbot starter written in Go.

## Requirements

- Go 1.22+
- A Telegram bot token from [@BotFather](https://t.me/BotFather)
- An OpenAI API key

## Setup

```bash
cp .env.example .env
cp bot.config.example.yaml bot.config.yaml
# fill .env with TELEGRAM_BOT_TOKEN and OPENAI_API_KEY
# fill bot.config.yaml with BOT_* behavior settings
set -a
source .env
set +a
```

Optional custom config path:

```bash
export BOT_CONFIG_PATH=./bot.config.yaml
```

## Run

```bash
go run ./cmd/bot
```

## Configuration Split

- `.env` contains API credentials and OpenAI options:
  - `TELEGRAM_BOT_TOKEN`
  - `OPENAI_API_KEY`
  - `OPENAI_MODEL`
  - `OPENAI_TTS_MODEL` (default: `gpt-4o-mini-tts`)
  - `OPENAI_SYSTEM_PROMPT`
- `bot.config.yaml` contains bot behavior settings:
  - `bot_debug`
  - `bot_response_delay_min_ms`
  - `bot_response_delay_max_ms`
  - `bot_random_reply_chance`
  - `bot_sticker_file_ids`
  - `bot_random_sticker_chance`
  - `bot_tts_reply_chance` (default: `0.5`)
  - `bot_tts_voice` (default: `alloy`)

## Behavior in Group Chats

- The bot listens to text messages in `group` and `supergroup` chats.
- It responds with ChatGPT when tagged (for example, `@your_bot_username hello`).
- It also responds when a user replies to a message sent by the bot.
- It can also respond randomly to about 1 out of 10 regular group messages.
- If stickers are configured, it can randomly send a random sticker instead of text.
- For generated text replies, it sends TTS voice messages using `bot_tts_reply_chance` (with text fallback if TTS fails).
- It reacts to about 1 out of 5 group messages using one of: `👍`, `💩`, `🤡`, `💯`, `🤣`.
- In [@BotFather](https://t.me/BotFather), disable privacy mode (`/setprivacy -> Disable`) so the bot can receive all group messages.
