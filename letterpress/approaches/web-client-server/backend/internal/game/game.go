package game

import (
	"fmt"
	"strings"
)

// Game owns the authoritative rules and mutable state for one game.
type Game struct {
	board         Board
	currentPlayer Player
	status        Status
	moves         []Move
	validator     WordValidator
	result        *Result
}

// NewDefault creates a 5x5 game using secure random letters and the embedded
// English dictionary.
func NewDefault() (*Game, error) {
	return New(DefaultWidth, DefaultHeight, NewCryptoBoardGenerator(), EnglishDictionaryValidator())
}

// New creates an active game with a configurable board and replaceable board
// generation and word-validation strategies.
func New(width, height int, generator BoardGenerator, validator WordValidator) (*Game, error) {
	if generator == nil {
		return nil, fmt.Errorf("%w: generator is required", ErrInvalidConfig)
	}
	if validator == nil {
		return nil, fmt.Errorf("%w: validator is required", ErrInvalidConfig)
	}
	letters, err := generator.Generate(width, height)
	if err != nil {
		return nil, err
	}
	board, err := newBoard(width, height, letters)
	if err != nil {
		return nil, err
	}
	return &Game{board: board, currentPlayer: PlayerOne, status: StatusActive, validator: validator}, nil
}

// Board returns a snapshot of the current board.
func (game *Game) Board() Board { return game.board.clone() }

// CurrentPlayer returns the seat whose turn it is.
func (game *Game) CurrentPlayer() Player { return game.currentPlayer }

// Status returns the game's current status.
func (game *Game) Status() Status { return game.status }

// Moves returns a deep copy of accepted moves in turn order.
func (game *Game) Moves() []Move {
	moves := make([]Move, len(game.moves))
	for index, move := range game.moves {
		moves[index] = cloneMove(move)
	}
	return moves
}

// Outcome reports the final result after completion.
func (game *Game) Outcome() (Result, bool) {
	if game.result == nil {
		return Result{}, false
	}
	return *game.result, true
}

// Play validates and applies a selection of ordered, non-adjacent tile indexes.
func (game *Game) Play(player Player, tileIndexes []int) (Move, error) {
	if game.status == StatusCompleted {
		return Move{}, ErrGameCompleted
	}
	if player != PlayerOne && player != PlayerTwo {
		return Move{}, ErrInvalidPlayer
	}
	if player != game.currentPlayer {
		return Move{}, ErrOutOfTurn
	}

	word, err := game.deriveWord(tileIndexes)
	if err != nil {
		return Move{}, err
	}
	if !game.validator.Valid(word) {
		return Move{}, ErrInvalidWord
	}
	for _, previous := range game.moves {
		if previous.Word == word {
			return Move{}, ErrRepeatedWord
		}
		if strings.HasPrefix(previous.Word, word) {
			return Move{}, ErrPrefixWord
		}
	}

	for _, tileIndex := range tileIndexes {
		if !game.board.tiles[tileIndex].Protected {
			game.board.tiles[tileIndex].Owner = player
		}
	}
	game.board.recalculateProtection()

	move := Move{
		Player: player, TileIndexes: append([]int(nil), tileIndexes...),
		Word: word, TurnNumber: len(game.moves) + 1,
	}
	game.moves = append(game.moves, move)
	if game.boardComplete() {
		game.complete()
	} else if player == PlayerOne {
		game.currentPlayer = PlayerTwo
	} else {
		game.currentPlayer = PlayerOne
	}
	return cloneMove(move), nil
}

func (game *Game) deriveWord(tileIndexes []int) (string, error) {
	seen := make(map[int]struct{}, len(tileIndexes))
	letters := make([]byte, len(tileIndexes))
	for position, tileIndex := range tileIndexes {
		if tileIndex < 0 || tileIndex >= len(game.board.tiles) {
			return "", fmt.Errorf("%w: %d", ErrInvalidTile, tileIndex)
		}
		if _, exists := seen[tileIndex]; exists {
			return "", fmt.Errorf("%w: %d", ErrDuplicateTile, tileIndex)
		}
		seen[tileIndex] = struct{}{}
		letters[position] = game.board.tiles[tileIndex].Letter
	}
	return string(letters), nil
}

func (game *Game) boardComplete() bool {
	for _, tile := range game.board.tiles {
		if tile.Owner == Neutral {
			return false
		}
	}
	return true
}

func (game *Game) complete() {
	result := Result{}
	for _, tile := range game.board.tiles {
		switch tile.Owner {
		case PlayerOne:
			result.PlayerOneScore++
		case PlayerTwo:
			result.PlayerTwoScore++
		}
	}
	switch {
	case result.PlayerOneScore > result.PlayerTwoScore:
		result.Winner = PlayerOne
	case result.PlayerTwoScore > result.PlayerOneScore:
		result.Winner = PlayerTwo
	default:
		result.Tie = true
	}
	game.status = StatusCompleted
	game.result = &result
}

func cloneMove(move Move) Move {
	move.TileIndexes = append([]int(nil), move.TileIndexes...)
	return move
}
