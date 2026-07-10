package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Business-context errors are deliberately coarse-grained at the API boundary:
// a context owned by another user is indistinguishable from a missing context.
// That prevents UUID probing while still letting callers distinguish malformed
// input from a transient database failure.
var (
	ErrInvalidBusinessContext  = errors.New("invalid business context")
	ErrBusinessContextNotFound = errors.New("business context not found")
)

type businessContextSpec struct {
	table       string
	titleColumn string
	requiresID  bool
}

var businessContextSpecs = map[string]businessContextSpec{
	"contact":                {table: "contacts", titleColumn: "display_name", requiresID: true},
	"laventecare":            {requiresID: false},
	"laventecare_company":    {table: "lc_companies", titleColumn: "naam", requiresID: true},
	"laventecare_lead":       {table: "lc_leads", titleColumn: "titel", requiresID: true},
	"laventecare_project":    {table: "lc_projects", titleColumn: "naam", requiresID: true},
	"laventecare_workstream": {table: "lc_workstreams", titleColumn: "titel", requiresID: true},
}

type businessContextQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// lockBusinessContextGraph serializes context-edge writes with contact mirror/
// merge lifecycle operations for one user. Taking this before any note/contact
// row lock gives every writer the same lock order and avoids contact↔note
// deadlocks during concurrent rename/delete and note edits.
func lockBusinessContextGraph(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, userID)
	return err
}

// normalizeBusinessContext validates the context triplet as one value and
// resolves its title from the owned source row. The caller-provided title is
// never trusted for persisted contexts.
func normalizeBusinessContext(
	ctx context.Context,
	q businessContextQuerier,
	userID string,
	contextType, contextID, contextTitle *string,
) (*string, *string, *string, error) {
	rawType := trimOptionalString(contextType)
	rawID := trimOptionalString(contextID)
	rawTitle := trimOptionalString(contextTitle)

	if rawType == "" {
		if rawID != "" || rawTitle != "" {
			return nil, nil, nil, fmt.Errorf("%w: type ontbreekt", ErrInvalidBusinessContext)
		}
		return nil, nil, nil, nil
	}

	normalizedType := strings.ToLower(rawType)
	spec, ok := businessContextSpecs[normalizedType]
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: onbekend type %q", ErrInvalidBusinessContext, rawType)
	}

	if !spec.requiresID {
		if rawID != "" {
			return nil, nil, nil, fmt.Errorf("%w: %s accepteert geen id", ErrInvalidBusinessContext, normalizedType)
		}
		title := "LaventeCare"
		return &normalizedType, nil, &title, nil
	}
	if rawID == "" {
		return nil, nil, nil, fmt.Errorf("%w: id ontbreekt voor %s", ErrInvalidBusinessContext, normalizedType)
	}

	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: ongeldig context-id", ErrInvalidBusinessContext)
	}

	// Table and column come exclusively from the fixed allowlist above. FOR
	// SHARE keeps the referenced row stable until the surrounding create/update
	// transaction commits.
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE user_id = $1 AND id = $2 FOR SHARE`, spec.titleColumn, spec.table)
	var resolvedTitle string
	if err := q.QueryRow(ctx, query, userID, id).Scan(&resolvedTitle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, ErrBusinessContextNotFound
		}
		return nil, nil, nil, err
	}
	resolvedTitle = strings.TrimSpace(resolvedTitle)
	if resolvedTitle == "" {
		return nil, nil, nil, fmt.Errorf("%w: context heeft geen naam", ErrInvalidBusinessContext)
	}
	normalizedID := id.String()
	return &normalizedType, &normalizedID, &resolvedTitle, nil
}

func trimOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func hasBusinessContextFields(fields map[string]any) bool {
	for _, key := range []string{"business_context_type", "business_context_id", "business_context_title"} {
		if _, ok := fields[key]; ok {
			return true
		}
	}
	return false
}

// mergedBusinessContext starts from the current triplet and applies a partial
// patch atomically. Changing the type without also supplying an id deliberately
// drops the old id, so an id from (say) a contact can never be reinterpreted as
// a LaventeCare project id by accident.
func mergedBusinessContext(currentType, currentID, currentTitle *string, fields map[string]any) (*string, *string, *string) {
	typ := trimOptionalString(currentType)
	id := trimOptionalString(currentID)
	title := trimOptionalString(currentTitle)

	nextType, typeTouched := businessContextField(fields, "business_context_type")
	nextID, idTouched := businessContextField(fields, "business_context_id")
	nextTitle, titleTouched := businessContextField(fields, "business_context_title")
	if typeTouched {
		if !strings.EqualFold(strings.TrimSpace(nextType), typ) && !idTouched {
			id = ""
		}
		typ = strings.TrimSpace(nextType)
	}
	if idTouched {
		id = strings.TrimSpace(nextID)
	}
	if titleTouched {
		title = strings.TrimSpace(nextTitle)
	}
	if typ == "" {
		id = ""
		title = ""
	}
	return optionalTrimmedString(typ), optionalTrimmedString(id), optionalTrimmedString(title)
}

func businessContextField(fields map[string]any, key string) (string, bool) {
	raw, ok := fields[key]
	if !ok {
		return "", false
	}
	value, valid := optionalStringValue(raw)
	if !valid {
		return "", true
	}
	return value, true
}

func optionalTrimmedString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
