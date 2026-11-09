package search

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"letterpress/backend/internal/game"
)

func TestCandidatesMatchBruteForceAndAuthoritativeGame(t *testing.T) {
	words := []string{"LETTER", "TREE", "TE", "LET", "BETA", "CAB", "BEER", "TELL", "ZZZ"}
	enumerator, err := New(words)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := game.Snapshot{
		Width: 3, Height: 3, Letters: []byte("LETTERABC"), Owners: make([]game.Player, 9),
		CurrentPlayer: game.PlayerOne, Status: game.StatusActive,
	}
	originalLetters := append([]byte(nil), snapshot.Letters...)
	originalOwners := append([]game.Player(nil), snapshot.Owners...)
	candidates, err := enumerator.Candidates(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	again, err := enumerator.Candidates(snapshot)
	if err != nil || !reflect.DeepEqual(candidates, again) {
		t.Fatalf("candidate generation is not deterministic: %v", err)
	}
	if !reflect.DeepEqual(snapshot.Letters, originalLetters) || !reflect.DeepEqual(snapshot.Owners, originalOwners) {
		t.Fatal("candidate generation mutated the snapshot")
	}

	gotWords := candidateWords(snapshot.Letters, candidates)
	wantWords := bruteFormableWords(words, snapshot.Letters, nil)
	if !reflect.DeepEqual(gotWords, wantWords) {
		t.Fatalf("candidate words = %v, want %v", gotWords, wantWords)
	}
	for _, candidate := range candidates {
		seen := map[int]bool{}
		for _, index := range candidate.TileIndexes {
			if seen[index] {
				t.Fatalf("candidate reused tile %d: %#v", index, candidate)
			}
			seen[index] = true
		}
		authoritative, err := game.Restore(snapshot, enumerator)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authoritative.Play(snapshot.CurrentPlayer, candidate.TileIndexes); err != nil {
			t.Fatalf("authoritative engine rejected %v: %v", candidate.TileIndexes, err)
		}
		board := authoritative.Board()
		tiles := board.Tiles()
		owners := make([]game.Player, len(tiles))
		protected := make([]bool, len(tiles))
		for index, tile := range tiles {
			owners[index], protected[index] = tile.Owner, tile.Protected
		}
		if !reflect.DeepEqual(owners, candidate.Outcome.Owners) || !reflect.DeepEqual(protected, candidate.Outcome.Protected) {
			t.Fatalf("simulated outcome differs for %v", candidate.TileIndexes)
		}
	}
}

func TestCandidatesFilterHistoryAndPrefixes(t *testing.T) {
	enumerator, _ := New([]string{"LETTER", "LET", "LE", "TREE", "TE"})
	snapshot := game.Snapshot{
		Width: 3, Height: 3, Letters: []byte("LETTERABC"), Owners: make([]game.Player, 9),
		CurrentPlayer: game.PlayerTwo, Status: game.StatusActive,
		Moves: []game.Move{{Player: game.PlayerOne, TileIndexes: []int{0, 1, 2, 3, 4, 5}, Word: "LETTER", TurnNumber: 1}},
	}
	candidates, err := enumerator.Candidates(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	got := candidateWords(snapshot.Letters, candidates)
	want := bruteFormableWords([]string{"LETTER", "LET", "LE", "TREE", "TE"}, snapshot.Letters, []string{"LETTER"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history-filtered words = %v, want %v", got, want)
	}
}

func TestAssignmentOptimizesOwnershipAndHonorsProtection(t *testing.T) {
	enumerator, _ := New([]string{"A", "E", "AA"})
	owners := []game.Player{
		game.PlayerOne, game.PlayerTwo, game.Neutral,
		game.PlayerTwo, game.PlayerTwo, game.PlayerTwo,
		game.Neutral, game.PlayerTwo, game.Neutral,
	}
	snapshot := game.Snapshot{Width: 3, Height: 3, Letters: []byte("AABCEFAAI"), Owners: owners, CurrentPlayer: game.PlayerOne, Status: game.StatusActive}
	candidates, err := enumerator.Candidates(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	byWord := map[string]Candidate{}
	for _, candidate := range candidates {
		byWord[wordFor(snapshot.Letters, candidate.TileIndexes)] = candidate
	}
	if got := byWord["A"].TileIndexes; !reflect.DeepEqual(got, []int{6}) {
		t.Fatalf("single A assignment = %v, want neutral tile 6", got)
	}
	if got := byWord["AA"].TileIndexes; !reflect.DeepEqual(got, []int{6, 1}) {
		t.Fatalf("repeated A assignment = %v, want neutral then capturable opponent tile", got)
	}
	if byWord["E"].Outcome.Owners[4] != game.PlayerTwo {
		t.Fatal("protected opponent center changed ownership in simulation")
	}
}

func TestCompletedGameHasNoCandidates(t *testing.T) {
	enumerator, _ := New([]string{"A"})
	snapshot := game.Snapshot{
		Width: 1, Height: 1, Letters: []byte("A"), Owners: []game.Player{game.PlayerOne},
		CurrentPlayer: game.PlayerOne, Status: game.StatusCompleted,
		Moves: []game.Move{{Player: game.PlayerOne, TileIndexes: []int{0}, Word: "A", TurnNumber: 1}},
	}
	candidates, err := enumerator.Candidates(snapshot)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("completed candidates = %#v, err %v", candidates, err)
	}
}

func BenchmarkEnglishCandidates5x5(b *testing.B) {
	enumerator, err := NewEnglish()
	if err != nil {
		b.Fatal(err)
	}
	snapshot := game.Snapshot{
		Width: 5, Height: 5, Letters: []byte("ETAOINSHRDLUCMFWYPVBGKQJX"),
		Owners: make([]game.Player, 25), CurrentPlayer: game.PlayerOne, Status: game.StatusActive,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := enumerator.Candidates(snapshot); err != nil {
			b.Fatal(err)
		}
	}
}

func candidateWords(letters []byte, candidates []Candidate) []string {
	words := make([]string, len(candidates))
	for index, candidate := range candidates {
		words[index] = wordFor(letters, candidate.TileIndexes)
	}
	return words
}

func wordFor(letters []byte, indexes []int) string {
	word := make([]byte, len(indexes))
	for position, index := range indexes {
		word[position] = letters[index]
	}
	return string(word)
}

func bruteFormableWords(words []string, letters []byte, played []string) []string {
	available := map[byte]int{}
	for _, letter := range letters {
		available[letter]++
	}
	result := []string{}
	for _, candidate := range words {
		word := strings.ToUpper(candidate)
		needed := map[byte]int{}
		for _, letter := range []byte(word) {
			needed[letter]++
		}
		formable := true
		for letter, count := range needed {
			if count > available[letter] {
				formable = false
			}
		}
		if !formable || prohibited(word, played) {
			continue
		}
		result = append(result, word)
	}
	sort.Strings(result)
	return result
}
