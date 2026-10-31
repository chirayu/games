package game

import (
	"errors"
	"testing"
)

func TestRestoreResumesRulesFromPersistedHistory(t *testing.T) {
	restored, err := Restore(Snapshot{
		Width: 5, Height: 1, Letters: []byte("DOGSX"),
		Owners:        []Player{PlayerOne, PlayerOne, Neutral, Neutral, Neutral},
		CurrentPlayer: PlayerTwo, Status: StatusActive,
		Moves: []Move{{Player: PlayerOne, TileIndexes: []int{0, 1}, Word: "DO", TurnNumber: 1}},
	}, MVPWordValidator{})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := restored.Play(PlayerTwo, []int{0, 1}); !errors.Is(err, ErrRepeatedWord) {
		t.Fatalf("repeated word error = %v, want repeated word", err)
	}
	move, err := restored.Play(PlayerTwo, []int{2, 3})
	if err != nil {
		t.Fatalf("play after restore: %v", err)
	}
	if move.Word != "GS" || move.TurnNumber != 2 || restored.CurrentPlayer() != PlayerOne {
		t.Fatalf("move/current player = %#v/%d", move, restored.CurrentPlayer())
	}
}

func TestRestoreRejectsInconsistentState(t *testing.T) {
	_, err := Restore(Snapshot{
		Width: 2, Height: 1, Letters: []byte("AB"), Owners: []Player{Neutral, Neutral},
		CurrentPlayer: PlayerTwo, Status: StatusActive,
	}, MVPWordValidator{})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want invalid config", err)
	}
}
