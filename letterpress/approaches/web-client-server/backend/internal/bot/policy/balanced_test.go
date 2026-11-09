package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"letterpress/backend/internal/bot/search"
	"letterpress/backend/internal/game"
)

func TestScoreCandidateCaptureAndNeutralExpansion(t *testing.T) {
	snapshot := game.Snapshot{Width: 3, Height: 1, Owners: []game.Player{game.Neutral, game.PlayerTwo, game.PlayerOne}, CurrentPlayer: game.PlayerOne}
	neutral := candidateWithOwners(t, snapshot, []int{0}, []game.Player{game.PlayerOne, game.PlayerTwo, game.PlayerOne})
	capture := candidateWithOwners(t, snapshot, []int{1}, []game.Player{game.Neutral, game.PlayerOne, game.PlayerOne})

	neutralScore, err := ScoreCandidate(snapshot, neutral)
	if err != nil || neutralScore.NeutralCapture != 1 || neutralScore.OpponentCapture != 0 {
		t.Fatalf("neutral score = %#v, %v", neutralScore, err)
	}
	captureScore, err := ScoreCandidate(snapshot, capture)
	if err != nil || captureScore.OpponentCapture != 1 || captureScore.NeutralCapture != 0 {
		t.Fatalf("capture score = %#v, %v", captureScore, err)
	}
	if captureScore.Total <= neutralScore.Total {
		t.Fatalf("opponent capture score %d should exceed neutral expansion %d", captureScore.Total, neutralScore.Total)
	}
}

func TestScoreCandidateDefenseAndBreakingProtection(t *testing.T) {
	defenseSnapshot := game.Snapshot{
		Width: 3, Height: 3, CurrentPlayer: game.PlayerOne,
		Owners: []game.Player{
			game.Neutral, game.PlayerOne, game.Neutral,
			game.PlayerOne, game.Neutral, game.PlayerOne,
			game.Neutral, game.PlayerOne, game.Neutral,
		},
	}
	defenseOwners := append([]game.Player(nil), defenseSnapshot.Owners...)
	defenseOwners[4] = game.PlayerOne
	defense := candidateWithOwners(t, defenseSnapshot, []int{4}, defenseOwners)
	defenseScore, err := ScoreCandidate(defenseSnapshot, defense)
	if err != nil || defenseScore.ProtectionGained == 0 {
		t.Fatalf("defense score = %#v, %v", defenseScore, err)
	}

	breakSnapshot := game.Snapshot{
		Width: 3, Height: 3, CurrentPlayer: game.PlayerOne,
		Owners: []game.Player{
			game.Neutral, game.PlayerTwo, game.Neutral,
			game.PlayerTwo, game.PlayerTwo, game.PlayerTwo,
			game.Neutral, game.PlayerTwo, game.Neutral,
		},
	}
	breakOwners := append([]game.Player(nil), breakSnapshot.Owners...)
	breakOwners[1] = game.PlayerOne
	broken := candidateWithOwners(t, breakSnapshot, []int{1}, breakOwners)
	breakScore, err := ScoreCandidate(breakSnapshot, broken)
	if err != nil || breakScore.OpponentProtectionBroken == 0 {
		t.Fatalf("break score = %#v, %v", breakScore, err)
	}
}

func TestBalancedSelectionIsReproducibleBoundedAndStructured(t *testing.T) {
	snapshot := game.Snapshot{
		Width: 3, Height: 1, Letters: []byte("ABC"), Owners: make([]game.Player, 3),
		CurrentPlayer: game.PlayerOne, Status: game.StatusActive,
	}
	candidates := make([]search.Candidate, 3)
	for index := range candidates {
		owners := make([]game.Player, 3)
		owners[index] = game.PlayerOne
		candidates[index] = candidateWithOwners(t, snapshot, []int{index}, owners)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	selector, err := NewBalanced(staticSource{candidates: candidates}, logger)
	if err != nil {
		t.Fatal(err)
	}
	seed := Seed{GameID: "game-1", ProfileVersion: 1, TurnNumber: 1}
	first, err := selector.Select(context.Background(), snapshot, seed)
	if err != nil {
		t.Fatal(err)
	}
	second, err := selector.Select(context.Background(), snapshot, seed)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated selection = %v and %v, err %v", first, second, err)
	}
	if len(first) != 1 || first[0] < 0 || first[0] > 2 {
		t.Fatalf("selection %v is not from legal candidates", first)
	}
	if withinQuality(84, 100) || !withinQuality(85, 100) {
		t.Fatal("quality threshold is not an inclusive 85 percent")
	}
	var event map[string]any
	if err := json.Unmarshal(bytes.Split(output.Bytes(), []byte("\n"))[0], &event); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"policy", "policy_version", "duration_ms", "candidate_count", "selected_rank"} {
		if _, exists := event[field]; !exists {
			t.Fatalf("telemetry missing %q: %v", field, event)
		}
	}
	if _, exposed := event["game_id"]; exposed {
		t.Fatal("telemetry exposed game seed input")
	}
}

