# Telegram Bot (Go)

Simple Telegram chatbot starter written in Go.

## Requirements

- Go 1.22+
- A Telegram bot token from [@BotFather](https://t.me/BotFather)
- An OpenAI API key

## Setup

```bash
cp .env.example .env
# set TELEGRAM_BOT_TOKEN and OPENAI_API_KEY in .env
set -a
source .env
set +a
```

## Run

```bash
go run ./cmd/bot
```

## Behavior in Group Chats

- The bot listens to text messages in `group` and `supergroup` chats.
- It responds with ChatGPT when tagged (for example, `@your_bot_username hello`).
- It also responds when a user replies to a message sent by the bot.
- It can also respond randomly to about 1 out of 10 regular group messages.
- Default model is `gpt-4.1-mini` (low-cost). Override with `OPENAI_MODEL`.
- Tone/persona is controlled by `OPENAI_SYSTEM_PROMPT` (defaults to informal friend-style replies).
- Human-like delay is configurable with `BOT_RESPONSE_DELAY_MIN_MS` and `BOT_RESPONSE_DELAY_MAX_MS` (default `900-2200` ms).
- In [@BotFather](https://t.me/BotFather), disable privacy mode (`/setprivacy -> Disable`) so the bot can receive all group messages.
