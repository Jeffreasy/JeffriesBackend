package store

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// TransactionStats contains the full aggregated finance dashboard data.
type TransactionStats struct {
	// Kasstromen
	TotaalIn     float64 `json:"totaalIn" binding:"required"`
	TotaalUit    float64 `json:"totaalUit" binding:"required"`
	NettoStroom  float64 `json:"nettoStroom" binding:"required"`
	GemiddeldIn  float64 `json:"gemiddeldIn" binding:"required"`
	GemiddeldUit float64 `json:"gemiddeldUit" binding:"required"`

	// Echte bankbalans
	HuidigSaldo           float64            `json:"huidigSaldo" binding:"required"`
	HuidigSaldoPerIban    map[string]float64 `json:"huidigSaldoPerIban" binding:"required"`
	SaldoPeildatumPerIban map[string]string  `json:"saldoPeildatumPerIban" binding:"required"`
	LaatsteSaldoPeildatum *string            `json:"laatsteSaldoPeildatum"`

	// Categorieën
	UitPerCategorie   []CategorieBreakdown `json:"uitPerCategorie" binding:"required"`
	InPerCategorie    []CategorieBreakdown `json:"inPerCategorie" binding:"required"`
	AantalCategorieen int                  `json:"aantalCategorieen" binding:"required"`

	// Grafieken
	SaldoPerMaand []MaandSaldo  `json:"saldoPerMaand" binding:"required"`
	InUitPerMaand []MaandInUit  `json:"inUitPerMaand" binding:"required"`
	TopMerchants  []TopMerchant `json:"topMerchants" binding:"required"`

	// Overige
	Storneringen  int      `json:"storneringen" binding:"required"`
	AantalAlleTxs int      `json:"aantalAlleTxs" binding:"required"`
	AantalTxs     int      `json:"aantalTxs" binding:"required"`
	Maanden       []string `json:"maanden" binding:"required"`
	Jaren         []string `json:"jaren" binding:"required"`
	Ibannen       []string `json:"ibannen" binding:"required"`
}

type CategorieBreakdown struct {
	Categorie  string  `json:"categorie" binding:"required"`
	Bedrag     float64 `json:"bedrag" binding:"required"`
	Count      int     `json:"count" binding:"required"`
	Percentage float64 `json:"percentage,omitempty"`
}

type MaandSaldo struct {
	Maand string  `json:"maand" binding:"required"`
	Saldo float64 `json:"saldo" binding:"required"`
}

type MaandInUit struct {
	Maand     string  `json:"maand" binding:"required"`
	Inkomsten float64 `json:"inkomsten" binding:"required"`
	Uitgaven  float64 `json:"uitgaven" binding:"required"`
	Netto     float64 `json:"netto" binding:"required"`
}

type TopMerchant struct {
	Naam   string  `json:"naam" binding:"required"`
	Bedrag float64 `json:"bedrag" binding:"required"`
	Count  int     `json:"count" binding:"required"`
}

