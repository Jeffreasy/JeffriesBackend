package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type SalaryStore struct{ db *DB }

func NewSalaryStore(db *DB) *SalaryStore { return &SalaryStore{db: db} }

const salaryColumns = `id, user_id, periode, jaar, maand, aantal_diensten, uurloon_ort,
	basis_loon, amt_zeerintensief, toeslag_balansvlf, ort_totaal,
	extra_uren_bedrag, toeslag_vakatie_uren, reiskosten, eenmalig_totaal,
	bruto_betaling, pensioenpremie, loonheffing_schat, netto_prognose, berekend_op`

// List returns all salary records for a user.
func (s *SalaryStore) List(ctx context.Context, userID string) ([]model.Salary, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+salaryColumns+` FROM salary WHERE user_id = $1 ORDER BY jaar DESC, maand DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanSalary)
}

// GetByPeriode returns a single salary record.
func (s *SalaryStore) GetByPeriode(ctx context.Context, userID, periode string) (*model.Salary, error) {
	var sal model.Salary
	err := s.db.Pool.QueryRow(ctx,
		`SELECT `+salaryColumns+` FROM salary WHERE user_id = $1 AND periode = $2`, userID, periode,
	).Scan(
		&sal.ID, &sal.UserID, &sal.Periode, &sal.Jaar, &sal.Maand,
		&sal.AantalDiensten, &sal.UurloonORT, &sal.BasisLoon, &sal.AmtZeerintensief,
		&sal.ToeslagBalansvlf, &sal.OrtTotaal, &sal.ExtraUrenBedrag,
		&sal.ToeslagVakatieUren, &sal.Reiskosten, &sal.EenmaligTotaal,
		&sal.BrutoBetaling, &sal.Pensioenpremie, &sal.LoonheffingSchat,
		&sal.NettoPrognose, &sal.BerekendOp,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sal, nil
}

// Upsert creates or updates a salary record.
func (s *SalaryStore) Upsert(ctx context.Context, sal model.Salary) error {
	if sal.ID == uuid.Nil {
		sal.ID = uuid.New()
	}
	sal.BerekendOp = time.Now().UTC()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO salary (id, user_id, periode, jaar, maand, aantal_diensten, uurloon_ort,
		    basis_loon, amt_zeerintensief, toeslag_balansvlf, ort_totaal,
		    extra_uren_bedrag, toeslag_vakatie_uren, reiskosten, eenmalig_totaal,
		    bruto_betaling, pensioenpremie, loonheffing_schat, netto_prognose, berekend_op)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		 ON CONFLICT (user_id, periode) DO UPDATE SET
		    aantal_diensten = EXCLUDED.aantal_diensten, uurloon_ort = EXCLUDED.uurloon_ort,
		    basis_loon = EXCLUDED.basis_loon, amt_zeerintensief = EXCLUDED.amt_zeerintensief,
		    toeslag_balansvlf = EXCLUDED.toeslag_balansvlf, ort_totaal = EXCLUDED.ort_totaal,
		    extra_uren_bedrag = EXCLUDED.extra_uren_bedrag, toeslag_vakatie_uren = EXCLUDED.toeslag_vakatie_uren,
		    reiskosten = EXCLUDED.reiskosten, eenmalig_totaal = EXCLUDED.eenmalig_totaal,
		    bruto_betaling = EXCLUDED.bruto_betaling, pensioenpremie = EXCLUDED.pensioenpremie,
		    loonheffing_schat = EXCLUDED.loonheffing_schat, netto_prognose = EXCLUDED.netto_prognose,
		    berekend_op = EXCLUDED.berekend_op`,
		sal.ID, sal.UserID, sal.Periode, sal.Jaar, sal.Maand,
		sal.AantalDiensten, sal.UurloonORT, sal.BasisLoon, sal.AmtZeerintensief,
		sal.ToeslagBalansvlf, sal.OrtTotaal, sal.ExtraUrenBedrag,
		sal.ToeslagVakatieUren, sal.Reiskosten, sal.EenmaligTotaal,
		sal.BrutoBetaling, sal.Pensioenpremie, sal.LoonheffingSchat,
		sal.NettoPrognose, sal.BerekendOp,
	)
	return err
}

func scanSalary(row pgx.CollectableRow) (model.Salary, error) {
	var s model.Salary
	err := row.Scan(
		&s.ID, &s.UserID, &s.Periode, &s.Jaar, &s.Maand,
		&s.AantalDiensten, &s.UurloonORT, &s.BasisLoon, &s.AmtZeerintensief,
		&s.ToeslagBalansvlf, &s.OrtTotaal, &s.ExtraUrenBedrag,
		&s.ToeslagVakatieUren, &s.Reiskosten, &s.EenmaligTotaal,
		&s.BrutoBetaling, &s.Pensioenpremie, &s.LoonheffingSchat,
		&s.NettoPrognose, &s.BerekendOp,
	)
	return s, err
}
