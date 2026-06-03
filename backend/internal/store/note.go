package store

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type NoteStore struct{ db *DB }

func NewNoteStore(db *DB) *NoteStore { return &NoteStore{db: db} }

var wikiLinkPattern = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)

const noteCols = `id, user_id, titel, inhoud, tags, kleur, is_pinned, is_archived,
	deadline, linked_event_id, prioriteit, triage_flag, aangemaakt, gewijzigd`

func scanNote(row pgx.Row) (model.Note, error) {
	var n model.Note
	err := row.Scan(&n.ID, &n.UserID, &n.Titel, &n.Inhoud, &n.Tags, &n.Kleur,
		&n.IsPinned, &n.IsArchived, &n.Deadline, &n.LinkedEventID, &n.Prioriteit,
		&n.TriageFlag, &n.Aangemaakt, &n.Gewijzigd)
	if n.Tags == nil {
		n.Tags = []string{}
	}
	return n, err
}

// List returns all notes for a user (sorted by pinned then newest).
func (s *NoteStore) List(ctx context.Context, userID string) ([]model.Note, error) {
	rows, err := s.db.Pool.Query(ctx, fmt.Sprintf(`
		SELECT %s FROM notes WHERE user_id = $1
		ORDER BY is_pinned DESC, gewijzigd DESC
	`, noteCols), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Note, error) {
		var n model.Note
		err := row.Scan(&n.ID, &n.UserID, &n.Titel, &n.Inhoud, &n.Tags, &n.Kleur,
			&n.IsPinned, &n.IsArchived, &n.Deadline, &n.LinkedEventID, &n.Prioriteit,
			&n.TriageFlag, &n.Aangemaakt, &n.Gewijzigd)
		if n.Tags == nil {
			n.Tags = []string{}
		}
		return n, err
	})
}

// Get returns a single note by ID.
func (s *NoteStore) Get(ctx context.Context, id uuid.UUID) (model.Note, error) {
	return scanNote(s.db.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s FROM notes WHERE id = $1
	`, noteCols), id))
}

// Create inserts a new note.
func (s *NoteStore) Create(ctx context.Context, userID string, n model.Note) (model.Note, error) {
	n.ID = uuid.New()
	n.UserID = userID
	now := time.Now()
	n.Aangemaakt = now
	n.Gewijzigd = now
	if n.Tags == nil {
		n.Tags = []string{}
	}

	created, err := scanNote(s.db.Pool.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO notes (id, user_id, titel, inhoud, tags, kleur, is_pinned, is_archived,
			deadline, linked_event_id, prioriteit, triage_flag, aangemaakt, gewijzigd)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING %s
	`, noteCols),
		n.ID, n.UserID, n.Titel, n.Inhoud, n.Tags, n.Kleur,
		n.IsPinned, n.IsArchived, n.Deadline, n.LinkedEventID, n.Prioriteit,
		n.TriageFlag, n.Aangemaakt, n.Gewijzigd,
	))
	if err != nil {
		return created, err
	}
	if err := s.SyncLinksFromContent(ctx, userID, created.ID, created.Inhoud); err != nil {
		return created, err
	}
	return created, nil
}

// Update patches a note with the given fields.
func (s *NoteStore) Update(ctx context.Context, id uuid.UUID, fields map[string]any) (model.Note, error) {
	sets := []string{}
	args := []any{}
	argIdx := 1

	for col, val := range fields {
		sets = append(sets, col+" = $"+strconv.Itoa(argIdx))
		args = append(args, val)
		argIdx++
	}
	sets = append(sets, "gewijzigd = $"+strconv.Itoa(argIdx))
	args = append(args, time.Now())
	argIdx++

	args = append(args, id)

	q := fmt.Sprintf(`UPDATE notes SET %s WHERE id = $%d RETURNING %s`,
		strings.Join(sets, ", "), argIdx, noteCols)

	updated, err := scanNote(s.db.Pool.QueryRow(ctx, q, args...))
	if err != nil {
		return updated, err
	}
	if _, changed := fields["inhoud"]; changed {
		if err := s.SyncLinksFromContent(ctx, updated.UserID, updated.ID, updated.Inhoud); err != nil {
			return updated, err
		}
	}
	return updated, nil
}

