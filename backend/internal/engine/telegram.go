package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
)

const telegramPollStream = "owner-bot"

// loopTelegram durably queues updates before acknowledging Telegram's offset.
// Processing is deliberately ordered: this owner bot values no lost commands
// over burst throughput, and AI concurrency is bounded elsewhere too.
func (e *Engine) loopTelegram(ctx context.Context) {
	token := e.cfg.TelegramBotToken
	if token == "" {
		slog.Warn("TELEGRAM_BOT_TOKEN not set, telegram poller disabled")
		return
	}
	client := tg.NewClient(token)
	_ = client.DeleteWebhook(false)
	if err := client.SetMyCommands(telegramMenuCommands()); err != nil {
		slog.Warn("telegram setMyCommands failed", "error", err)
	}

	offset, err := e.db.LoadTelegramOffset(ctx, telegramPollStream)
	if err != nil {
		slog.Error("telegram durable offset load failed", "error", err)
		return
	}
	slog.Info("🤖 telegram poller started (durable queue)", "offset", offset)
	backoff := 3 * time.Second

	for ctx.Err() == nil {
		if _, err := e.drainTelegramQueue(ctx, client); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("telegram durable queue processing paused", "error", err, "backoff", backoff)
			sleepCtx(ctx, backoff)
			continue
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
		backoff = 3 * time.Second

		if len(updates) > 0 {
			records := make([]store.TelegramUpdateRecord, 0, len(updates))
			for _, update := range updates {
				payload, marshalErr := json.Marshal(update)
				if marshalErr != nil {
					slog.Error("telegram update marshal failed; offset not advanced", "updateID", update.UpdateID, "error", marshalErr)
					records = nil
					break
				}
				records = append(records, store.TelegramUpdateRecord{UpdateID: update.UpdateID, Payload: payload})
			}
			if records == nil {
				sleepCtx(ctx, backoff)
				continue
			}
			nextOffset, persistErr := e.db.PersistTelegramUpdates(ctx, telegramPollStream, records)
			if persistErr != nil {
				slog.Error("telegram updates not durably queued; offset unchanged", "error", persistErr)
				sleepCtx(ctx, backoff)
				continue
			}
			offset = nextOffset
			slog.Info("📩 telegram updates durably queued", "count", len(records), "nextOffset", offset)
		}
		sleepCtx(ctx, 100*time.Millisecond)
	}
}

func (e *Engine) drainTelegramQueue(ctx context.Context, client *tg.Client) (processed int, err error) {
	for ctx.Err() == nil {
		record, err := e.db.ClaimTelegramUpdate(ctx)
		if err != nil {
			return processed, err
		}
		if record == nil {
			return processed, nil
		}
		processErr := e.processQueuedTelegramUpdate(ctx, client, record.Payload)
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		if processErr != nil {
			retryErr := e.db.RetryTelegramUpdate(persistCtx, record.UpdateID, record.Attempts, processErr)
			cancel()
			if retryErr != nil {
				return processed, errors.Join(processErr, retryErr)
			}
			return processed, processErr
		}
		completeErr := e.db.CompleteTelegramUpdate(persistCtx, record.UpdateID)
		cancel()
		if completeErr != nil {
			return processed, completeErr
		}
		processed++
	}
	return processed, ctx.Err()
}

func (e *Engine) processQueuedTelegramUpdate(ctx context.Context, client *tg.Client, payload []byte) (err error) {
	var update tg.Update
	if err := json.Unmarshal(payload, &update); err != nil {
		return fmt.Errorf("telegram update payload decode: %w", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("telegram processUpdate panic: %v", recovered)
		}
	}()
	select {
	case e.aiSem <- struct{}{}:
		defer func() { <-e.aiSem }()
	case <-ctx.Done():
		return ctx.Err()
	}
	e.processUpdate(ctx, client, update)
	return nil
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
			slog.Warn("telegram callback query from non-owner chat rejected", "chatID", chatID)
			if err := client.AnswerCallbackQuery(cb.ID, "Ongeautoriseerd."); err != nil {
				slog.Warn("telegram AnswerCallbackQuery failed", "error", err)
			}
			return
		}

		// Acknowledge the click immediately so the loading spinner goes away
		if err := client.AnswerCallbackQuery(cb.ID, ""); err != nil {
			slog.Warn("telegram AnswerCallbackQuery failed", "error", err)
		}

		// Process the callback data exactly as if the user typed it. Pass the
		// tapped message's ID so button-originated actions (note archive/done/
		// pin, pending confirm/reject) can edit that message in place instead
		// of always sending a new one and leaving the original's now-stale
		// buttons live and re-tappable.
		messageID := cb.Message.MessageID
		e.processText(ctx, client, chatID, strings.TrimSpace(cb.Data), &messageID)
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

	e.processText(ctx, client, chatID, strings.TrimSpace(msg.Text), nil)
}
