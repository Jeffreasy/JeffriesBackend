package engine

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
)

// loopTelegram polls for Telegram updates and processes them natively in Go.
func (e *Engine) loopTelegram(ctx context.Context) {
	token := e.cfg.TelegramBotToken
	if token == "" {
		slog.Warn("TELEGRAM_BOT_TOKEN not set, telegram poller disabled")
		return
	}

	slog.Info("🤖 telegram poller started (native Go)")

	client := tg.NewClient(token)
	_ = client.DeleteWebhook(false)

	var offset int64
	backoff := 3 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := client.GetUpdatesContext(ctx, offset, 10)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("telegram getUpdates failed", "error", err, "backoff", backoff)
			sleepCtx(ctx, backoff)
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			continue
		}

		// Reset backoff on success
		backoff = 3 * time.Second

		if len(updates) > 0 {
			slog.Info("📩 telegram updates received", "count", len(updates))
		}

		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if ctx.Err() != nil {
				return
			}
			go func(u tg.Update) {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("telegram processUpdate panic", "recover", r)
					}
				}()
				if ctx.Err() != nil {
					return
				}
				e.processUpdate(ctx, client, u)
			}(update)
		}

		sleepCtx(ctx, 100*time.Millisecond)
	}
}

func (e *Engine) processUpdate(ctx context.Context, client *tg.Client, update tg.Update) {
	if ctx.Err() != nil {
		return
	}

	// Handle Callback Queries (Button Clicks)
	if cb := update.CallbackQuery; cb != nil {
		if cb.Message == nil || cb.Message.Chat == nil {
			return
		}
		chatID := cb.Message.Chat.ID

		ownerID := e.cfg.TelegramChatID
		if ownerID == "" || strconv.FormatInt(chatID, 10) != ownerID {
			_ = client.AnswerCallbackQuery(cb.ID, "Ongeautoriseerd.")
			return
		}

		// Acknowledge the click immediately so the loading spinner goes away
		_ = client.AnswerCallbackQuery(cb.ID, "")

		// Process the callback data exactly as if the user typed it
		e.processText(ctx, client, chatID, strings.TrimSpace(cb.Data))
		return
	}

	msg := update.Message
	if msg == nil || msg.Chat == nil {
		return
	}
	chatID := msg.Chat.ID

	// Security: owner-only
	ownerID := e.cfg.TelegramChatID
	if ownerID == "" || strconv.FormatInt(chatID, 10) != ownerID {
		_ = client.SendMessage(chatID, "Je bent niet geautoriseerd om deze bot te gebruiken.")
		return
	}

	// Voice message → Groq Whisper
	voice := msg.Voice
	if voice == nil {
		voice = msg.Audio
	}
	if voice != nil && msg.Text == "" {
		e.handleVoice(ctx, client, chatID, voice.FileID)
		return
	}

	if msg.Text == "" {
		return
	}

	e.processText(ctx, client, chatID, strings.TrimSpace(msg.Text))
}