// GetFullStats returns the complete finance dashboard statistics,
// replicating the Convex getStats query with full aggregation.
//
// Filter precedence (jaarFilter + datumVan/datumTot):
//   - jaarFilter constrains the aggregation set to a single calendar year.
//   - datumVan/datumTot narrow *within* that year; if both a year and a date
//     range are supplied they are intersected (year lower/upper bounds AND the
//     explicit range), so the effective window is the tighter of the two on
//     each side. All bounds are pushed into SQL rather than filtered in Go.
//
// The point-in-time balance fields (huidigSaldo, huidigSaldoPerIban, saldo per
// maand carry-forward) and the jaren/ibannen index lists intentionally stay
// unfiltered — they describe the account as a whole, not the selected period.
func (s *TransactionStore) GetFullStats(ctx context.Context, userID string, ibanFilter, jaarFilter, datumVan, datumTot *string) (*TransactionStats, error) {
	stats := &TransactionStats{
		HuidigSaldoPerIban:    make(map[string]float64),
		SaldoPeildatumPerIban: make(map[string]string),
	}

	// ── Unfiltered index data (jaren, ibannen, total count) ──────────────────
	// Cheap dedicated queries instead of scanning every row in Go.
	jaren, ibannen, alleCount, err := s.statsIndex(ctx, userID)
	if err != nil {
		return nil, err
	}
	stats.Jaren = jaren
	stats.Ibannen = ibannen
	stats.AantalAlleTxs = alleCount

	// ── Latest saldo per IBAN (unfiltered, point-in-time) ────────────────────
	type ibanSaldo struct {
		datum string
		saldo float64
	}
	ibanSaldoMap, err := s.statsLatestSaldoPerIban(ctx, userID)
	if err != nil {
		return nil, err
	}
	var laatsteDatum string
	for iban, s := range ibanSaldoMap {
		stats.HuidigSaldoPerIban[iban] = round2(s.saldo)
		stats.SaldoPeildatumPerIban[iban] = s.datum
		if s.datum > laatsteDatum {
			laatsteDatum = s.datum
		}
	}
	if laatsteDatum != "" {
		stats.LaatsteSaldoPeildatum = &laatsteDatum
	}

	// ── Filtered aggregation set (iban + jaar ∩ date-range), pushed to SQL ────
	van, tot := intersectYearAndRange(jaarFilter, datumVan, datumTot)
	txs, err := s.statsAggregationRows(ctx, userID, ibanFilter, van, tot)
	if err != nil {
		return nil, err
	}
	stats.AantalTxs = len(txs)

	// Huidig saldo
	if ibanFilter != nil && *ibanFilter != "" {
		if s, ok := ibanSaldoMap[*ibanFilter]; ok {
			stats.HuidigSaldo = round2(s.saldo)
		}
	} else {
		var total float64
		for _, s := range ibanSaldoMap {
			total += s.saldo
		}
		stats.HuidigSaldo = round2(total)
	}

	// Extern = excl. interne overboekingen
	extern := filterExtern(txs)

	// Kasstromen
	for _, t := range extern {
		if t.Bedrag > 0 {
			stats.TotaalIn += t.Bedrag
		} else {
			stats.TotaalUit += t.Bedrag
		}
	}
	stats.TotaalIn = round2(stats.TotaalIn)
	stats.TotaalUit = round2(math.Abs(stats.TotaalUit))
	stats.NettoStroom = round2(stats.TotaalIn - stats.TotaalUit)

	// Storneringen
	for _, t := range txs {
		if t.Code == "st" {
			stats.Storneringen++
		}
	}

	// Maanden
	maandSet := make(map[string]bool)
	for _, t := range txs {
		if len(t.Datum) >= 7 {
			maandSet[t.Datum[:7]] = true
		}
	}
	stats.Maanden = sortedKeys(maandSet)

	aantalMaanden := max(len(stats.Maanden), 1)
	stats.GemiddeldIn = round2(stats.TotaalIn / float64(aantalMaanden))
	stats.GemiddeldUit = round2(stats.TotaalUit / float64(aantalMaanden))

	// Uitgaven per categorie
	catUitMap := make(map[string]*CategorieBreakdown)
	for _, t := range extern {
		if t.Bedrag >= 0 {
			continue
		}
		cat := derefOr(t.Categorie, "Overig")
		cb, ok := catUitMap[cat]
		if !ok {
			cb = &CategorieBreakdown{Categorie: cat}
			catUitMap[cat] = cb
		}
		cb.Bedrag += math.Abs(t.Bedrag)
		cb.Count++
	}
	if stats.TotaalUit > 0 {
		for _, cb := range catUitMap {
			cb.Bedrag = round2(cb.Bedrag)
			cb.Percentage = round1(cb.Bedrag / stats.TotaalUit * 100)
		}
	}
	stats.UitPerCategorie = sortedBreakdowns(catUitMap)
	stats.AantalCategorieen = len(catUitMap)

	// Inkomsten per categorie
	catInMap := make(map[string]*CategorieBreakdown)
	for _, t := range extern {
		if t.Bedrag <= 0 {
			continue
		}
		cat := derefOr(t.Categorie, "Overig")
		cb, ok := catInMap[cat]
		if !ok {
			cb = &CategorieBreakdown{Categorie: cat}
			catInMap[cat] = cb
		}
		cb.Bedrag += t.Bedrag
		cb.Count++
	}
	for _, cb := range catInMap {
		cb.Bedrag = round2(cb.Bedrag)
	}
	stats.InPerCategorie = sortedBreakdowns(catInMap)

	// In/Uit per maand
	inUitMap := make(map[string]*MaandInUit)
	for _, t := range extern {
		if len(t.Datum) < 7 {
			continue
		}
		maand := t.Datum[:7]
		m, ok := inUitMap[maand]
		if !ok {
			m = &MaandInUit{Maand: maand}
			inUitMap[maand] = m
		}
		if t.Bedrag > 0 {
			m.Inkomsten += t.Bedrag
		} else {
			m.Uitgaven += math.Abs(t.Bedrag)
		}
	}
	for _, m := range inUitMap {
		m.Inkomsten = round2(m.Inkomsten)
		m.Uitgaven = round2(m.Uitgaven)
		m.Netto = round2(m.Inkomsten - m.Uitgaven)
	}
	stats.InUitPerMaand = sortedMaandInUit(inUitMap)

	// Saldo per maand (with carry-forward per IBAN). This deliberately does NOT
	// apply the lower date bound: the carry-forward needs every earlier row to
	// seed each displayed month's opening balance. Only the iban filter applies.
	saldoBron, err := s.statsAggregationRows(ctx, userID, ibanFilter, nil, tot)
	if err != nil {
		return nil, err
	}
	stats.SaldoPerMaand = computeSaldoPerMaand(saldoBron, stats.Maanden)

	// Top merchants
	merchantMap := make(map[string]*TopMerchant)
	for _, t := range extern {
		if t.Bedrag >= 0 || t.TegenpartijNaam == nil || *t.TegenpartijNaam == "" {
			continue
		}
		naam := *t.TegenpartijNaam
		m, ok := merchantMap[naam]
		if !ok {
			m = &TopMerchant{Naam: naam}
			merchantMap[naam] = m
		}
		m.Bedrag += math.Abs(t.Bedrag)
		m.Count++
	}
	for _, m := range merchantMap {
		m.Bedrag = round2(m.Bedrag)
	}
	stats.TopMerchants = topNMerchants(merchantMap, 10)

	return stats, nil
}

