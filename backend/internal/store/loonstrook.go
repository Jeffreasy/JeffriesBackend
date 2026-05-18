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

// ImportBatch inserts parsed loonstroken, skipping existing periode/jaar combos.
func (s *LoonstrookStore) ImportBatch(ctx context.Context, userID string, items []map[string]any) (int, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var inserted int
	for _, item := range items {
		ortDetailJSON, _ := json.Marshal(item["ortDetail"])
		componentsJSON, _ := json.Marshal(item["componenten"])
		cumulJSON, _ := json.Marshal(item["cumulatieven"])

		tag, err := tx.Exec(ctx,
			`INSERT INTO loonstroken (id, user_id, jaar, periode, periode_label, type,
			    netto, bruto_betaling, bruto_inhouding, salaris_basis,
			    ort_totaal, ort_detail, amt_zeerintensief, pensioenpremie,
			    loonheffing, reiskosten, vakantietoeslag, eju_bedrag,
			    toeslag_balansvlf, extra_uren_bedrag,
			    schaalnummer, trede, parttime_factor, uurloon,
			    componenten, cumulatieven)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
			 ON CONFLICT (user_id, jaar, periode) DO NOTHING`,
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
		)
		if err != nil {
			return 0, err
		}
		inserted += int(tag.RowsAffected())
	}
	return inserted, tx.Commit(ctx)
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
