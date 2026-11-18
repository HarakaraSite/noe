package apidocs

import (
	"sort"
	"strings"
	"sync"
	"unicode"
)

const (
	searchLimit  = 5
	excerptLimit = 240
)

// SearchResult identifies an entry that can be passed directly to Lookup.
type SearchResult struct {
	Topic   string `js:"topic"`
	Member  string `js:"member"`
	Excerpt string `js:"excerpt"`
}

type searchEntry struct {
	result SearchResult
	names  []string
	words  map[string]int
}

var searchIndex = sync.OnceValue(func() []searchEntry {
	var index []searchEntry
	for _, e := range builtin.ordered {
		names := append([]string{e.info.ID}, e.info.Aliases...)
		index = append(index, newSearchEntry(e.info.ID, "", e.info.Summary, names))
		for _, member := range e.info.Members {
			_, detail, _ := strings.Cut(e.details[member], "\n")
			// Accept an unqualified method name as well as the documented ID.
			shortName := member[strings.LastIndexByte(member, '.')+1:]
			names := []string{member, shortName, e.info.ID + " " + member}
			index = append(index, newSearchEntry(e.info.ID, member, strings.TrimSpace(detail), names))
		}
	}
	return index
})

// Search returns up to five matches from the embedded reference.
func Search(query string) []SearchResult {
	query = strings.TrimSpace(query)
	words := searchWords(localizedSearchWords.Replace(query))
	results := make([]SearchResult, 0, searchLimit)
	if len(words) == 0 {
		return results
	}

	type match struct {
		result SearchResult
		exact  bool
		count  int
		score  int
	}
	var matches []match
	for _, entry := range searchIndex() {
		ranked := match{result: entry.result}
		for _, name := range entry.names {
			if strings.EqualFold(query, name) {
				ranked.exact = true
				break
			}
		}
		for word := range words {
			if weight := entry.words[word]; weight != 0 {
				ranked.count++
				ranked.score += weight
			}
		}
		if ranked.exact || ranked.count != 0 {
			matches = append(matches, ranked)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		left, right := matches[i], matches[j]
		if left.exact != right.exact {
			return left.exact
		}
		if left.score != right.score {
			return left.score > right.score
		}
		return left.count > right.count
	})
	for _, match := range matches[:min(len(matches), searchLimit)] {
		if matches[0].exact && !match.exact {
			break
		}
		results = append(results, match.result)
	}
	return results
}

func newSearchEntry(topic, member, text string, names []string) searchEntry {
	lead, _, _ := strings.Cut(text, "\n\n")
	excerpt := strings.Join(strings.Fields(lead), " ")
	if chars := []rune(excerpt); len(chars) > excerptLimit {
		excerpt = string(chars[:excerptLimit-1])
		if end := strings.LastIndexByte(excerpt, ' '); end > 0 {
			excerpt = excerpt[:end]
		}
		excerpt += "…"
	}
	words := searchWords(text)
	for word := range searchWords(lead) {
		words[word] = 3
	}
	for word := range searchWords(strings.Join(names, " ")) {
		words[word] = 4
	}
	return searchEntry{result: SearchResult{Topic: topic, Member: member, Excerpt: excerpt}, names: names, words: words}
}

var localizedSearchWords = strings.NewReplacer(
	"ファイル", " file ",
	"一部", " part ",
	"部分", " part ",
	"バイト範囲", " byte range ",
	"バイト位置", " byte offset ",
	"読み込", " read ",
	"読み取", " read ",
	"読み出", " read ",
	"読む", " read ",
)

func searchWords(text string) map[string]int {
	words := make(map[string]int)
	var token strings.Builder
	flush := func() {
		switch word := token.String(); word {
		case "", "a", "an", "and", "at", "by", "for", "from", "how", "in", "of", "or", "the", "to", "with":
		default:
			words[word] = 1
		}
		token.Reset()
	}
	var previous rune
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if unicode.IsUpper(r) && unicode.IsLower(previous) {
				flush()
			}
			token.WriteRune(unicode.ToLower(r))
		} else {
			flush()
		}
		previous = r
	}
	flush()
	return words
}