// ─── SQL data loaders for stats ──────────────────────────────────────────────

// statsIndex returns the all-time jaren (years), ibannen and total tx count via
// dedicated aggregate queries instead of scanning every row in Go.
func (s *TransactionStore) statsIndex(ctx context.Context, userID string) (jaren, ibannen []string, total int, err error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT DISTINCT to_char(datum, 'YYYY') FROM transactions WHERE user_id = $1 ORDER BY 1`, userID)
	if err != nil {
		return nil, nil, 0, err
	}
	jaren, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, nil, 0, err
	}

	rows, err = s.db.Pool.Query(ctx,
		`SELECT DISTINCT rekening_iban FROM transactions WHERE user_id = $1 ORDER BY 1`, userID)
	if err != nil {
		return nil, nil, 0, err
	}
	ibannen, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, nil, 0, err
	}

	if err = s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, nil, 0, err
	}
	return jaren, ibannen, total, nil
}

// statsLatestSaldoPerIban returns the point-in-time balance per IBAN: the
// saldo_na_trn of the most recent transaction (by datum, then numeric volgnr).
func (s *TransactionStore) statsLatestSaldoPerIban(ctx context.Context, userID string) (map[string]struct {
	datum string
	saldo float64
}, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT rekening_iban, datum::text, saldo_na_trn FROM (
		     SELECT rekening_iban, datum, saldo_na_trn,
		            ROW_NUMBER() OVER (PARTITION BY rekening_iban
		                               ORDER BY datum DESC, LPAD(LTRIM(volgnr, '0'), 50, '0') DESC) AS rn
		       FROM transactions WHERE user_id = $1
		 ) t WHERE rn = 1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]struct {
		datum string
		saldo float64
	})
	for rows.Next() {
		var iban, datum string
		var saldo float64
		if err := rows.Scan(&iban, &datum, &saldo); err != nil {
			return nil, err
		}
		out[iban] = struct {
			datum string
			saldo float64
		}{datum: datum, saldo: saldo}
	}
	return out, rows.Err()
}

// statsAggregationRows loads the transaction rows for aggregation with the
// iban filter and (optional) date range pushed into SQL. Both bounds are
// inclusive. Passing nil skips a bound.
func (s *TransactionStore) statsAggregationRows(ctx context.Context, userID string, ibanFilter, van, tot *string) ([]model.Transaction, error) {
	query := `SELECT ` + trxColumns + ` FROM transactions WHERE user_id = $1`
	args := []any{userID}
	if ibanFilter != nil && *ibanFilter != "" {
		args = append(args, *ibanFilter)
		query += ` AND rekening_iban = $` + pgArgNum(len(args))
	}
	if van != nil && *van != "" {
		args = append(args, *van)
		query += ` AND datum >= $` + pgArgNum(len(args))
	}
	if tot != nil && *tot != "" {
		args = append(args, *tot)
		query += ` AND datum <= $` + pgArgNum(len(args))
	}
	query += ` ORDER BY datum DESC, LPAD(LTRIM(volgnr, '0'), 50, '0') DESC`

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanTransaction)
}

// intersectYearAndRange combines a jaarFilter (single year, e.g. "2026") with an
// explicit datumVan/datumTot range. The year contributes YYYY-01-01 / YYYY-12-31
// bounds; the explicit range narrows within it. The result is the intersection:
// the later of the two lower bounds and the earlier of the two upper bounds.
func intersectYearAndRange(jaarFilter, datumVan, datumTot *string) (van, tot *string) {
	if jaarFilter != nil && *jaarFilter != "" {
		lo := *jaarFilter + "-01-01"
		hi := *jaarFilter + "-12-31"
		van, tot = &lo, &hi
	}
	if datumVan != nil && *datumVan != "" {
		if van == nil || *datumVan > *van {
			v := *datumVan
			van = &v
		}
	}
	if datumTot != nil && *datumTot != "" {
		if tot == nil || *datumTot < *tot {
			t := *datumTot
			tot = &t
		}
	}
	return van, tot
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func isLater(datum1, volgnr1, datum2, volgnr2 string) bool {
	if datum1 != datum2 {
		return datum1 > datum2
	}
	// Numeric compare so a shorter volgnr ("9") is not treated as greater than a
	// longer one ("10") at a digit-length rollover.
	return volgnrLess(volgnr2, volgnr1)
}

func filterExtern(txs []model.Transaction) []model.Transaction {
	var out []model.Transaction
	for _, t := range txs {
		if !t.IsInterneOverboeking {
			out = append(out, t)
		}
	}
	return out
}

func derefOr(s *string, fallback string) string {
	if s != nil && *s != "" {
		return *s
	}
	return fallback
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round1(v float64) float64 { return math.Round(v*10) / 10 }

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sortedBreakdowns(m map[string]*CategorieBreakdown) []CategorieBreakdown {
	out := make([]CategorieBreakdown, 0, len(m))
	for _, cb := range m {
		out = append(out, *cb)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bedrag > out[j].Bedrag })
	return out
}

func sortedMaandInUit(m map[string]*MaandInUit) []MaandInUit {
	out := make([]MaandInUit, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Maand < out[j].Maand })
	return out
}

func topNMerchants(m map[string]*TopMerchant, n int) []TopMerchant {
	out := make([]TopMerchant, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bedrag > out[j].Bedrag })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// volgnrLess reports whether volgnr a sorts before b using numeric semantics
// (left-pad to equal width, then lexical), so "9" < "10" instead of the raw
// string comparison that would put "9" after "10".
func volgnrLess(a, b string) bool {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func computeSaldoPerMaand(txs []model.Transaction, maanden []string) []MaandSaldo {
	// Sort transactions by datum+volgnr ascending (numeric volgnr tiebreak).
	sorted := make([]model.Transaction, len(txs))
	copy(sorted, txs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Datum != sorted[j].Datum {
			return sorted[i].Datum < sorted[j].Datum
		}
		return volgnrLess(sorted[i].Volgnr, sorted[j].Volgnr)
	})

	type ibanState struct{ saldo float64 }
	laatsteSaldo := make(map[string]*ibanState)
	cursor := 0

	result := make([]MaandSaldo, 0, len(maanden))
	for _, maand := range maanden {
		maandEinde := maand + "-31"
		for cursor < len(sorted) && sorted[cursor].Datum <= maandEinde {
			t := sorted[cursor]
			laatsteSaldo[t.RekeningIban] = &ibanState{saldo: t.SaldoNaTrn}
			cursor++
		}
		var total float64
		for _, s := range laatsteSaldo {
			total += s.saldo
		}
		result = append(result, MaandSaldo{Maand: maand, Saldo: round2(total)})
	}
	return result
}
