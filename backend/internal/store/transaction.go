package store

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type TransactionStore struct{ db *DB }

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
	query += ` ORDER BY datum DESC, volgnr DESC`

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
		args = append(args, "%"+f.Zoekterm+"%")
		n := pgArgNum(len(args))
		where += ` AND (LOWER(tegenpartij_naam) LIKE LOWER($` + n + `) OR LOWER(omschrijving) LIKE LOWER($` + n + `))`
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
		` ORDER BY datum DESC, volgnr DESC LIMIT $` + limitN + ` OFFSET $` + offsetN

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

	for _, item := range items {
		tag, err := tx.Exec(ctx,
			`INSERT INTO transactions (id, user_id, rekening_iban, volgnr, datum, bedrag,
			    saldo_na_trn, code, tegenrekening_iban, tegenpartij_naam,
			    omschrijving, referentie, reden_retour, oorsp_bedrag, oorsp_munt,
			    is_interne_overboeking, categorie)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			 ON CONFLICT (user_id, rekening_iban, volgnr) DO NOTHING`,
			uuid.New(), userID, item.RekeningIban, item.Volgnr, item.Datum,
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

// UpdateCategorie sets the categorie for a transaction.
func (s *TransactionStore) UpdateCategorie(ctx context.Context, id uuid.UUID, categorie string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE transactions SET categorie = $2 WHERE id = $1`, id, categorie)
	return err
}

// GetStats returns aggregate stats for a user.
func (s *TransactionStore) GetStats(ctx context.Context, userID string) (map[string]any, error) {
	var totaal int
	var inkomsten, uitgaven float64
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN bedrag > 0 THEN bedrag ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN bedrag < 0 THEN bedrag ELSE 0 END), 0)
		   FROM transactions WHERE user_id = $1`, userID,
	).Scan(&totaal, &inkomsten, &uitgaven)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"totaal":    totaal,
		"inkomsten": inkomsten,
		"uitgaven":  uitgaven,
		"saldo":     inkomsten + uitgaven,
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
