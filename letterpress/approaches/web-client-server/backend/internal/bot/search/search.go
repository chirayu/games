// Package search enumerates deterministic legal bot move candidates without
// HTTP, persistence, or mutation of authoritative game state.
package search

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"letterpress/backend/internal/game"
)

// Outcome is the simulated board state after one candidate move.
type Outcome struct {
	Owners         []game.Player
	Protected      []bool
	PlayerOneScore int
	PlayerTwoScore int
}

// Candidate contains only the ordered indexes submitted to the game API and
// derived, non-authoritative outcome data.
type Candidate struct {
	TileIndexes []int
	Outcome     Outcome
}

type indexedWord struct {
	word         string
	requirements [26]uint8
}

// Enumerator is an immutable indexed lexicon suitable for concurrent use.
type Enumerator struct {
	words []indexedWord
	set   map[string]struct{}
}

// New builds a canonical deterministic index over a controlled lexicon.
func New(words []string) (*Enumerator, error) {
	canonical := make([]string, len(words))
	for index, word := range words {
		if word == "" {
			return nil, errors.New("lexicon contains an empty word")
		}
		bytes := []byte(word)
		for position, letter := range bytes {
			switch {
			case letter >= 'A' && letter <= 'Z':
			case letter >= 'a' && letter <= 'z':
				bytes[position] = letter - ('a' - 'A')
			default:
				return nil, fmt.Errorf("lexicon word %q contains non-A-Z characters", word)
			}
		}
		canonical[index] = string(bytes)
	}
	sort.Strings(canonical)
	entries := make([]indexedWord, 0, len(canonical))
	set := make(map[string]struct{}, len(canonical))
	for _, word := range canonical {
		if _, duplicate := set[word]; duplicate {
			continue
		}
		entry := indexedWord{word: word}
		for _, letter := range []byte(word) {
			entry.requirements[letter-'A']++
		}
		entries = append(entries, entry)
		set[word] = struct{}{}
	}
	return &Enumerator{words: entries, set: set}, nil
}

// NewEnglish builds an enumerator over the exact embedded authoritative lexicon.
func NewEnglish() (*Enumerator, error) {
	return New(game.EnglishDictionaryWords())
}

// Valid implements game.WordValidator over the same enumerable words.
func (enumerator *Enumerator) Valid(word string) bool {
	canonical := strings.ToUpper(word)
	if canonical == "" {
		return false
	}
	for _, letter := range []byte(canonical) {
		if letter < 'A' || letter > 'Z' {
			return false
		}
	}
	_, exists := enumerator.set[canonical]
	return exists
}

// Candidates returns one deterministic, ownership-optimized tile assignment
// for every legal formable word. It never mutates snapshot slices.
func (enumerator *Enumerator) Candidates(snapshot game.Snapshot) ([]Candidate, error) {
	if _, err := game.Restore(snapshot, enumerator); err != nil {
		return nil, err
	}
	if snapshot.Status == game.StatusCompleted {
		return []Candidate{}, nil
	}
	analysis, err := game.AnalyzeOwnership(snapshot.Width, snapshot.Height, snapshot.Owners)
	if err != nil {
		return nil, err
	}
	available := [26]uint8{}
	byLetter := [26][]int{}
	for index, letter := range snapshot.Letters {
		if letter < 'A' || letter > 'Z' {
			return nil, game.ErrInvalidBoard
		}
		available[letter-'A']++
		byLetter[letter-'A'] = append(byLetter[letter-'A'], index)
	}
	for letter := range byLetter {
		sort.SliceStable(byLetter[letter], func(left, right int) bool {
			return tilePriority(snapshot.Owners[byLetter[letter][left]], analysis.Protected[byLetter[letter][left]], snapshot.CurrentPlayer, byLetter[letter][left]) <
				tilePriority(snapshot.Owners[byLetter[letter][right]], analysis.Protected[byLetter[letter][right]], snapshot.CurrentPlayer, byLetter[letter][right])
		})
	}
	played := make([]string, 0, len(snapshot.Moves))
	for _, move := range snapshot.Moves {
		played = append(played, strings.ToUpper(move.Word))
	}
	candidates := make([]Candidate, 0)
	for _, entry := range enumerator.words {
		if len(entry.word) > len(snapshot.Letters) || !formable(entry.requirements, available) || prohibited(entry.word, played) {
			continue
		}
		used := [26]int{}
		indexes := make([]int, len(entry.word))
		for position, letter := range []byte(entry.word) {
			letterIndex := letter - 'A'
			indexes[position] = byLetter[letterIndex][used[letterIndex]]
			used[letterIndex]++
		}
		outcome, err := simulate(snapshot, analysis.Protected, indexes)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, Candidate{TileIndexes: indexes, Outcome: outcome})
	}
	return candidates, nil
}

func formable(required, available [26]uint8) bool {
	for index, count := range required {
		if count > available[index] {
			return false
		}
	}
	return true
}

func prohibited(word string, played []string) bool {
	for _, previous := range played {
		if previous == word || strings.HasPrefix(previous, word) {
			return true
		}
	}
	return false
}

func tilePriority(owner game.Player, protected bool, active game.Player, index int) int {
	class := 3
	switch {
	case owner == game.Neutral:
		class = 0
	case owner != active && !protected:
		class = 1
	case owner == active:
		class = 2
	}
	return class*1000 + index
}

func simulate(snapshot game.Snapshot, protected []bool, indexes []int) (Outcome, error) {
	owners := append([]game.Player(nil), snapshot.Owners...)
	for _, index := range indexes {
		if !protected[index] {
			owners[index] = snapshot.CurrentPlayer
		}
	}
	analysis, err := game.AnalyzeOwnership(snapshot.Width, snapshot.Height, owners)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		Owners: owners, Protected: analysis.Protected,
		PlayerOneScore: analysis.PlayerOneScore, PlayerTwoScore: analysis.PlayerTwoScore,
	}, nil
}
