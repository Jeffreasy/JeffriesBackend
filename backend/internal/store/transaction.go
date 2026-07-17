package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type TransactionStore struct{ db *DB }

// ErrInvalidTransactionImport identifies malformed client-side CSV rows. The
// handler maps it to 400; database and infrastructure errors remain 500.
var ErrInvalidTransactionImport = errors.New("ongeldige transactieregel")

func NewTransactionStore(db *DB) *TransactionStore { return &TransactionStore{db: db} }

const trxColumns = `id, user_id, rekening_iban, volgnr, datum::text, bedrag,
	saldo_na_trn, code, tegenrekening_iban, tegenpartij_naam,
	omschrijving, referentie, reden_retour, oorsp_bedrag, oorsp_munt,
	is_interne_overboeking, categorie`

// List returns transactions for a user, optionally filtered by date range.
func (s *TransactionStore) List(ctx context.Context, userID string, fromDate, toDate *string) ([]model.Transaction, error) {
	query := `SELECT ` + trxColumns + ` FROM transactions WHERE user_id = $1`
	args := []any{userID}

	if fromDate != nil {
		args = append(args, *fromDate)
		query += ` AND datum >= $` + pgArgNum(len(args))
	}
	if toDate != nil {
		args = append(args, *toDate)
		query += ` AND datum <= $` + pgArgNum(len(args))
	}
	// LPAD volgnr so ordering is numeric ("9" < "10") at a digit-length rollover.
	query += ` ORDER BY datum DESC, LPAD(LTRIM(volgnr, '0'), 50, '0') DESC`

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanTransaction)
}

// TransactionFilter holds all possible filter parameters.
type TransactionFilter struct {
	ExcludeIntern    bool
	OnlyStorneringen bool
	Code             string
	Iban             string
	Categorie        string
	Richting         string // "in" | "uit"
	MinBedrag        *float64
	MaxBedrag        *float64
	DatumVan         string
	DatumTot         string
	Zoekterm         string
	Limit            int
	Offset           int
}