func TestBalancedPolicyCompletesRepresentativeGame(t *testing.T) {
	words := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}
	enumerator, err := search.New(words)
	if err != nil {
		t.Fatal(err)
	}
	selector, _ := NewBalanced(enumerator, nil)
	letterGame, err := game.New(3, 3, fixedGenerator{letters: []byte("ABCDEFGHI")}, enumerator)
	if err != nil {
		t.Fatal(err)
	}
	for turn := 1; letterGame.Status() == game.StatusActive; turn++ {
		if turn > 9 {
			t.Fatal("representative game did not complete")
		}
		snapshot := snapshotFromGame(letterGame)
		indexes, selectErr := selector.Select(context.Background(), snapshot, Seed{GameID: "simulation", ProfileVersion: 1, TurnNumber: turn})
		if selectErr != nil {
			t.Fatal(selectErr)
		}
		if _, playErr := letterGame.Play(letterGame.CurrentPlayer(), indexes); playErr != nil {
			t.Fatalf("turn %d selected invalid move %v: %v", turn, indexes, playErr)
		}
	}
	if len(letterGame.Moves()) != 9 {
		t.Fatalf("completed after %d moves, want 9", len(letterGame.Moves()))
	}
}

func TestBalancedPolicyNoLegalMove(t *testing.T) {
	selector, _ := NewBalanced(staticSource{}, nil)
	_, err := selector.Select(context.Background(), game.Snapshot{}, Seed{GameID: "g", ProfileVersion: 1, TurnNumber: 1})
	if !errors.Is(err, ErrNoLegalMove) {
		t.Fatalf("Select error = %v, want ErrNoLegalMove", err)
	}
}

func BenchmarkBalancedDecision5x5(b *testing.B) {
	enumerator, err := search.NewEnglish()
	if err != nil {
		b.Fatal(err)
	}
	selector, _ := NewBalanced(enumerator, nil)
	snapshot := game.Snapshot{
		Width: 5, Height: 5, Letters: []byte("ETAOINSHRDLUCMFWYPVBGKQJX"), Owners: make([]game.Player, 25),
		CurrentPlayer: game.PlayerOne, Status: game.StatusActive,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for turn := 1; turn <= b.N; turn++ {
		if _, err := selector.Select(context.Background(), snapshot, Seed{GameID: "benchmark", ProfileVersion: 1, TurnNumber: turn}); err != nil {
			b.Fatal(err)
		}
	}
}

type staticSource struct {
	candidates []search.Candidate
	err        error
}

func (source staticSource) Candidates(game.Snapshot) ([]search.Candidate, error) {
	return source.candidates, source.err
}

type fixedGenerator struct{ letters []byte }

func (generator fixedGenerator) Generate(width, height int) ([]byte, error) {
	return append([]byte(nil), generator.letters...), nil
}

func candidateWithOwners(t *testing.T, snapshot game.Snapshot, indexes []int, owners []game.Player) search.Candidate {
	t.Helper()
	analysis, err := game.AnalyzeOwnership(snapshot.Width, snapshot.Height, owners)
	if err != nil {
		t.Fatal(err)
	}
	return search.Candidate{
		TileIndexes: append([]int(nil), indexes...),
		Outcome: search.Outcome{
			Owners: owners, Protected: analysis.Protected,
			PlayerOneScore: analysis.PlayerOneScore, PlayerTwoScore: analysis.PlayerTwoScore,
		},
	}
}

func snapshotFromGame(letterGame *game.Game) game.Snapshot {
	board := letterGame.Board()
	tiles := board.Tiles()
	owners := make([]game.Player, len(tiles))
	for index, tile := range tiles {
		owners[index] = tile.Owner
	}
	return game.Snapshot{
		Width: board.Width(), Height: board.Height(), Letters: board.Letters(), Owners: owners,
		CurrentPlayer: letterGame.CurrentPlayer(), Status: letterGame.Status(), Moves: letterGame.Moves(),
	}
}
