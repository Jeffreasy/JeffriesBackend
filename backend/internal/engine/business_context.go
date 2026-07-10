package engine

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// inferNoteBusinessContext adds a deliberately strict contact path in front of
// the existing LaventeCare inference. A contact is inferred only from an
// explicit @ mention that resolves to exactly one active, owned contact; normal
// prose and fuzzy names never create a relationship silently.
func (e *HomeBotExecutor) inferNoteBusinessContext(ctx context.Context, currentType, currentID, currentTitle string, textParts ...string) (string, string, string) {
	currentType = strings.TrimSpace(currentType)
	currentID = strings.TrimSpace(currentID)
	currentTitle = strings.TrimSpace(currentTitle)

	if currentType == "contact" && currentID != "" {
		return currentType, currentID, currentTitle
	}
	if currentType == "" || currentType == "contact" {
		text := strings.Join(append(textParts, currentTitle), " ")
		if e.contactStore != nil {
			contacts, err := e.contactStore.List(ctx, e.userID, store.ListContactsOptions{})
			if err == nil {
				if contact, ok := explicitContactMention(text, contacts); ok {
					return "contact", contact.ID.String(), contact.DisplayName
				}
			}
		}
		if currentType == "contact" {
			// Do not reinterpret a malformed explicit contact request as a company.
			// NoteStore will return the shared validation error to the caller.
			return currentType, currentID, currentTitle
		}
	}
	return e.inferLaventeCareBusinessContext(ctx, currentType, currentID, currentTitle, textParts...)
}

func explicitContactMention(text string, contacts []model.Contact) (model.Contact, bool) {
	byID := make(map[string]model.Contact, len(contacts))
	for _, contact := range contacts {
		if strings.TrimSpace(contact.DisplayName) != "" {
			byID[contact.ID.String()] = contact
		}
	}

	// Bracket form is the unambiguous option for names with spaces:
	// @[Anne van Dijk]. If duplicate contacts share that exact display name, do
	// not guess which record the user intended.
	matched := map[string]model.Contact{}
	if matches := explicitContactBracketPattern.FindAllStringSubmatchIndex(text, -1); len(matches) > 0 {
		for _, match := range matches {
			if !isContactMentionLeadingBoundary(text, match[0]) {
				continue
			}
			wanted := normalizeContactMentionName(text[match[2]:match[3]])
			for id, contact := range byID {
				if normalizeContactMentionName(contact.DisplayName) == wanted {
					matched[id] = contact
				}
			}
		}
	}

	// The compact @Name form is also supported. At one @ position, the longest
	// exact contact name wins (so @Jan Jansen does not collapse to @Jan). Equal
	// longest names or mentions of multiple people remain ambiguous.
	type positionMatch struct {
		nameLength int
		contacts   map[string]model.Contact
	}
	positions := map[int]positionMatch{}
	for id, contact := range byID {
		name := strings.TrimSpace(contact.DisplayName)
		for _, mentionStart := range explicitContactMentionStarts(text, name) {
			candidateLength := len(name)
			current := positions[mentionStart]
			switch {
			case candidateLength > current.nameLength:
				positions[mentionStart] = positionMatch{nameLength: candidateLength, contacts: map[string]model.Contact{id: contact}}
			case candidateLength == current.nameLength:
				if current.contacts == nil {
					current.contacts = map[string]model.Contact{}
				}
				current.contacts[id] = contact
				positions[mentionStart] = current
			}
		}
	}
	for _, position := range positions {
		for id, contact := range position.contacts {
			matched[id] = contact
		}
	}
	if len(matched) == 1 {
		for _, contact := range matched {
			return contact, true
		}
	}
	return model.Contact{}, false
}

func normalizeContactMentionName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func explicitContactMentionStarts(text, displayName string) []int {
	if displayName == "" {
		return nil
	}
	lowerText := strings.ToLower(text)
	needle := "@" + strings.ToLower(displayName)
	starts := []int{}
	for searchFrom := 0; searchFrom < len(lowerText); {
		relative := strings.Index(lowerText[searchFrom:], needle)
		if relative < 0 {
			break
		}
		start := searchFrom + relative
		end := start + len(needle)
		if isContactMentionLeadingBoundary(text, start) && isContactMentionTrailingBoundary(text, end) {
			starts = append(starts, start)
		}
		searchFrom = start + len("@")
	}
	return starts
}

func isContactMentionLeadingBoundary(text string, start int) bool {
	if start == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	return unicode.IsSpace(r) || strings.ContainsRune(`([{"'“‘,;:!?`, r)
}

func isContactMentionTrailingBoundary(text string, end int) bool {
	if end >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[end:])
	return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-'
}

var explicitContactBracketPattern = regexp.MustCompile(`@\[\s*([^\]\r\n]+?)\s*\]`)

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
