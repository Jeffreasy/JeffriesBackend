package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type LoonstrookStore struct{ db *DB }

func NewLoonstrookStore(db *DB) *LoonstrookStore { return &LoonstrookStore{db: db} }

// List returns all loonstroken for a user.
func (s *LoonstrookStore) List(ctx context.Context, userID string) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, jaar, periode, periode_label, type,
		        netto, bruto_betaling, bruto_inhouding, salaris_basis,
		        ort_totaal, ort_detail, amt_zeerintensief, pensioenpremie,
		        loonheffing, reiskosten, vakantietoeslag, eju_bedrag,
		        toeslag_balansvlf, extra_uren_bedrag,
		        schaalnummer, trede, parttime_factor, uurloon,
		        componenten, cumulatieven, geimporteerd_op
		   FROM loonstroken WHERE user_id = $1
		  ORDER BY jaar DESC, periode DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (map[string]any, error) {
		var (
			id, userID                                                      string
			jaar, periode                                                   int
			periodeLabel, typ, schaalnummer, trede                          string
			netto, brutoBetaling, brutoInhouding, salarisBasis, ortTotaal   float64
			parttimeFactor                                                  float64
			ortDetail, componenten, cumulatieven                            json.RawMessage
			amtZeer, pensioen, lhf, reis, vak, eju, balans, extra, uurloon *float64
			geimporteerdOp                                                  *time.Time
		)
		err := row.Scan(&id, &userID, &jaar, &periode, &periodeLabel, &typ,
			&netto, &brutoBetaling, &brutoInhouding, &salarisBasis,
			&ortTotaal, &ortDetail, &amtZeer, &pensioen,
			&lhf, &reis, &vak, &eju, &balans, &extra,
			&schaalnummer, &trede, &parttimeFactor, &uurloon,
			&componenten, &cumulatieven, &geimporteerdOp)
		if err != nil {
			return nil, err
		}

		var geimpStr string
		if geimporteerdOp != nil {
			geimpStr = geimporteerdOp.Format(time.RFC3339)
		}

		return map[string]any{
			"id": id, "user_id": userID, "jaar": jaar, "periode": periode,
			"periode_label": periodeLabel, "type": typ,
			"netto": netto, "bruto_betaling": brutoBetaling,
			"bruto_inhouding": brutoInhouding, "salaris_basis": salarisBasis,
			"ort_totaal": ortTotaal, "ort_detail": ortDetail,
			"amt_zeerintensief": amtZeer, "pensioenpremie": pensioen,
			"loonheffing": lhf, "reiskosten": reis, "vakantietoeslag": vak,
			"eju_bedrag": eju, "toeslag_balansvlf": balans, "extra_uren_bedrag": extra,
			"schaalnummer": schaalnummer, "trede": trede,
			"parttime_factor": parttimeFactor, "uurloon": uurloon,
			"componenten": componenten, "cumulatieven": cumulatieven,
			"geimporteerd_op": geimpStr,
		}, nil
	})
}

// ImportResult reports how many rows were newly inserted versus updated.
type ImportResult struct {
	Inserted int // genuinely new rows
	Updated  int // existing rows whose data changed via upsert
}

// ImportBatch upserts parsed loonstroken. A re-uploaded (corrected) payslip for an
// existing (user_id, jaar, periode) now overwrites all mutable columns instead of
// being silently skipped. The returned ImportResult distinguishes truly-new rows
// (Inserted) from overwritten ones (Updated) using the Postgres `xmax = 0` trick:
// on a fresh INSERT xmax is 0, on an ON CONFLICT UPDATE it is the deleting txid.
func (s *LoonstrookStore) ImportBatch(ctx context.Context, userID string, items []map[string]any) (ImportResult, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback(ctx)

	var res ImportResult
	for _, item := range items {
		ortDetailJSON, _ := json.Marshal(item["ortDetail"])
		componentsJSON, _ := json.Marshal(item["componenten"])
		cumulJSON, _ := json.Marshal(item["cumulatieven"])

		var isInsert bool
		err := tx.QueryRow(ctx,
			`INSERT INTO loonstroken (id, user_id, jaar, periode, periode_label, type,
			    netto, bruto_betaling, bruto_inhouding, salaris_basis,
			    ort_totaal, ort_detail, amt_zeerintensief, pensioenpremie,
			    loonheffing, reiskosten, vakantietoeslag, eju_bedrag,
			    toeslag_balansvlf, extra_uren_bedrag,
			    schaalnummer, trede, parttime_factor, uurloon,
			    componenten, cumulatieven)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
			 ON CONFLICT (user_id, jaar, periode) DO UPDATE SET
			    periode_label     = EXCLUDED.periode_label,
			    type              = EXCLUDED.type,
			    netto             = EXCLUDED.netto,
			    bruto_betaling    = EXCLUDED.bruto_betaling,
			    bruto_inhouding   = EXCLUDED.bruto_inhouding,
			    salaris_basis     = EXCLUDED.salaris_basis,
			    ort_totaal        = EXCLUDED.ort_totaal,
			    ort_detail        = EXCLUDED.ort_detail,
			    amt_zeerintensief = EXCLUDED.amt_zeerintensief,
			    pensioenpremie    = EXCLUDED.pensioenpremie,
			    loonheffing       = EXCLUDED.loonheffing,
			    reiskosten        = EXCLUDED.reiskosten,
			    vakantietoeslag   = EXCLUDED.vakantietoeslag,
			    eju_bedrag        = EXCLUDED.eju_bedrag,
			    toeslag_balansvlf = EXCLUDED.toeslag_balansvlf,
			    extra_uren_bedrag = EXCLUDED.extra_uren_bedrag,
			    schaalnummer      = EXCLUDED.schaalnummer,
			    trede             = EXCLUDED.trede,
			    parttime_factor   = EXCLUDED.parttime_factor,
			    uurloon           = EXCLUDED.uurloon,
			    componenten       = EXCLUDED.componenten,
			    cumulatieven      = EXCLUDED.cumulatieven,
			    geimporteerd_op   = now()
			 RETURNING (xmax = 0)`,
			uuid.New(), userID,
			toInt(item["jaar"]), toInt(item["periode"]),
			toString(item["periodeLabel"]), toString(item["type"]),
			toFloat(item["netto"]), toFloat(item["brutoBetaling"]),
			toFloat(item["brutoInhouding"]), toFloat(item["salarisBasis"]),
			toFloat(item["ortTotaal"]), ortDetailJSON,
			toFloatPtr(item["amtZeerintensief"]), toFloatPtr(item["pensioenpremie"]),
			toFloatPtr(item["loonheffing"]), toFloatPtr(item["reiskosten"]),
			toFloatPtr(item["vakantietoeslag"]), toFloatPtr(item["ejuBedrag"]),
			toFloatPtr(item["toeslagBalansvlf"]), toFloatPtr(item["extraUrenBedrag"]),
			toString(item["schaalnummer"]), toString(item["trede"]),
			toFloat(item["parttimeFactor"]), toFloatPtr(item["uurloon"]),
			componentsJSON, cumulJSON,
		).Scan(&isInsert)
		if err != nil {
			return ImportResult{}, err
		}
		if isInsert {
			res.Inserted++
		} else {
			res.Updated++
		}
	}
	return res, tx.Commit(ctx)
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

func toFloatPtr(v any) *float64 {
	if v == nil {
		return nil
	}
	f := toFloat(v)
	return &f
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
