package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
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

var ErrNoteNotFound = pgx.ErrNoRows
var ErrLinkedEventNotFound = errors.New("linked event not found")

// ErrNoteConflict is returned when an update carries an optimistic-concurrency
// token (expected_gewijzigd) that no longer matches the stored note — i.e. the
// note was changed by another write since the caller last read it.
var ErrNoteConflict = errors.New("note modified by another write")

// normalizeNoteTags trims, drops blanks, and de-duplicates tags
// (case-insensitive, first spelling wins). Applied centrally on every write so
// no caller (handler, AI executor, restore) can store duplicate/empty tags.
func normalizeNoteTags(tags []string) []string {
	if tags == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

const noteCols = `id, user_id, titel, inhoud, tags, kleur, is_pinned, is_archived, is_completed, completed_at,
	deadline, linked_event_id, prioriteit, symbol, business_context_type, business_context_id,
	business_context_title, triage_flag, aangemaakt, gewijzigd`

const noteRevisionCols = `id, note_id, user_id, titel, inhoud, tags, kleur,
	deadline, linked_event_id, prioriteit, symbol, business_context_type, business_context_id,
	business_context_title, aangemaakt`

func scanNote(row pgx.Row) (model.Note, error) {
	var n model.Note
	err := row.Scan(&n.ID, &n.UserID, &n.Titel, &n.Inhoud, &n.Tags, &n.Kleur,
		&n.IsPinned, &n.IsArchived, &n.IsCompleted, &n.CompletedAt, &n.Deadline, &n.LinkedEventID, &n.Prioriteit,
		&n.Symbol, &n.BusinessContextType, &n.BusinessContextID, &n.BusinessContextTitle, &n.TriageFlag, &n.Aangemaakt, &n.Gewijzigd)
	if n.Tags == nil {
		n.Tags = []string{}
	}
	return n, err
}

func scanNoteRevision(row pgx.Row) (model.NoteRevision, error) {
	var r model.NoteRevision
	err := row.Scan(&r.ID, &r.NoteID, &r.UserID, &r.Titel, &r.Inhoud, &r.Tags,
		&r.Kleur, &r.Deadline, &r.LinkedEventID, &r.Prioriteit, &r.Symbol,
		&r.BusinessContextType, &r.BusinessContextID, &r.BusinessContextTitle, &r.Aangemaakt)
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return r, err
}

// List returns all notes for a user (sorted by pinned then newest).
func (s *NoteStore) List(ctx context.Context, userID string) ([]model.Note, error) {
	return s.ListPaged(ctx, userID, 0, 0, false)
}

// NoteListOptions controls pagination/payload size and optionally restricts the
// result to an owned business context. ContextType by itself filters all notes
// of that type; ContextID additionally validates ownership and matches one
// specific object.
type NoteListOptions struct {
	Limit       int
	Offset      int
	Summary     bool
	ContextType string
	ContextID   string
}

// ListPaged returns notes for a user with optional pagination and a summary
// mode that skips the (potentially large) inhoud column. limit/offset of 0 keep
// the historical unlimited/full behaviour; summary=true returns inhoud as "".
func (s *NoteStore) ListPaged(ctx context.Context, userID string, limit, offset int, summary bool) ([]model.Note, error) {
	return s.ListWithOptions(ctx, userID, NoteListOptions{Limit: limit, Offset: offset, Summary: summary})
}

// ListWithOptions is the context-aware list implementation. ListPaged remains
// as a backward-compatible wrapper for kiosk, Telegram and existing callers.
func (s *NoteStore) ListWithOptions(ctx context.Context, userID string, opts NoteListOptions) ([]model.Note, error) {
	cols := noteCols
	if opts.Summary {
		// Same column list/order as noteCols, but with inhoud blanked (payloads
		// stay small) and a trailing left(inhoud,80) preview so untitled notes
		// still render a meaningful line on the kiosk instead of "Naamloze notitie".
		cols = strings.Replace(noteCols, "inhoud", "'' AS inhoud", 1) + ", left(inhoud, 80) AS preview"
	}
	// Summary mode orders by deadline-urgency first (overdue/soonest deadlines
	// win, then newest) so the kiosk's newest-N window doesn't truncate away the
	// overdue heavyweights; full mode keeps the pinned-then-newest ordering.
	order := "is_pinned DESC, gewijzigd DESC"
	if opts.Summary {
		order = "is_pinned DESC, (deadline IS NULL), deadline ASC, gewijzigd DESC"
	}
	q := fmt.Sprintf(`SELECT %s FROM notes WHERE user_id = $1`, cols)
	args := []any{userID}

	contextType := strings.ToLower(strings.TrimSpace(opts.ContextType))
	contextID := strings.TrimSpace(opts.ContextID)
	if contextType == "" && contextID != "" {
		return nil, fmt.Errorf("%w: contextType ontbreekt", ErrInvalidBusinessContext)
	}
	if contextType != "" {
		spec, ok := businessContextSpecs[contextType]
		if !ok {
			return nil, fmt.Errorf("%w: onbekend type %q", ErrInvalidBusinessContext, contextType)
		}
		args = append(args, contextType)
		q += fmt.Sprintf(" AND business_context_type = $%d", len(args))
		if contextID != "" {
			if !spec.requiresID {
				return nil, fmt.Errorf("%w: %s accepteert geen id", ErrInvalidBusinessContext, contextType)
			}
			normalizedType, normalizedID, _, err := normalizeBusinessContext(ctx, s.db.Pool, userID, &contextType, &contextID, nil)
			if err != nil {
				return nil, err
			}
			// normalizedType is resolved from the same allowlist but assign it for
			// completeness if future aliases are introduced.
			args[len(args)-1] = *normalizedType
			args = append(args, *normalizedID)
			q += fmt.Sprintf(" AND business_context_id = $%d", len(args))
		}
	}
	q += fmt.Sprintf(" ORDER BY %s", order)
	if opts.Limit > 0 {
		args = append(args, opts.Limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if opts.Offset > 0 {
		args = append(args, opts.Offset)
		q += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Note, error) {
		var n model.Note
		dest := []any{&n.ID, &n.UserID, &n.Titel, &n.Inhoud, &n.Tags, &n.Kleur,
			&n.IsPinned, &n.IsArchived, &n.IsCompleted, &n.CompletedAt, &n.Deadline, &n.LinkedEventID, &n.Prioriteit,
			&n.Symbol, &n.BusinessContextType, &n.BusinessContextID, &n.BusinessContextTitle, &n.TriageFlag, &n.Aangemaakt, &n.Gewijzigd}
		if opts.Summary {
			dest = append(dest, &n.Preview)
		}
		err := row.Scan(dest...)
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

// GetForUser returns a note only when it belongs to the given user.
func (s *NoteStore) GetForUser(ctx context.Context, userID string, id uuid.UUID) (model.Note, error) {
	return scanNote(s.db.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s FROM notes WHERE id = $1 AND user_id = $2
	`, noteCols), id, userID))
}

// Create inserts a new note.
func (s *NoteStore) Create(ctx context.Context, userID string, n model.Note) (model.Note, error) {
	n.ID = uuid.New()
	n.UserID = userID
	now := time.Now()
	n.Aangemaakt = now
	n.Gewijzigd = now
	n.Tags = normalizeNoteTags(n.Tags)
	if n.Tags == nil {
		n.Tags = []string{}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.Note{}, err
	}
	defer tx.Rollback(ctx)

	if n.BusinessContextType != nil || n.BusinessContextID != nil || n.BusinessContextTitle != nil {
		if err := lockBusinessContextGraph(ctx, tx, userID); err != nil {
			return model.Note{}, err
		}
	}
	n.BusinessContextType, n.BusinessContextID, n.BusinessContextTitle, err = normalizeBusinessContext(
		ctx, tx, userID, n.BusinessContextType, n.BusinessContextID, n.BusinessContextTitle,
	)
	if err != nil {
		return model.Note{}, err
	}

	created, err := scanNote(tx.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO notes (id, user_id, titel, inhoud, tags, kleur, is_pinned, is_archived, is_completed, completed_at,
			deadline, linked_event_id, prioriteit, symbol, business_context_type, business_context_id,
			business_context_title, triage_flag, aangemaakt, gewijzigd)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		RETURNING %s
	`, noteCols),
		n.ID, n.UserID, n.Titel, n.Inhoud, n.Tags, n.Kleur,
		n.IsPinned, n.IsArchived, n.IsCompleted, n.CompletedAt, n.Deadline, n.LinkedEventID, n.Prioriteit,
		n.Symbol, n.BusinessContextType, n.BusinessContextID, n.BusinessContextTitle, n.TriageFlag, n.Aangemaakt, n.Gewijzigd,
	))
	if err != nil {
		return created, err
	}
	if err := tx.Commit(ctx); err != nil {
		return created, err
	}
	if err := s.SyncLinksFromContent(ctx, userID, created.ID, created.Inhoud); err != nil {
		slog.Warn("note link-sync failed after create", "note", created.ID, "error", err)
	}
	return created, nil
}

// NormalizeLinkedEventID trims and validates note agenda links against known
// personal events or roster schedule entries. Legacy pending placeholders are
// treated as no link so they do not keep polluting the note graph.
func (s *NoteStore) NormalizeLinkedEventID(ctx context.Context, userID string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	eventID := strings.TrimSpace(*value)
	if eventID == "" || isPendingLinkedEventID(eventID) {
		return nil, nil
	}

	var exists bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM personal_events WHERE user_id = $1 AND event_id = $2
			UNION ALL
			SELECT 1 FROM schedule WHERE user_id = $1 AND event_id = $2
		)
	`, userID, eventID).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrLinkedEventNotFound
	}
	return &eventID, nil
}

func isPendingLinkedEventID(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.Contains(value, "::pending::") || strings.HasPrefix(value, "pending:")
}

// Update patches a note with the given fields.
func (s *NoteStore) Update(ctx context.Context, id uuid.UUID, fields map[string]any) (model.Note, error) {
	return s.update(ctx, id, "", fields)
}

// UpdateForUser patches a note only when it belongs to the given user.
func (s *NoteStore) UpdateForUser(ctx context.Context, userID string, id uuid.UUID, fields map[string]any) (model.Note, error) {
	return s.update(ctx, id, userID, fields)
}

func (s *NoteStore) update(ctx context.Context, id uuid.UUID, userID string, fields map[string]any) (model.Note, error) {
	// Optional optimistic-concurrency token: when the caller passes the
	// gewijzigd timestamp it last saw, a concurrent write that already bumped
	// gewijzigd makes this update fail loudly (ErrNoteConflict) instead of
	// silently clobbering the other change. Absent token = no guard (back-compat).
	var expected *time.Time
	if raw, ok := fields["expected_gewijzigd"]; ok {
		delete(fields, "expected_gewijzigd")
		if t, ok := optionalTimeValue(raw); ok {
			expected = t
		}
	}
	// Normalize tags centrally so every write path stores trimmed/de-duped tags.
	if raw, ok := fields["tags"]; ok {
		if tags, ok := tagsValue(raw); ok {
			fields["tags"] = normalizeNoteTags(tags)
		}
	}
	if len(fields) == 0 {
		// Only a token was sent — nothing to write; return the note as-is.
		if userID != "" {
			return s.GetForUser(ctx, userID, id)
		}
		return s.Get(ctx, id)
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.Note{}, err
	}
	defer tx.Rollback(ctx)
	if hasBusinessContextFields(fields) {
		lockUserID := userID
		if lockUserID == "" {
			if err := tx.QueryRow(ctx, `SELECT user_id FROM notes WHERE id = $1`, id).Scan(&lockUserID); err != nil {
				return model.Note{}, err
			}
		}
		if err := lockBusinessContextGraph(ctx, tx, lockUserID); err != nil {
			return model.Note{}, err
		}
	}

	if expected != nil || shouldCheckNoteRevision(fields) {
		selectWhere := "id = $1"
		selectArgs := []any{id}
		if userID != "" {
			selectWhere += " AND user_id = $2"
			selectArgs = append(selectArgs, userID)
		}
		current, err := scanNote(tx.QueryRow(ctx, fmt.Sprintf(`
			SELECT %s FROM notes WHERE %s FOR UPDATE
		`, noteCols, selectWhere), selectArgs...))
		if err != nil {
			return current, err
		}
		if expected != nil && !sameOptionalTime(&current.Gewijzigd, expected) {
			return current, ErrNoteConflict
		}
		if hasBusinessContextFields(fields) {
			requestedType, requestedID, requestedTitle := mergedBusinessContext(
				current.BusinessContextType, current.BusinessContextID, current.BusinessContextTitle, fields,
			)
			resolvedType, resolvedID, resolvedTitle, err := normalizeBusinessContext(
				ctx, tx, current.UserID, requestedType, requestedID, requestedTitle,
			)
			if err != nil {
				return current, err
			}
			// A context is one value even though it occupies three columns. Always
			// write the complete canonical triplet so partial patches cannot leave
			// a mixed type/id/title behind.
			fields["business_context_type"] = resolvedType
			fields["business_context_id"] = resolvedID
			fields["business_context_title"] = resolvedTitle
		}
		if shouldCheckNoteRevision(fields) && noteRevisionFieldsChanged(current, fields) {
			if err := insertNoteRevision(ctx, tx, current); err != nil {
				return model.Note{}, err
			}
		}
	}

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
	idArg := argIdx
	argIdx++

	where := fmt.Sprintf("id = $%d", idArg)
	if userID != "" {
		args = append(args, userID)
		where += fmt.Sprintf(" AND user_id = $%d", argIdx)
	}

	q := fmt.Sprintf(`UPDATE notes SET %s WHERE %s RETURNING %s`,
		strings.Join(sets, ", "), where, noteCols)

	updated, err := scanNote(tx.QueryRow(ctx, q, args...))
	if err != nil {
		return updated, err
	}
	if err := tx.Commit(ctx); err != nil {
		return updated, err
	}
	if _, changed := fields["inhoud"]; changed {
		// The note is already committed; backlink sync is best-effort and
		// self-heals on the next edit, so its failure must not 500 a saved write.
		if err := s.SyncLinksFromContent(ctx, updated.UserID, updated.ID, updated.Inhoud); err != nil {
			slog.Warn("note link-sync failed after commit", "note", updated.ID, "error", err)
		}
	}
	return updated, nil
}

func shouldCheckNoteRevision(fields map[string]any) bool {
	for _, key := range []string{"titel", "inhoud", "tags", "kleur", "deadline", "linked_event_id", "prioriteit", "symbol", "business_context_type", "business_context_id", "business_context_title"} {
		if _, ok := fields[key]; ok {
			return true
		}
	}
	return false
}

func noteRevisionFieldsChanged(n model.Note, fields map[string]any) bool {
	for col, val := range fields {
		switch col {
		case "titel":
			next, ok := optionalStringValue(val)
			if ok && normalizedOptionalString(n.Titel) != next {
				return true
			}
		case "inhoud":
			next, ok := stringValue(val)
			if ok && n.Inhoud != next {
				return true
			}
		case "tags":
			next, ok := tagsValue(val)
			if ok && tagsRevisionKey(n.Tags) != tagsRevisionKey(next) {
				return true
			}
		case "kleur":
			next, ok := optionalStringValue(val)
			if ok && normalizedOptionalString(n.Kleur) != next {
				return true
			}
		case "deadline":
			next, ok := optionalTimeValue(val)
			if ok && !sameOptionalTime(n.Deadline, next) {
				return true
			}
		case "linked_event_id":
			next, ok := optionalStringValue(val)
			if ok && normalizedOptionalString(n.LinkedEventID) != next {
				return true
			}
		case "prioriteit":
			next, ok := optionalStringValue(val)
			if ok && normalizedOptionalString(n.Prioriteit) != next {
				return true
			}
		case "symbol":
			next, ok := optionalStringValue(val)
			if ok && normalizedOptionalString(n.Symbol) != next {
				return true
			}
		case "business_context_type":
			next, ok := optionalStringValue(val)
			if ok && normalizedOptionalString(n.BusinessContextType) != next {
				return true
			}
		case "business_context_id":
			next, ok := optionalStringValue(val)
			if ok && normalizedOptionalString(n.BusinessContextID) != next {
				return true
			}
		case "business_context_title":
			next, ok := optionalStringValue(val)
			if ok && normalizedOptionalString(n.BusinessContextTitle) != next {
				return true
			}
		}
	}
	return false
}

func optionalStringValue(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", true
	case string:
		return v, true
	case *string:
		if v == nil {
			return "", true
		}
		return *v, true
	default:
		return "", false
	}
}

func stringValue(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case *string:
		if v == nil {
			return "", true
		}
		return *v, true
	default:
		return "", false
	}
}

func tagsValue(value any) ([]string, bool) {
	switch v := value.(type) {
	case nil:
		return []string{}, true
	case []string:
		return v, true
	case *[]string:
		if v == nil {
			return []string{}, true
		}
		return *v, true
	default:
		return nil, false
	}
}

func optionalTimeValue(value any) (*time.Time, bool) {
	switch v := value.(type) {
	case nil:
		return nil, true
	case time.Time:
		return &v, true
	case *time.Time:
		return v, true
	default:
		return nil, false
	}
}

func normalizedOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func tagsRevisionKey(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag != "" {
			normalized = append(normalized, tag)
		}
	}
	sort.Strings(normalized)
	return strings.Join(normalized, "\x00")
}

func sameOptionalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.UTC().Equal(b.UTC())
}

func insertNoteRevision(ctx context.Context, tx pgx.Tx, n model.Note) error {
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO note_revisions (note_id, user_id, titel, inhoud, tags, kleur,
			deadline, linked_event_id, prioriteit, symbol, business_context_type, business_context_id,
			business_context_title)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`, n.ID, n.UserID, n.Titel, n.Inhoud, tags, n.Kleur,
		n.Deadline, n.LinkedEventID, n.Prioriteit, n.Symbol, n.BusinessContextType, n.BusinessContextID,
		n.BusinessContextTitle); err != nil {
		return err
	}
	// Cap revision history per note so it can't grow unbounded over a note's life.
	_, err := tx.Exec(ctx, `
		DELETE FROM note_revisions
		 WHERE note_id = $1
		   AND id NOT IN (
			   SELECT id FROM note_revisions
			    WHERE note_id = $1
			    ORDER BY aangemaakt DESC
			    LIMIT $2
		   )
	`, n.ID, noteRevisionRetention)
	return err
}

// noteRevisionRetention caps how many saved versions are kept per note.
const noteRevisionRetention = 50

// ListRevisions returns recent saved versions for a note.
func (s *NoteStore) ListRevisions(ctx context.Context, userID string, noteID uuid.UUID, limit int) ([]model.NoteRevision, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.Pool.Query(ctx, fmt.Sprintf(`
		SELECT %s
		FROM note_revisions
		WHERE note_id = $1 AND user_id = $2
		ORDER BY aangemaakt DESC
		LIMIT $3
	`, noteRevisionCols), noteID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	revisions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.NoteRevision, error) {
		return scanNoteRevision(row)
	})
	if revisions == nil {
		revisions = []model.NoteRevision{}
	}
	return revisions, err
}

