package game

import (
	"errors"
	"reflect"
	"testing"
)

func TestAnalyzeOwnershipDerivesProtectionAndScores(t *testing.T) {
	owners := []Player{
		PlayerOne, PlayerOne, Neutral,
		PlayerOne, Neutral, PlayerTwo,
		Neutral, PlayerTwo, PlayerTwo,
	}
	analysis, err := AnalyzeOwnership(3, 3, owners)
	if err != nil {
		t.Fatalf("analyze ownership: %v", err)
	}
	if analysis.PlayerOneScore != 3 || analysis.PlayerTwoScore != 3 {
		t.Fatalf("scores = %d/%d, want 3/3", analysis.PlayerOneScore, analysis.PlayerTwoScore)
	}
	wantProtected := []bool{true, false, false, false, false, false, false, false, true}
	if !reflect.DeepEqual(analysis.Protected, wantProtected) {
		t.Fatalf("protected = %v, want %v", analysis.Protected, wantProtected)
	}
}

func TestAnalyzeOwnershipRejectsInvalidState(t *testing.T) {
	if _, err := AnalyzeOwnership(2, 2, []Player{Neutral}); !errors.Is(err, ErrInvalidBoard) {
		t.Fatalf("dimension error = %v, want invalid board", err)
	}
	if _, err := AnalyzeOwnership(1, 1, []Player{Player(3)}); !errors.Is(err, ErrInvalidPlayer) {
		t.Fatalf("owner error = %v, want invalid player", err)
	}
}
