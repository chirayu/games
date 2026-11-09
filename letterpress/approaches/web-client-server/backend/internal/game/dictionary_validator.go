package game

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"
)

const englishDictionarySHA256 = "a64b8817f69d554f09165d461a1c115774147f3cb80790879757331f26e62558"

//go:embed dictionary/words.txt
var embeddedEnglishWords string

// DictionaryWordValidator performs immutable, concurrency-safe exact lookups.
type DictionaryWordValidator struct {
	words map[string]struct{}
	list  []string
}

var defaultEnglishValidator = mustLoadEnglishDictionary()

// EnglishDictionaryValidator returns the process-wide validated dictionary.
func EnglishDictionaryValidator() WordValidator { return defaultEnglishValidator }

// EnglishDictionaryWords returns an immutable-view copy in lexical order.
// The returned strings and Valid use the same checksum-verified source.
func EnglishDictionaryWords() []string {
	return defaultEnglishValidator.Words()
}

func mustLoadEnglishDictionary() *DictionaryWordValidator {
	validator, err := newDictionaryWordValidator(embeddedEnglishWords, englishDictionarySHA256)
	if err != nil {
		panic(fmt.Sprintf("load embedded English dictionary: %v", err))
	}
	return validator
}

func newDictionaryWordValidator(contents, expectedSHA256 string) (*DictionaryWordValidator, error) {
	digest := sha256.Sum256([]byte(contents))
	if hex.EncodeToString(digest[:]) != expectedSHA256 {
		return nil, fmt.Errorf("checksum does not match")
	}
	if contents == "" || !strings.HasSuffix(contents, "\n") {
		return nil, fmt.Errorf("dictionary must be non-empty and newline terminated")
	}
	lines := strings.Split(strings.TrimSuffix(contents, "\n"), "\n")
	words := make(map[string]struct{}, len(lines))
	previous := ""
	for index, word := range lines {
		if word == "" || (index > 0 && word <= previous) {
			return nil, fmt.Errorf("entry %d is empty, duplicated, or unsorted", index+1)
		}
		for _, letter := range []byte(word) {
			if letter < 'A' || letter > 'Z' {
				return nil, fmt.Errorf("entry %d contains a non-A-Z character", index+1)
			}
		}
		words[word] = struct{}{}
		previous = word
	}
	return &DictionaryWordValidator{words: words, list: append([]string(nil), lines...)}, nil
}

// Words returns a copy of the canonical sorted lexicon.
func (validator *DictionaryWordValidator) Words() []string {
	return append([]string(nil), validator.list...)
}

// Valid implements WordValidator with ASCII case-insensitive matching.
func (validator *DictionaryWordValidator) Valid(word string) bool {
	canonical, ok := canonicalASCIIWord(word)
	if !ok {
		return false
	}
	_, exists := validator.words[canonical]
	return exists
}

func canonicalASCIIWord(word string) (string, bool) {
	if word == "" {
		return "", false
	}
	bytes := []byte(word)
	changed := false
	for index, letter := range bytes {
		switch {
		case letter >= 'A' && letter <= 'Z':
		case letter >= 'a' && letter <= 'z':
			bytes[index] = letter - ('a' - 'A')
			changed = true
		default:
			return "", false
		}
	}
	if changed {
		return string(bytes), true
	}
	return word, true
}
