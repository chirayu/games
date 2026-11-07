// Package game implements the Letterpress rules without HTTP or persistence
// dependencies.
package game

import "errors"

const (
	// DefaultWidth and DefaultHeight define the MVP board size.
	DefaultWidth  = 5
	DefaultHeight = 5
)

// Player identifies a tile owner or player seat.
type Player uint8

const (
	Neutral Player = iota
	PlayerOne
	PlayerTwo
)

// Status describes whether moves may still be played.
type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

var (
	ErrGameCompleted    = errors.New("game is completed")
	ErrOutOfTurn        = errors.New("move is out of turn")
	ErrInvalidPlayer    = errors.New("invalid player")
	ErrInvalidTile      = errors.New("tile index is out of range")
	ErrDuplicateTile    = errors.New("tile selected more than once")
	ErrInvalidWord      = errors.New("word is invalid")
	ErrRepeatedWord     = errors.New("word has already been played")
	ErrPrefixWord       = errors.New("word is a prefix of a previously played word")
	ErrInvalidBoard     = errors.New("board is invalid")
	ErrInvalidConfig    = errors.New("game configuration is invalid")
	ErrGenerationFailed = errors.New("board generation failed")
)

// Tile is a snapshot of one board tile.
type Tile struct {
	Letter    byte
	Owner     Player
	Protected bool
}

// Move is an accepted move derived by the game engine.
type Move struct {
	Player      Player
	TileIndexes []int
	Word        string
	TurnNumber  int
}

// Result is available once a game is complete. Winner is Neutral for a tie.
type Result struct {
	PlayerOneScore int
	PlayerTwoScore int
	Winner         Player
	Tie            bool
}
