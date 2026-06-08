package engine

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func (e *HomeBotExecutor) inferLaventeCareBusinessContext(ctx context.Context, currentType, currentID, currentTitle string, textParts ...string) (string, string, string) {
	currentType = strings.TrimSpace(currentType)
	currentID = strings.TrimSpace(currentID)
	currentTitle = strings.TrimSpace(currentTitle)
	if currentType != "" && currentType != "laventecare" && currentID != "" {
		return currentType, currentID, currentTitle
	}

	text := normalizeLCContextText(strings.Join(append(textParts, currentTitle), " "))
	if text == "" || e.laventeCareStore == nil {
		return currentType, currentID, currentTitle
	}

	companies, err := e.laventeCareStore.ListCompanies(ctx, e.userID, 50, "")
	if err != nil {
		return currentType, currentID, currentTitle
	}

	compactText := compactLCContextText(text)
	var best *model.LCCompany
	bestScore := 0
	for i := range companies {
		company := &companies[i]
		score := scoreCompanyContextMatch(text, compactText, company)
		if score > bestScore {
			best = company
			bestScore = score
		}
	}

	if best != nil && bestScore >= 75 {
		return "laventecare_company", best.ID.String(), best.Naam
	}
	if currentType == "" && containsLCContextTerm(text, "laventecare") {
		return "laventecare", "", "LaventeCare"
	}
	return currentType, currentID, currentTitle
}

func scoreCompanyContextMatch(text, compactText string, company *model.LCCompany) int {
	if company == nil {
		return 0
	}
	score := scoreLCContextAlias(text, compactText, company.Naam, 95)
	if company.Website != nil {
		for _, alias := range lcWebsiteAliases(*company.Website) {
			if aliasScore := scoreLCContextAlias(text, compactText, alias, 88); aliasScore > score {
				score = aliasScore
			}
		}
	}
	return score
}

func scoreLCContextAlias(text, compactText, alias string, base int) int {
	alias = normalizeLCContextText(alias)
	if alias == "" || isGenericLCContextAlias(alias) {
		return 0
	}
	if containsLCContextTerm(text, alias) {
		return base + min(len(alias), 40)
	}
	compactAlias := compactLCContextText(alias)
	if len(compactAlias) >= 5 && strings.Contains(compactText, compactAlias) {
		return base + min(len(compactAlias), 40) - 8
	}
	return 0
}

func lcWebsiteAliases(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	host := value
	if err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	host = strings.Split(host, "/")[0]
	name := strings.Split(host, ".")[0]
	return []string{host, name}
}

func normalizeLCContextText(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "&", " en ")
	value = lcContextNonWord.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func compactLCContextText(value string) string {
	return strings.ReplaceAll(normalizeLCContextText(value), " ", "")
}

func containsLCContextTerm(text, term string) bool {
	if term == "" {
		return false
	}
	return regexp.MustCompile(`(^|[^a-z0-9])` + regexp.QuoteMeta(term) + `([^a-z0-9]|$)`).MatchString(text)
}

func isGenericLCContextAlias(value string) bool {
	switch value {
	case "project", "opdracht", "pilot", "website", "klant", "klantdossier", "laventecare":
		return true
	default:
		return false
	}
}

var lcContextNonWord = regexp.MustCompile(`[^a-z0-9]+`)
