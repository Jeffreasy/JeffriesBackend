package google

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ─── Gmail API types ─────────────────────────────────────────────────────────

type gmailListResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
	NextPageToken string `json:"nextPageToken"`
}

type gmailHistoryResponse struct {
	History       []gmailHistoryEntry `json:"history"`
	HistoryID     string              `json:"historyId"`
	NextPageToken string              `json:"nextPageToken"`
}

type gmailHistoryEntry struct {
	MessagesAdded []struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	} `json:"messagesAdded"`
	LabelsAdded []struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	} `json:"labelsAdded"`
	LabelsRemoved []struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	} `json:"labelsRemoved"`
}

type gmailMessage struct {
	ID           string   `json:"id"`
	ThreadID     string   `json:"threadId"`
	InternalDate string   `json:"internalDate"`
	Snippet      string   `json:"snippet"`
	LabelIDs     []string `json:"labelIds"`
	Payload      struct {
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		Parts []struct {
			Filename string `json:"filename"`
		} `json:"parts"`
	} `json:"payload"`
}

type gmailProfileResponse struct {
	HistoryID string `json:"historyId"`
}

// ParsedEmail represents a synced email ready for PostgreSQL storage.
type ParsedEmail struct {
	UserID        string   `json:"user_id"`
	GmailID       string   `json:"gmail_id"`
	ThreadID      string   `json:"thread_id"`
	From          string   `json:"from_addr"`
	To            string   `json:"to_addr"`
	CC            string   `json:"cc"`
	BCC           string   `json:"bcc"`
	Subject       string   `json:"subject"`
	Snippet       string   `json:"snippet"`
	Datum         string   `json:"datum"`
	Ontvangen     int64    `json:"ontvangen"`
	IsGelezen     bool     `json:"is_gelezen"`
	IsSter        bool     `json:"is_ster"`
	IsVerwijderd  bool     `json:"is_verwijderd"`
	IsDraft       bool     `json:"is_draft"`
	LabelIDs      []string `json:"label_ids"`
	Categorie     string   `json:"categorie"`
	HeeftBijlagen bool     `json:"heeft_bijlagen"`
	BijlagenCount int      `json:"bijlagen_count"`
	SearchText    string   `json:"search_text"`
	SyncedAt      string   `json:"synced_at"`
}

// GmailSyncResult contains the result of a Gmail sync operation.
type GmailSyncResult struct {
	Synced int    `json:"synced"`
	Mode   string `json:"mode"`
}

const (
	gmailBase = "https://gmail.googleapis.com/gmail/v1/users/me"
	// maxInitialSync caps how many of the newest messages a full sync captures.
	// A full sync runs only on first sync or expired-history fallback; this is an
	// explicit, documented bound rather than a silent single-page truncation.
	maxInitialSync = 500
	gmailPageSize  = 250
	messageWorkers = 8
)

// SyncGmail performs incremental or full Gmail sync and returns parsed emails.
//
// A full sync only captures the newest maxInitialSync messages, so it is used
// only when there is no historyId yet, or when the stored historyId has expired
// (Gmail returns HTTP 404 — it retains history for a bounded window). Any other
// incremental error (transient 5xx/429/network) is propagated so the cron simply
// retries next tick instead of doing a wasteful, lossy full re-list.
func SyncGmail(ctx context.Context, client *OAuthClient, userID, historyID string) (*GmailSyncResult, []ParsedEmail, string, error) {
	if historyID != "" {
		result, emails, newHistID, err := incrementalGmailSync(ctx, client, userID, historyID)
		if err == nil {
			return result, emails, newHistID, nil
		}
		if StatusCode(err) != http.StatusNotFound {
			return nil, nil, "", err
		}
		slog.Warn("gmail historyId expired (404), falling back to full sync", "error", err)
	}

	return fullGmailSync(ctx, client, userID)
}

