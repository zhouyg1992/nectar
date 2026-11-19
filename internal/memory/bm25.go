package memory

import (
	"math"
	"strings"
	"unicode"
)

const (
	defaultBM25K1       = 1.2
	defaultBM25B        = 0.75
	defaultBM25TagBoost = 1.5
)

type bm25Doc struct {
	tf     map[string]int
	tags   map[string]bool
	length int
}

type bm25Index struct {
	docs     []bm25Doc
	df       map[string]int
	avgdl    float64
	k1       float64
	b        float64
	tagBoost float64
}

func newBM25Index(docs []bm25Doc) *bm25Index {
	idx := &bm25Index{
		docs:     docs,
		df:       make(map[string]int),
		k1:       defaultBM25K1,
		b:        defaultBM25B,
		tagBoost: defaultBM25TagBoost,
	}
	if len(docs) == 0 {
		return idx
	}
	total := 0
	for _, doc := range docs {
		total += doc.length
		for term := range doc.tf {
			idx.df[term]++
		}
	}
	idx.avgdl = float64(total) / float64(len(docs))
	return idx
}

func newBM25Doc(content string, tags []string) bm25Doc {
	tokens := tokenizeMemory(content)
	tagSet := make(map[string]bool)
	for _, tag := range tags {
		for _, token := range tokenizeMemory(tag) {
			tagSet[token] = true
			tokens = append(tokens, token)
		}
	}
	tf := make(map[string]int, len(tokens))
	for _, token := range tokens {
		tf[token]++
	}
	return bm25Doc{tf: tf, tags: tagSet, length: len(tokens)}
}

func (idx *bm25Index) scores(query []string) []float64 {
	out := make([]float64, len(idx.docs))
	terms := uniqueTerms(query)
	if len(terms) == 0 || len(idx.docs) == 0 || idx.avgdl == 0 {
		return out
	}
	n := float64(len(idx.docs))
	for i, doc := range idx.docs {
		score := 0.0
		for _, term := range terms {
			tf := doc.tf[term]
			if tf == 0 {
				continue
			}
			df := float64(idx.df[term])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			denom := float64(tf) + idx.k1*(1-idx.b+idx.b*float64(doc.length)/idx.avgdl)
			weight := 1.0
			if doc.tags[term] {
				weight = idx.tagBoost
			}
			score += weight * idf * float64(tf) * (idx.k1 + 1) / denom
		}
		out[i] = score
	}
	return out
}

func uniqueTerms(terms []string) []string {
	seen := make(map[string]bool, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return out
}

// tokenizeMemory tokenizes Chinese as bigrams and keeps code-like identifiers
// intact. Query and document tokenization use the same rules so BM25 scores are
// comparable.
func tokenizeMemory(text string) []string {
	var tokens []string
	var han []rune
	var word []rune

	flushWord := func() {
		if len(word) == 0 {
			return
		}
		token := strings.ToLower(strings.Trim(string(word), ".-_/"))
		if token != "" {
			tokens = append(tokens, token)
		}
		word = word[:0]
	}
	flushHan := func() {
		if len(han) == 0 {
			return
		}
		if len(han) == 1 {
			tokens = append(tokens, string(han))
		} else {
			for i := 0; i+1 < len(han); i++ {
				tokens = append(tokens, string(han[i:i+2]))
			}
		}
		han = han[:0]
	}

	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			flushWord()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) ||
			r == '_' || r == '-' || r == '.' || r == '/' || r == '#' || r == '+' || r == '@':
			flushHan()
			word = append(word, r)
		default:
			flushWord()
			flushHan()
		}
	}
	flushWord()
	flushHan()
	return tokens
}
