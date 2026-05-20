package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
)

// cronScheduleWeeklyCheck runs once a week on Sunday evening to proactively evaluate the upcoming week.
func cronScheduleWeeklyCheck(db *store.DB, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().In(amsterdam)
		
		// Run only on Sunday between 19:00 and 20:00
		if now.Weekday() != time.Sunday || now.Hour() != 19 {
			return nil
		}

		slog.Info("🔍 cronScheduleWeeklyCheck: evaluating next week's schedule")

		// Calculate the start and end of NEXT week (Monday - Sunday)
		// Next Monday:
		daysUntilMonday := (8 - int(now.Weekday())) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7 // next Monday
		}
		
		startOfNextWeek := now.AddDate(0, 0, daysUntilMonday).Truncate(24 * time.Hour)
		endOfNextWeek := startOfNextWeek.AddDate(0, 0, 7).Add(-time.Second) // Sunday 23:59:59

		startIso := startOfNextWeek.Format("2006-01-02")
		endIso := endOfNextWeek.Format("2006-01-02")

		scheduleStore := store.NewScheduleStore(db)
		events, err := scheduleStore.ListRange(ctx, cfg.UserID, startIso, endIso)
		if err != nil {
			slog.Error("cronScheduleWeeklyCheck failed to fetch schedule", "error", err)
			return err
		}

		var totalHours float64
		for _, ev := range events {
			if ev.Status != "VERWIJDERD" {
				totalHours += ev.Duur
			}
		}

		contractHours := 16.0
		var message string

		if totalHours > contractHours {
			delta := totalHours - contractHours
			message = fmt.Sprintf("⚠️ Rooster Waarschuwing\n\nJe bent komende week (%s t/m %s) voor %.2f uur ingepland.\nDat is +%.2f uur boven je 16-uur contract. Hou hier rekening mee!", startIso, endIso, totalHours, delta)
		} else if totalHours < contractHours {
			delta := contractHours - totalHours
			message = fmt.Sprintf("ℹ️ Rooster Info\n\nJe bent komende week (%s t/m %s) voor %.2f uur ingepland.\nDat is -%.2f uur onder je 16-uur contract.", startIso, endIso, totalHours, delta)
		} else {
			message = fmt.Sprintf("✅ Rooster Perfect\n\nJe bent komende week (%s t/m %s) voor exact 16 uur ingepland.", startIso, endIso)
		}

		// Send to Telegram
		if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
			chatID, _ := strconv.ParseInt(cfg.TelegramChatID, 10, 64)
			client := tg.NewClient(cfg.TelegramBotToken)
			err = client.SendMessage(chatID, message)
			if err != nil {
				slog.Error("cronScheduleWeeklyCheck failed to send telegram message", "error", err)
				return err
			}
			slog.Info("📤 cronScheduleWeeklyCheck sent telegram alert", "hours", totalHours)
		}

		return nil
	}
}