func incrementalGmailSync(ctx context.Context, client *OAuthClient, userID, historyID string) (*GmailSyncResult, []ParsedEmail, string, error) {
	changedIDs := make(map[string]bool)
	newHistoryID := ""
	pageToken := ""

	// Paginate the full history: dropping later pages while still advancing the
	// historyId would silently lose changes that accumulated between ticks.
	for {
		params := url.Values{
			"startHistoryId": {historyID},
			"historyTypes":   {"messageAdded", "labelAdded", "labelRemoved"},
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		var histResp gmailHistoryResponse
		if err := client.GetJSON(ctx, gmailBase+"/history?"+params.Encode(), &histResp); err != nil {
			return nil, nil, "", fmt.Errorf("history list: %w", err)
		}

		for _, h := range histResp.History {
			for _, m := range h.MessagesAdded {
				changedIDs[m.Message.ID] = true
			}
			for _, m := range h.LabelsAdded {
				changedIDs[m.Message.ID] = true
			}
			for _, m := range h.LabelsRemoved {
				changedIDs[m.Message.ID] = true
			}
		}
		if histResp.HistoryID != "" {
			newHistoryID = histResp.HistoryID
		}

		pageToken = histResp.NextPageToken
		if pageToken == "" {
			break
		}
	}

	if len(changedIDs) == 0 {
		return &GmailSyncResult{Synced: 0, Mode: "incremental"}, nil, newHistoryID, nil
	}

	ids := make([]string, 0, len(changedIDs))
	for id := range changedIDs {
		ids = append(ids, id)
	}

	emails, err := fetchMessageBatch(ctx, client, userID, ids)
	if err != nil {
		return nil, nil, "", err
	}

	return &GmailSyncResult{Synced: len(emails), Mode: "incremental"}, emails, newHistoryID, nil
}

func fullGmailSync(ctx context.Context, client *OAuthClient, userID string) (*GmailSyncResult, []ParsedEmail, string, error) {
	ids := make([]string, 0, maxInitialSync)
	pageToken := ""

	for len(ids) < maxInitialSync {
		params := url.Values{"maxResults": {strconv.Itoa(gmailPageSize)}}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		var listResp gmailListResponse
		if err := client.GetJSON(ctx, gmailBase+"/messages?"+params.Encode(), &listResp); err != nil {
			return nil, nil, "", fmt.Errorf("messages list: %w", err)
		}

		for _, m := range listResp.Messages {
			ids = append(ids, m.ID)
			if len(ids) >= maxInitialSync {
				break
			}
		}

		pageToken = listResp.NextPageToken
		if pageToken == "" {
			break
		}
	}

	emails, err := fetchMessageBatch(ctx, client, userID, ids)
	if err != nil {
		return nil, nil, "", err
	}

	// Get current historyId for future incremental syncs
	var profile gmailProfileResponse
	if err := client.GetJSON(ctx, gmailBase+"/profile", &profile); err != nil {
		slog.Warn("gmail profile fetch failed", "error", err)
	}

	return &GmailSyncResult{Synced: len(emails), Mode: "full"}, emails, profile.HistoryID, nil
}

func fetchMessageBatch(ctx context.Context, client *OAuthClient, userID string, ids []string) ([]ParsedEmail, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	workers := messageWorkers
	if len(ids) < workers {
		workers = len(ids)
	}

	type fetchResult struct {
		email ParsedEmail
		err   error
	}

	jobs := make(chan string)
	results := make(chan fetchResult, len(ids))
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				u := fmt.Sprintf("%s/messages/%s?format=metadata&metadataHeaders=From&metadataHeaders=To&metadataHeaders=Cc&metadataHeaders=Bcc&metadataHeaders=Subject", gmailBase, id)
				var msg gmailMessage
				if err := client.GetJSON(ctx, u, &msg); err != nil {
					results <- fetchResult{err: err}
					continue
				}
				results <- fetchResult{email: parseGmailMessage(msg, userID)}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, id := range ids {
			select {
			case <-ctx.Done():
				return
			case jobs <- id:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var emails []ParsedEmail
	var firstErr error
	var failed int

	for result := range results {
		if result.err != nil {
			failed++
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		emails = append(emails, result.email)
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(emails) == 0 && firstErr != nil {
		return nil, fmt.Errorf("message metadata fetch failed for all %d messages: %w", len(ids), firstErr)
	}
	if failed > 0 {
		slog.Warn("gmail message metadata partial failure", "requested", len(ids), "fetched", len(emails), "failed", failed, "firstError", firstErr)
	}

	return emails, nil
}

func parseGmailMessage(msg gmailMessage, userID string) ParsedEmail {
	headers := msg.Payload.Headers
	getHeader := func(name string) string {
		for _, h := range headers {
			if strings.EqualFold(h.Name, name) {
				return h.Value
			}
		}
		return ""
	}

	labelIDs := msg.LabelIDs
	from := getHeader("From")
	to := getHeader("To")
	subject := getHeader("Subject")
	if subject == "" {
		subject = "(geen onderwerp)"
	}

	ontvangen, _ := strconv.ParseInt(msg.InternalDate, 10, 64)
	datum := time.UnixMilli(ontvangen).Format("2006-01-02")

	var attachCount int
	for _, p := range msg.Payload.Parts {
		if p.Filename != "" {
			attachCount++
		}
	}

	searchText := strings.Join([]string{subject, msg.Snippet, from, to}, " ")
	searchText = truncateRunes(searchText, 500)

	hasLabel := func(l string) bool {
		for _, id := range labelIDs {
			if id == l {
				return true
			}
		}
		return false
	}

	categorie := "primary"
	switch {
	case hasLabel("CATEGORY_SOCIAL"):
		categorie = "social"
	case hasLabel("CATEGORY_PROMOTIONS"):
		categorie = "promotions"
	case hasLabel("CATEGORY_UPDATES"):
		categorie = "updates"
	case hasLabel("CATEGORY_FORUMS"):
		categorie = "forums"
	}

	filteredLabels := make([]string, 0, len(labelIDs))
	for _, l := range labelIDs {
		if !strings.HasPrefix(l, "CATEGORY_") {
			filteredLabels = append(filteredLabels, l)
		}
	}

	return ParsedEmail{
		UserID:        userID,
		GmailID:       msg.ID,
		ThreadID:      msg.ThreadID,
		From:          from,
		To:            to,
		CC:            getHeader("Cc"),
		BCC:           getHeader("Bcc"),
		Subject:       subject,
		Snippet:       msg.Snippet,
		Datum:         datum,
		Ontvangen:     ontvangen,
		IsGelezen:     !hasLabel("UNREAD"),
		IsSter:        hasLabel("STARRED"),
		IsVerwijderd:  hasLabel("TRASH"),
		IsDraft:       hasLabel("DRAFT"),
		LabelIDs:      filteredLabels,
		Categorie:     categorie,
		HeeftBijlagen: attachCount > 0,
		BijlagenCount: attachCount,
		SearchText:    searchText,
		SyncedAt:      time.Now().UTC().Format(time.RFC3339),
	}
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}

	var b strings.Builder
	count := 0
	for _, r := range s {
		if count >= max {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
