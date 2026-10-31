package game

import (
	"fmt"
	"strings"
)

// Snapshot contains the persisted domain state needed to resume a game.
type Snapshot struct {
	Width         int
	Height        int
	Letters       []byte
	Owners        []Player
	CurrentPlayer Player
	Status        Status
	Moves         []Move
}

// Restore reconstructs a game from authoritative persisted state.
func Restore(snapshot Snapshot, validator WordValidator) (*Game, error) {
	if validator == nil {
		return nil, fmt.Errorf("%w: validator is required", ErrInvalidConfig)
	}
	board, err := newBoard(snapshot.Width, snapshot.Height, snapshot.Letters)
	if err != nil {
		return nil, err
	}
	if len(snapshot.Owners) != board.Len() {
		return nil, fmt.Errorf("%w: ownership dimensions do not match", ErrInvalidBoard)
	}
	for index, owner := range snapshot.Owners {
		if owner != Neutral && owner != PlayerOne && owner != PlayerTwo {
			return nil, fmt.Errorf("%w: tile %d has owner %d", ErrInvalidPlayer, index, owner)
		}
		board.tiles[index].Owner = owner
	}
	board.recalculateProtection()

	if snapshot.CurrentPlayer != PlayerOne && snapshot.CurrentPlayer != PlayerTwo {
		return nil, fmt.Errorf("%w: current player %d", ErrInvalidPlayer, snapshot.CurrentPlayer)
	}
	if snapshot.Status != StatusActive && snapshot.Status != StatusCompleted {
		return nil, fmt.Errorf("%w: unknown status %q", ErrInvalidConfig, snapshot.Status)
	}

	restored := &Game{
		board: board, currentPlayer: snapshot.CurrentPlayer, status: snapshot.Status,
		validator: validator, moves: make([]Move, 0, len(snapshot.Moves)),
	}
	for index, move := range snapshot.Moves {
		if move.TurnNumber != index+1 {
			return nil, fmt.Errorf("%w: move %d has turn number %d", ErrInvalidConfig, index, move.TurnNumber)
		}
		expectedPlayer := PlayerOne
		if index%2 == 1 {
			expectedPlayer = PlayerTwo
		}
		if move.Player != expectedPlayer {
			return nil, fmt.Errorf("%w: turn %d belongs to player %d", ErrInvalidPlayer, move.TurnNumber, move.Player)
		}
		word, err := restored.deriveWord(move.TileIndexes)
		if err != nil {
			return nil, fmt.Errorf("restore turn %d: %w", move.TurnNumber, err)
		}
		if word == "" || word != move.Word {
			return nil, fmt.Errorf("%w: turn %d word does not match its tiles", ErrInvalidWord, move.TurnNumber)
		}
		for _, previous := range restored.moves {
			if previous.Word == move.Word {
				return nil, fmt.Errorf("%w: turn %d", ErrRepeatedWord, move.TurnNumber)
			}
			if strings.HasPrefix(previous.Word, move.Word) {
				return nil, fmt.Errorf("%w: turn %d", ErrPrefixWord, move.TurnNumber)
			}
		}
		restored.moves = append(restored.moves, cloneMove(move))
	}

	if snapshot.Status == StatusActive {
		expectedPlayer := PlayerOne
		if len(snapshot.Moves)%2 == 1 {
			expectedPlayer = PlayerTwo
		}
		if snapshot.CurrentPlayer != expectedPlayer || restored.boardComplete() {
			return nil, fmt.Errorf("%w: active state is inconsistent with move history", ErrInvalidConfig)
		}
	} else {
		if !restored.boardComplete() || len(snapshot.Moves) == 0 || snapshot.CurrentPlayer != snapshot.Moves[len(snapshot.Moves)-1].Player {
			return nil, fmt.Errorf("%w: completed state is inconsistent with move history", ErrInvalidConfig)
		}
		restored.complete()
	}
	return restored, nil
}