// ListFiltered returns transactions with full filter support.
func (s *TransactionStore) ListFiltered(ctx context.Context, userID string, f TransactionFilter) ([]model.Transaction, int, error) {
	where := `WHERE user_id = $1`
	args := []any{userID}

	if f.ExcludeIntern {
		where += ` AND is_interne_overboeking = false`
	}
	if f.OnlyStorneringen {
		where += ` AND code = 'st'`
	}
	if f.Code != "" {
		args = append(args, f.Code)
		where += ` AND code = $` + pgArgNum(len(args))
	}
	if f.Iban != "" {
		args = append(args, f.Iban)
		where += ` AND rekening_iban = $` + pgArgNum(len(args))
	}
	if f.Categorie != "" {
		args = append(args, f.Categorie)
		where += ` AND categorie = $` + pgArgNum(len(args))
	}
	switch f.Richting {
	case "in":
		where += ` AND bedrag > 0`
	case "uit":
		where += ` AND bedrag < 0`
	}
	if f.MinBedrag != nil {
		args = append(args, *f.MinBedrag)
		where += ` AND ABS(bedrag) >= $` + pgArgNum(len(args))
	}
	if f.MaxBedrag != nil {
		args = append(args, *f.MaxBedrag)
		where += ` AND ABS(bedrag) <= $` + pgArgNum(len(args))
	}
	if f.DatumVan != "" {
		args = append(args, f.DatumVan)
		where += ` AND datum >= $` + pgArgNum(len(args))
	}
	if f.DatumTot != "" {
		args = append(args, f.DatumTot)
		where += ` AND datum <= $` + pgArgNum(len(args))
	}
	if f.Zoekterm != "" {
		// Escape LIKE wildcards in the user term so "100%" or "a_b" match
		// literally instead of acting as patterns.
		args = append(args, "%"+escapeLikePattern(f.Zoekterm)+"%")
		n := pgArgNum(len(args))
		where += ` AND (LOWER(tegenpartij_naam) LIKE LOWER($` + n + `) ESCAPE '\' OR LOWER(omschrijving) LIKE LOWER($` + n + `) ESCAPE '\')`
	}

	// Count
	var totalCount int
	countQ := `SELECT COUNT(*) FROM transactions ` + where
	if err := s.db.Pool.QueryRow(ctx, countQ, args...).Scan(&totalCount); err != nil {
		return nil, 0, err
	}

	// Paginated data
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	args = append(args, limit)
	limitN := pgArgNum(len(args))
	args = append(args, offset)
	offsetN := pgArgNum(len(args))

	dataQ := `SELECT ` + trxColumns + ` FROM transactions ` + where +
		` ORDER BY datum DESC, LPAD(LTRIM(volgnr, '0'), 50, '0') DESC LIMIT $` + limitN + ` OFFSET $` + offsetN

	rows, err := s.db.Pool.Query(ctx, dataQ, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	txs, err := pgx.CollectRows(rows, scanTransaction)
	return txs, totalCount, err
}

// ImportBatch inserts transactions, skipping duplicates via ON CONFLICT DO NOTHING.
func (s *TransactionStore) ImportBatch(ctx context.Context, userID string, items []model.TransactionImport) (inserted int, err error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	for i, item := range items {
		datum, err := validateTransactionImport(item)
		if err != nil {
			return 0, fmt.Errorf("rij %d: %w", i+1, err)
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO transactions (id, user_id, rekening_iban, volgnr, datum, bedrag,
			    saldo_na_trn, code, tegenrekening_iban, tegenpartij_naam,
			    omschrijving, referentie, reden_retour, oorsp_bedrag, oorsp_munt,
			    is_interne_overboeking, categorie)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			 ON CONFLICT (user_id, rekening_iban, volgnr) DO NOTHING`,
			uuid.New(), userID, strings.TrimSpace(item.RekeningIban), strings.TrimSpace(item.Volgnr), datum,
			item.Bedrag, item.SaldoNaTrn, item.Code, item.TegenrekeningIban,
			item.TegenpartijNaam, item.Omschrijving, item.Referentie,
			item.RedenRetour, item.OorspBedrag, item.OorspMunt,
			item.IsInterneOverboeking, item.Categorie,
		)
		if err != nil {
			return 0, err
		}
		inserted += int(tag.RowsAffected())
	}

	return inserted, tx.Commit(ctx)
}

func validateTransactionImport(item model.TransactionImport) (time.Time, error) {
	if strings.TrimSpace(item.RekeningIban) == "" {
		return time.Time{}, fmt.Errorf("%w: rekeningIban ontbreekt", ErrInvalidTransactionImport)
	}
	if utf8.RuneCountInString(strings.TrimSpace(item.RekeningIban)) > 34 {
		return time.Time{}, fmt.Errorf("%w: rekeningIban is langer dan 34 tekens", ErrInvalidTransactionImport)
	}
	volgnr := strings.TrimSpace(item.Volgnr)
	if volgnr == "" {
		return time.Time{}, fmt.Errorf("%w: volgnr ontbreekt", ErrInvalidTransactionImport)
	}
	if utf8.RuneCountInString(volgnr) > 50 {
		return time.Time{}, fmt.Errorf("%w: volgnr is langer dan 50 tekens", ErrInvalidTransactionImport)
	}
	for _, r := range volgnr {
		if r < '0' || r > '9' {
			return time.Time{}, fmt.Errorf("%w: volgnr moet uitsluitend cijfers bevatten", ErrInvalidTransactionImport)
		}
	}
	datum, err := time.Parse("2006-01-02", item.Datum)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: datum moet een geldige YYYY-MM-DD-datum zijn", ErrInvalidTransactionImport)
	}
	if utf8.RuneCountInString(item.Code) > 10 {
		return time.Time{}, fmt.Errorf("%w: code is langer dan 10 tekens", ErrInvalidTransactionImport)
	}
	if item.TegenrekeningIban != nil && utf8.RuneCountInString(*item.TegenrekeningIban) > 34 {
		return time.Time{}, fmt.Errorf("%w: tegenrekeningIban is langer dan 34 tekens", ErrInvalidTransactionImport)
	}
	if item.TegenpartijNaam != nil && utf8.RuneCountInString(*item.TegenpartijNaam) > 200 {
		return time.Time{}, fmt.Errorf("%w: tegenpartijNaam is langer dan 200 tekens", ErrInvalidTransactionImport)
	}
	if item.Referentie != nil && utf8.RuneCountInString(*item.Referentie) > 200 {
		return time.Time{}, fmt.Errorf("%w: referentie is langer dan 200 tekens", ErrInvalidTransactionImport)
	}
	if item.RedenRetour != nil && utf8.RuneCountInString(*item.RedenRetour) > 200 {
		return time.Time{}, fmt.Errorf("%w: redenRetour is langer dan 200 tekens", ErrInvalidTransactionImport)
	}
	if item.OorspMunt != nil && utf8.RuneCountInString(*item.OorspMunt) > 10 {
		return time.Time{}, fmt.Errorf("%w: oorspMunt is langer dan 10 tekens", ErrInvalidTransactionImport)
	}
	if item.Categorie != nil && utf8.RuneCountInString(*item.Categorie) > 100 {
		return time.Time{}, fmt.Errorf("%w: categorie is langer dan 100 tekens", ErrInvalidTransactionImport)
	}
	return datum, nil
}

// UpdateCategorie sets the categorie for a transaction owned by userID.
// Returns the number of rows affected (0 means not found or not owned).
func (s *TransactionStore) UpdateCategorie(ctx context.Context, userID string, id uuid.UUID, categorie string) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE transactions SET categorie = $3 WHERE id = $1 AND user_id = $2`, id, userID, categorie)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// BulkUpdateCategorie sets one category for multiple transactions owned by userID.
func (s *TransactionStore) BulkUpdateCategorie(ctx context.Context, userID string, ids []uuid.UUID, categorie string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE transactions SET categorie = $3 WHERE id = ANY($1) AND user_id = $2`,
		ids, userID, categorie)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// GetStats returns aggregate stats for a user.
func (s *TransactionStore) GetStats(ctx context.Context, userID string) (map[string]any, error) {
	var totaal int
	var inkomsten, uitgaven, saldo float64
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN bedrag > 0 THEN bedrag ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN bedrag < 0 THEN bedrag ELSE 0 END), 0)
		   FROM transactions WHERE user_id = $1`, userID,
	).Scan(&totaal, &inkomsten, &uitgaven)
	if err != nil {
		return nil, err
	}

	err = s.db.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(saldo_na_trn), 0)
		   FROM (
		       SELECT saldo_na_trn,
		              ROW_NUMBER() OVER (PARTITION BY rekening_iban ORDER BY datum DESC, LPAD(LTRIM(volgnr, '0'), 50, '0') DESC) as rn
		         FROM transactions
		        WHERE user_id = $1
		   ) t
		  WHERE rn = 1`, userID,
	).Scan(&saldo)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"totaal":    totaal,
		"inkomsten": inkomsten,
		"uitgaven":  uitgaven,
		"saldo":     saldo,
	}, nil
}

func scanTransaction(row pgx.CollectableRow) (model.Transaction, error) {
	var t model.Transaction
	err := row.Scan(
		&t.ID, &t.UserID, &t.RekeningIban, &t.Volgnr, &t.Datum, &t.Bedrag,
		&t.SaldoNaTrn, &t.Code, &t.TegenrekeningIban, &t.TegenpartijNaam,
		&t.Omschrijving, &t.Referentie, &t.RedenRetour, &t.OorspBedrag, &t.OorspMunt,
		&t.IsInterneOverboeking, &t.Categorie,
	)
	return t, err
}

func pgArgNum(n int) string { return strconv.Itoa(n) }

// escapeLikePattern escapes \, % and _ in a user-supplied search term so it is
// matched literally inside a LIKE ... ESCAPE '\' pattern.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
