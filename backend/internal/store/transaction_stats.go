package store

import (
	"context"
	"math"

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
	HuidigSaldo            float64            `json:"huidigSaldo" binding:"required"`
	HuidigSaldoPerIban     map[string]float64 `json:"huidigSaldoPerIban" binding:"required"`
	SaldoPeildatumPerIban  map[string]string  `json:"saldoPeildatumPerIban" binding:"required"`
	LaatsteSaldoPeildatum  *string            `json:"laatsteSaldoPeildatum"`

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
func (s *TransactionStore) GetFullStats(ctx context.Context, userID string, ibanFilter, jaarFilter *string) (*TransactionStats, error) {
	// Load all transactions once
	allTxs, err := s.List(ctx, userID, nil, nil)
	if err != nil {
		return nil, err
	}

	stats := &TransactionStats{
		HuidigSaldoPerIban:    make(map[string]float64),
		SaldoPeildatumPerIban: make(map[string]string),
	}
	stats.AantalAlleTxs = len(allTxs)

	// Jaren + IBANs from all transactions
	jarenSet := make(map[string]bool)
	ibanSet := make(map[string]bool)
	for _, t := range allTxs {
		if len(t.Datum) >= 4 {
			jarenSet[t.Datum[:4]] = true
		}
		ibanSet[t.RekeningIban] = true
	}
	stats.Jaren = sortedKeys(jarenSet)
	stats.Ibannen = sortedKeys(ibanSet)

	// Saldo per IBAN: find latest transaction per IBAN
	type ibanSaldo struct {
		datum  string
		volgnr string
		saldo  float64
	}
	ibanSaldoMap := make(map[string]ibanSaldo)
	for _, t := range allTxs {
		prev, exists := ibanSaldoMap[t.RekeningIban]
		if !exists || isLater(t.Datum, t.Volgnr, prev.datum, prev.volgnr) {
			ibanSaldoMap[t.RekeningIban] = ibanSaldo{datum: t.Datum, volgnr: t.Volgnr, saldo: t.SaldoNaTrn}
		}
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

	// Filter on IBAN if set
	txs := allTxs
	if ibanFilter != nil && *ibanFilter != "" {
		txs = filterByIban(txs, *ibanFilter)
	}

	// Filter on year if set
	if jaarFilter != nil && *jaarFilter != "" {
		txs = filterByJaar(txs, *jaarFilter)
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

	// Saldo per maand (with carry-forward per IBAN)
	saldoBron := allTxs
	if ibanFilter != nil && *ibanFilter != "" {
		saldoBron = filterByIban(allTxs, *ibanFilter)
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

// ─── Helpers ─────────────────────────────────────────────────────────────────

func isLater(datum1, volgnr1, datum2, volgnr2 string) bool {
	if datum1 != datum2 {
		return datum1 > datum2
	}
	return volgnr1 > volgnr2
}

func filterByIban(txs []model.Transaction, iban string) []model.Transaction {
	var out []model.Transaction
	for _, t := range txs {
		if t.RekeningIban == iban {
			out = append(out, t)
		}
	}
	return out
}

func filterByJaar(txs []model.Transaction, jaar string) []model.Transaction {
	var out []model.Transaction
	for _, t := range txs {
		if len(t.Datum) >= 4 && t.Datum[:4] == jaar {
			out = append(out, t)
		}
	}
	return out
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
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Bedrag > out[j-1].Bedrag; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func sortedMaandInUit(m map[string]*MaandInUit) []MaandInUit {
	out := make([]MaandInUit, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Maand < out[j-1].Maand; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func topNMerchants(m map[string]*TopMerchant, n int) []TopMerchant {
	out := make([]TopMerchant, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Bedrag > out[j-1].Bedrag; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func computeSaldoPerMaand(txs []model.Transaction, maanden []string) []MaandSaldo {
	// Sort transactions by datum+volgnr ascending
	sorted := make([]model.Transaction, len(txs))
	copy(sorted, txs)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && isLater(sorted[j-1].Datum, sorted[j-1].Volgnr, sorted[j].Datum, sorted[j].Volgnr); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

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
