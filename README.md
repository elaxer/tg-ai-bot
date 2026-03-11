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
# fill bot.config.yaml with bot settings (models, logging, behavior)
```

Optional custom config path:

```bash
export BOT_CONFIG_PATH=./bot.config.yaml
```

## Run

```bash
go run ./cmd/bot
```

### Meme Sender Worker

Send a random meme (image post) from a configurable list of subreddits to every known group chat roughly every 5–6 hours:

```bash
go run ./cmd/meme-sender
```

Flags include `-once` for a single cycle, `-dry-run` to log without sending, `-check-interval` to change the scan cadence, and `-known-chats` if you keep the chat list in a non-default location.

## ELK Log Viewer (Docker Compose)

Use the bundled ELK stack to view bot logs in Kibana.

1. Start bot so it writes JSON logs to `logs/bot.log`.
2. Start ELK:

```bash
docker compose -f docker-compose.elk.yml up -d
```

3. Open Kibana: `http://localhost:5601`
4. Create a data view:
   - Pattern: `bot-logs-*`
   - Time field: `@timestamp`
5. Open Discover and filter/search logs.

Useful commands:

```bash
# Check stack status
docker compose -f docker-compose.elk.yml ps

# Follow Logstash ingestion logs
docker compose -f docker-compose.elk.yml logs -f logstash

# Stop ELK
docker compose -f docker-compose.elk.yml down
```

## Configuration Split

- `.env` contains only API credentials:
  - `TELEGRAM_BOT_TOKEN`
  - `OPENAI_API_KEY`
- `bot.config.yaml` contains bot behavior settings:
  - `bot_debug`
  - `log_file_path` (default: `logs/bot.log`)
  - `log_level` (default: `info`)
  - `log_max_size_mb` (default: `50`)
  - `log_max_backups` (default: `10`)
  - `log_max_age_days` (default: `30`)
  - `log_compress` (default: `false`)
  - `openai_model` (default: `gpt-4.1-mini`)
  - `openai_tts_model` (default: `gpt-4o-mini-tts`)
  - `openai_tts_voice` (default: `alloy`)
  - `openai_tts_instructions` (default: `Speak with a natural England (British English) accent.`)
  - `openai_system_prompt` (default: built-in friendly prompt)
  - `bot_random_reply_chance`
  - `bot_reaction_chance` (default: `0.2`)
  - `bot_reactions` (default: `["👍","💩","🤡","💯","🤣"]`)
  - `bot_sticker_file_ids`
  - `bot_random_sticker_chance`
  - `bot_tts_reply_chance` (default: `0.5`)
  - `bot_daily_message_interval` (default: `24h`)
  - `conversation_db_path` (default: `data/conversations.db`)
  - `bot_meme_subreddits` (default: `["memes","dankmemes","me_irl"]`)
  - `bot_meme_interval_min` (default: `5h`)
  - `bot_meme_interval_max` (default: `6h`)

## Behavior

- The bot listens to text messages in `group` and `supergroup` chats.
- It responds with ChatGPT when tagged (for example, `@your_bot_username hello`).
- It also responds when a user replies to a message sent by the bot.
- It can also respond randomly to about 1 out of 10 regular group messages.
- In private chats (`type=private`) it now responds to every incoming message and keeps a short rolling memory (about 20 turns) so conversations feel like ChatGPT.
- In group chats the bot also keeps a short per-chat memory (about 20 recent turns it participated in) so follow-up mentions have context.
- Each user can set a personal tone for replies with `/persona <instructions>` (or `!persona ...`); use `/persona show` to view it or `/persona clear` to reset.
- Logs are written in JSON to `log_file_path` with rotation enabled.
- Reply delay is automatically calculated from incoming message length.
- If stickers are configured, it can randomly send a random sticker instead of text.
- For generated text replies, it sends TTS voice messages using `bot_tts_reply_chance` (with text fallback if TTS fails).
- It reacts using `bot_reaction_chance` with emojis from `bot_reactions`.
- In [@BotFather](https://t.me/BotFather), disable privacy mode (`/setprivacy -> Disable`) so the bot can receive all group messages.
