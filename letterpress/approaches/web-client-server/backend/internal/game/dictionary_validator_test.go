package game

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestEnglishDictionaryValidatorAcceptsCanonicalWords(t *testing.T) {
	validator := EnglishDictionaryValidator()
	for _, word := range []string{"CAT", "dog", "extraordinary", "Q"} {
		if !validator.Valid(word) {
			t.Errorf("Valid(%q) = false, want true", word)
		}
	}
	for _, word := range []string{"", "QQQQQ", "can't", "CAFÉ", "DOG-"} {
		if validator.Valid(word) {
			t.Errorf("Valid(%q) = true, want false", word)
		}
	}
}

func TestDictionaryTreatsQAsALiteralTile(t *testing.T) {
	letterGame, err := New(1, 1, fixedGenerator{letters: []byte("Q")}, EnglishDictionaryValidator())
	if err != nil {
		t.Fatalf("new Q game: %v", err)
	}
	move, err := letterGame.Play(PlayerOne, []int{0})
	if err != nil {
		t.Fatalf("play literal Q: %v", err)
	}
	if move.Word != "Q" {
		t.Fatalf("derived word = %q, want Q", move.Word)
	}
}

func TestEnglishDictionaryWordsIsAnImmutableEnumerableView(t *testing.T) {
	words := EnglishDictionaryWords()
	if len(words) == 0 {
		t.Fatal("EnglishDictionaryWords returned no words")
	}
	first := words[0]
	if !EnglishDictionaryValidator().Valid(first) {
		t.Fatalf("enumerated word %q is not valid", first)
	}
	words[0] = "MUTATED"
	if got := EnglishDictionaryWords()[0]; got != first {
		t.Fatalf("mutating returned words changed embedded lexicon: got %q, want %q", got, first)
	}
}

func TestDictionaryParserRejectsMalformedArtifacts(t *testing.T) {
	for _, contents := range []string{"", "DOG\nCAT\n", "CAT\nCAT\n", "CAFÉ\n", "CAT"} {
		digest := sha256.Sum256([]byte(contents))
		if _, err := newDictionaryWordValidator(contents, hex.EncodeToString(digest[:])); err == nil {
			t.Errorf("newDictionaryWordValidator(%q) succeeded", contents)
		}
	}
	if _, err := newDictionaryWordValidator("CAT\n", "wrong"); err == nil {
		t.Fatal("newDictionaryWordValidator accepted wrong checksum")
	}
}