// RestoreRevision replaces a note with a previous saved version.
func (s *NoteStore) RestoreRevision(ctx context.Context, userID string, noteID, revisionID uuid.UUID) (model.Note, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.Note{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockBusinessContextGraph(ctx, tx, userID); err != nil {
		return model.Note{}, err
	}

	rev, err := scanNoteRevision(tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM note_revisions
		WHERE id = $1 AND note_id = $2 AND user_id = $3
	`, noteRevisionCols), revisionID, noteID, userID))
	if err != nil {
		return model.Note{}, err
	}

	current, err := scanNote(tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s FROM notes WHERE id = $1 AND user_id = $2
	`, noteCols), noteID, userID))
	if err != nil {
		return current, err
	}
	if err := insertNoteRevision(ctx, tx, current); err != nil {
		return model.Note{}, err
	}

	// Re-resolve historical context before restoring it. Lifecycle operations
	// keep revisions in sync, but this also sanitizes legacy revisions that may
	// still point at a deleted or no-longer-owned object.
	restoredContextType, restoredContextID, restoredContextTitle, contextErr := normalizeBusinessContext(
		ctx, tx, userID, rev.BusinessContextType, rev.BusinessContextID, rev.BusinessContextTitle,
	)
	if contextErr != nil {
		if errors.Is(contextErr, ErrInvalidBusinessContext) || errors.Is(contextErr, ErrBusinessContextNotFound) {
			restoredContextType, restoredContextID, restoredContextTitle = nil, nil, nil
		} else {
			return model.Note{}, contextErr
		}
	}

	updated, err := scanNote(tx.QueryRow(ctx, fmt.Sprintf(`
		UPDATE notes
		   SET titel = $1,
		       inhoud = $2,
		       tags = $3,
		       kleur = $4,
		       deadline = $5,
		       linked_event_id = $6,
		       prioriteit = $7,
		       symbol = $8,
		       business_context_type = $9,
		       business_context_id = $10,
		       business_context_title = $11,
		       gewijzigd = $12
		 WHERE id = $13 AND user_id = $14
		RETURNING %s
	`, noteCols), rev.Titel, rev.Inhoud, rev.Tags, rev.Kleur, rev.Deadline,
		rev.LinkedEventID, rev.Prioriteit, rev.Symbol, restoredContextType, restoredContextID,
		restoredContextTitle, time.Now(), noteID, userID))
	if err != nil {
		return updated, err
	}
	if err := tx.Commit(ctx); err != nil {
		return updated, err
	}
	if err := s.SyncLinksFromContent(ctx, updated.UserID, updated.ID, updated.Inhoud); err != nil {
		slog.Warn("note link-sync failed after restore", "note", updated.ID, "error", err)
	}
	return updated, nil
}