// Delete permanently removes a note.
func (s *NoteStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM notes WHERE id = $1`, id)
	return err
}

// Search performs full-text search across notes.
func (s *NoteStore) Search(ctx context.Context, userID, query string, limit int) ([]model.Note, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Pool.Query(ctx, fmt.Sprintf(`
		SELECT %s FROM notes
		WHERE user_id = $1
		  AND to_tsvector('dutch', COALESCE(titel,'') || ' ' || inhoud) @@ plainto_tsquery('dutch', $2)
		ORDER BY gewijzigd DESC
		LIMIT $3
	`, noteCols), userID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Note, error) {
		var n model.Note
		err := row.Scan(&n.ID, &n.UserID, &n.Titel, &n.Inhoud, &n.Tags, &n.Kleur,
			&n.IsPinned, &n.IsArchived, &n.Deadline, &n.LinkedEventID, &n.Prioriteit,
			&n.TriageFlag, &n.Aangemaakt, &n.Gewijzigd)
		if n.Tags == nil {
			n.Tags = []string{}
		}
		return n, err
	})
}

// ─── Note Links ─────────────────────────────────────────────────────────────

// GetLinks returns all links for a note (both directions).
func (s *NoteStore) GetLinks(ctx context.Context, noteID uuid.UUID) ([]model.NoteLink, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, source_id, target_id, aangemaakt
		FROM note_links WHERE source_id = $1 OR target_id = $1
	`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.NoteLink, error) {
		var l model.NoteLink
		err := row.Scan(&l.ID, &l.UserID, &l.SourceID, &l.TargetID, &l.Aangemaakt)
		return l, err
	})
}

// AddLink creates a bi-directional link between two notes.
func (s *NoteStore) AddLink(ctx context.Context, userID string, sourceID, targetID uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO note_links (user_id, source_id, target_id)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
	`, userID, sourceID, targetID)
	return err
}

// RemoveLink deletes a link between two notes.
func (s *NoteStore) RemoveLink(ctx context.Context, sourceID, targetID uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `
		DELETE FROM note_links
		WHERE (source_id = $1 AND target_id = $2) OR (source_id = $2 AND target_id = $1)
	`, sourceID, targetID)
	return err
}

// SyncLinksFromContent replaces outgoing wiki links for a note based on [[Title]] references.
func (s *NoteStore) SyncLinksFromContent(ctx context.Context, userID string, sourceID uuid.UUID, content string) error {
	titles := extractWikiLinkTitles(content)

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM note_links WHERE user_id = $1 AND source_id = $2`, userID, sourceID); err != nil {
		return err
	}

	for _, title := range titles {
		var targetID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id
			FROM notes
			WHERE user_id = $1
			  AND id <> $2
			  AND lower(COALESCE(NULLIF(titel, ''), left(split_part(inhoud, E'\n', 1), 50))) = lower($3)
			ORDER BY gewijzigd DESC
			LIMIT 1
		`, userID, sourceID, title).Scan(&targetID)
		if err == pgx.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO note_links (user_id, source_id, target_id)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
		`, userID, sourceID, targetID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func extractWikiLinkTitles(content string) []string {
	matches := wikiLinkPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]bool{}
	titles := make([]string, 0, len(matches))
	for _, match := range matches {
		title := strings.TrimSpace(match[1])
		if title == "" {
			continue
		}
		key := strings.ToLower(title)
		if seen[key] {
			continue
		}
		seen[key] = true
		titles = append(titles, title)
	}
	return titles
}

// AllTags returns all unique tags across a user's notes.
func (s *NoteStore) AllTags(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT unnest(tags) AS tag FROM notes
		WHERE user_id = $1 AND NOT is_archived
		ORDER BY tag
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, rows.Err()
}

// GetBacklinks returns basic info (id, titel) of notes that link to this note.
func (s *NoteStore) GetBacklinks(ctx context.Context, noteID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT n.id, n.titel
		FROM notes n
		JOIN note_links nl ON n.id = nl.source_id
		WHERE nl.target_id = $1
	`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var titel *string
		if err := rows.Scan(&id, &titel); err != nil {
			return nil, err
		}

		t := ""
		if titel != nil {
			t = *titel
		} else {
			t = "Naamloze notitie"
		}

		links = append(links, map[string]any{
			"id":    id.String(),
			"titel": t,
		})
	}
	if links == nil {
		links = []map[string]any{}
	}
	return links, nil
}
