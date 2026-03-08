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

- `.env` contains only API credentials:
  - `TELEGRAM_BOT_TOKEN`
  - `OPENAI_API_KEY`
- `bot.config.yaml` contains bot behavior settings:
  - `bot_debug`
  - `openai_model` (default: `gpt-4.1-mini`)
  - `openai_tts_model` (default: `gpt-4o-mini-tts`)
  - `openai_tts_voice` (default: `alloy`)
  - `openai_system_prompt` (default: built-in friendly prompt)
  - `bot_random_reply_chance`
  - `bot_reaction_chance` (default: `0.2`)
  - `bot_sticker_file_ids`
  - `bot_random_sticker_chance`
  - `bot_tts_reply_chance` (default: `0.5`)

## Behavior in Group Chats

- The bot listens to text messages in `group` and `supergroup` chats.
- It responds with ChatGPT when tagged (for example, `@your_bot_username hello`).
- It also responds when a user replies to a message sent by the bot.
- It can also respond randomly to about 1 out of 10 regular group messages.
- Reply delay is automatically calculated from incoming message length.
- If stickers are configured, it can randomly send a random sticker instead of text.
- For generated text replies, it sends TTS voice messages using `bot_tts_reply_chance` (with text fallback if TTS fails).
- It reacts using `bot_reaction_chance` with one of: `👍`, `💩`, `🤡`, `💯`, `🤣`.
- In [@BotFather](https://t.me/BotFather), disable privacy mode (`/setprivacy -> Disable`) so the bot can receive all group messages.