// Delete permanently removes a note.
func (s *NoteStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM notes WHERE id = $1`, id)
	return err
}

// DeleteForUser removes a note only when it belongs to the given user, and
// removes any links touching it so no dangling note_links row survives.
func (s *NoteStore) DeleteForUser(ctx context.Context, userID string, id uuid.UUID) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM note_links WHERE source_id = $1 OR target_id = $1`, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM notes WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return tx.Commit(ctx)
}

// Search performs full-text search across notes.
func (s *NoteStore) Search(ctx context.Context, userID, query string, limit int) ([]model.Note, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Pool.Query(ctx, fmt.Sprintf(`
		WITH q AS (
			SELECT plainto_tsquery('dutch', $2) AS tsq,
			       -- Escape LIKE wildcards (\ %% _) in the user's term so a literal
			       -- '%%' or '_' matches itself instead of acting as a wildcard.
			       '%%' || replace(replace(replace(lower($2), '\', '\\'), '%%', '\%%'), '_', '\_') || '%%' AS likeq
		)
		SELECT %s FROM notes, q
		WHERE user_id = $1
		  AND NOT is_archived
		  AND (
			  to_tsvector('dutch', COALESCE(titel,'') || ' ' || inhoud || ' ' || COALESCE(business_context_title,'')) @@ q.tsq
			  OR lower(COALESCE(prioriteit,'')) LIKE q.likeq ESCAPE '\'
			  OR lower(COALESCE(symbol,'')) LIKE q.likeq ESCAPE '\'
			  OR lower(COALESCE(business_context_title,'')) LIKE q.likeq ESCAPE '\'
			  OR EXISTS (
				  SELECT 1
				  FROM unnest(COALESCE(tags, ARRAY[]::text[])) AS tag
				  WHERE lower(tag) LIKE q.likeq ESCAPE '\'
			  )
		  )
		ORDER BY ts_rank(to_tsvector('dutch', COALESCE(titel,'') || ' ' || inhoud || ' ' || COALESCE(business_context_title,'')), q.tsq) DESC,
		         is_pinned DESC, gewijzigd DESC
		LIMIT $3
	`, noteCols), userID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Note, error) {
		return scanNote(row)
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

// AddLink creates a directed link source → target (idempotent). Backlinks are
// derived by querying the reverse direction, so this intentionally stores one row.
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
			ORDER BY aangemaakt ASC, id ASC
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
	return s.getBacklinks(ctx, "", noteID)
}

// GetBacklinksForUser returns backlinks only when source and target notes belong to the user.
func (s *NoteStore) GetBacklinksForUser(ctx context.Context, userID string, noteID uuid.UUID) ([]map[string]any, error) {
	return s.getBacklinks(ctx, userID, noteID)
}

func (s *NoteStore) getBacklinks(ctx context.Context, userID string, noteID uuid.UUID) ([]map[string]any, error) {
	where := "nl.target_id = $1"
	args := []any{noteID}
	if userID != "" {
		where += " AND nl.user_id = $2 AND n.user_id = $2 AND target.user_id = $2"
		args = append(args, userID)
	}

	rows, err := s.db.Pool.Query(ctx, `
		SELECT n.id, n.titel
		FROM notes n
		JOIN note_links nl ON n.id = nl.source_id
		JOIN notes target ON target.id = nl.target_id
		WHERE `+where, args...)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if links == nil {
		links = []map[string]any{}
	}
	return links, nil
}
