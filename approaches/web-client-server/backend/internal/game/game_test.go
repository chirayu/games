package game

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
)

type fixedGenerator struct {
	letters []byte
	err     error
}

func (generator fixedGenerator) Generate(_, _ int) ([]byte, error) {
	return append([]byte(nil), generator.letters...), generator.err
}

type rejectingValidator struct{}

func (rejectingValidator) Valid(string) bool { return false }

func TestNewDefaultCreatesFiveByFiveBoard(t *testing.T) {
	game, err := NewDefault()
	if err != nil {
		t.Fatalf("new default game: %v", err)
	}
	board := game.Board()
	if board.Width() != 5 || board.Height() != 5 || board.Len() != 25 {
		t.Fatalf("default board dimensions = %dx%d (%d tiles), want 5x5 (25 tiles)", board.Width(), board.Height(), board.Len())
	}
	for index, letter := range board.Letters() {
		if letter < 'A' || letter > 'Z' {
			t.Fatalf("letter %d = %q, want A-Z", index, letter)
		}
	}
	if game.CurrentPlayer() != PlayerOne || game.Status() != StatusActive {
		t.Fatalf("new game player/status = %d/%q, want PlayerOne/active", game.CurrentPlayer(), game.Status())
	}
}

func TestCryptoBoardGeneratorUsesUnbiasedLiteralLetters(t *testing.T) {
	// 234 is rejected; the remaining bytes map literally to A, Z, and B.
	generator := newCryptoBoardGenerator(bytes.NewReader([]byte{0, 25, 234, 1}))
	letters, err := generator.Generate(3, 1)
	if err != nil {
		t.Fatalf("generate board: %v", err)
	}
	if string(letters) != "AZB" {
		t.Fatalf("letters = %q, want AZB", letters)
	}
}

func TestCryptoBoardGeneratorReportsEntropyFailure(t *testing.T) {
	generator := newCryptoBoardGenerator(bytes.NewReader(nil))
	_, err := generator.Generate(1, 1)
	if !errors.Is(err, ErrGenerationFailed) || !errors.Is(err, io.EOF) {
		t.Fatalf("error = %v, want generation failure wrapping EOF", err)
	}
}

func TestFixedBoardGeneratorIsValidatedAndDeterministic(t *testing.T) {
	generator, err := NewFixedBoardGenerator("ABCD")
	if err != nil {
		t.Fatalf("new fixed generator: %v", err)
	}
	first, err := generator.Generate(2, 2)
	if err != nil {
		t.Fatalf("generate fixed board: %v", err)
	}
	first[0] = 'Z'
	second, err := generator.Generate(2, 2)
	if err != nil {
		t.Fatalf("regenerate fixed board: %v", err)
	}
	if string(second) != "ABCD" {
		t.Fatalf("fixed board = %q, want ABCD", second)
	}
	if _, err := NewFixedBoardGenerator("ABC1"); !errors.Is(err, ErrInvalidBoard) {
		t.Fatalf("invalid letter error = %v, want ErrInvalidBoard", err)
	}
	if _, err := generator.Generate(3, 2); !errors.Is(err, ErrInvalidBoard) {
		t.Fatalf("dimension error = %v, want ErrInvalidBoard", err)
	}
}

func TestPlayDerivesLiteralWordFromOrderedNonAdjacentTiles(t *testing.T) {
	letters := []byte("QBCDEFGHIJKLMNOPQRSTUVWXY")
	game := newTestGame(t, 5, 5, letters)

	move, err := game.Play(PlayerOne, []int{0, 24})
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if move.Word != "QY" {
		t.Fatalf("word = %q, want QY", move.Word)
	}
	if !reflect.DeepEqual(move.TileIndexes, []int{0, 24}) {
		t.Fatalf("tile indexes = %v, want [0 24]", move.TileIndexes)
	}
	if move.TurnNumber != 1 {
		t.Fatalf("turn number = %d, want 1", move.TurnNumber)
	}
}

func TestProtectionForCornerEdgeAndInteriorTiles(t *testing.T) {
	tests := []struct {
		name      string
		tileIndex int
		owned     []int
	}{
		{name: "corner", tileIndex: 0, owned: []int{0, 1, 3}},
		{name: "edge", tileIndex: 1, owned: []int{0, 1, 2, 4}},
		{name: "interior", tileIndex: 4, owned: []int{1, 3, 4, 5, 7}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			board, err := newBoard(3, 3, []byte("ABCDEFGHI"))
			if err != nil {
				t.Fatalf("new board: %v", err)
			}
			for _, index := range test.owned {
				board.tiles[index].Owner = PlayerOne
			}
			board.recalculateProtection()
			if !board.tiles[test.tileIndex].Protected {
				t.Fatalf("tile %d is not protected", test.tileIndex)
			}
		})
	}
}

func TestProtectedTileDoesNotChangeOwnerWhenOpponentUsesIt(t *testing.T) {
	game := gameWithProtectedCenter(t)

	if _, err := game.Play(PlayerTwo, []int{4}); err != nil {
		t.Fatalf("play protected center: %v", err)
	}
	tile, _ := game.Board().Tile(4)
	if tile.Owner != PlayerOne || !tile.Protected {
		t.Fatalf("center = %#v, want protected PlayerOne tile", tile)
	}
}

func TestTileLosesProtectionWhenSurroundingOwnershipChanges(t *testing.T) {
	game := gameWithProtectedCenter(t)

	if _, err := game.Play(PlayerTwo, []int{1}); err != nil {
		t.Fatalf("play neighboring tile: %v", err)
	}
	center, _ := game.Board().Tile(4)
	neighbor, _ := game.Board().Tile(1)
	if center.Protected {
		t.Fatal("center remained protected after a neighbor changed ownership")
	}
	if neighbor.Owner != PlayerTwo {
		t.Fatalf("neighbor owner = %d, want PlayerTwo", neighbor.Owner)
	}
}

func TestPlayRejectsInvalidMovesWithoutChangingState(t *testing.T) {
	tests := []struct {
		name      string
		game      func(*testing.T) *Game
		player    Player
		indexes   []int
		wantError error
	}{
		{name: "out of turn", game: func(t *testing.T) *Game { return newTestGame(t, 3, 1, []byte("ABC")) }, player: PlayerTwo, indexes: []int{0}, wantError: ErrOutOfTurn},
		{name: "invalid player", game: func(t *testing.T) *Game { return newTestGame(t, 3, 1, []byte("ABC")) }, player: Neutral, indexes: []int{0}, wantError: ErrInvalidPlayer},
		{name: "duplicate tile", game: func(t *testing.T) *Game { return newTestGame(t, 3, 1, []byte("ABC")) }, player: PlayerOne, indexes: []int{0, 0}, wantError: ErrDuplicateTile},
		{name: "tile out of range", game: func(t *testing.T) *Game { return newTestGame(t, 3, 1, []byte("ABC")) }, player: PlayerOne, indexes: []int{3}, wantError: ErrInvalidTile},
		{name: "empty word", game: func(t *testing.T) *Game { return newTestGame(t, 3, 1, []byte("ABC")) }, player: PlayerOne, indexes: nil, wantError: ErrInvalidWord},
		{name: "validator rejection", game: func(t *testing.T) *Game {
			game, err := New(3, 1, fixedGenerator{letters: []byte("ABC")}, rejectingValidator{})
			if err != nil {
				t.Fatalf("new game: %v", err)
			}
			return game
		}, player: PlayerOne, indexes: []int{0}, wantError: ErrInvalidWord},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			game := test.game(t)
			before := game.Board().Tiles()
			_, err := game.Play(test.player, test.indexes)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if !reflect.DeepEqual(game.Board().Tiles(), before) || len(game.Moves()) != 0 || game.CurrentPlayer() != PlayerOne {
				t.Fatal("invalid move changed game state")
			}
		})
	}
}

func TestPlayRejectsRepeatedWord(t *testing.T) {
	game := newTestGame(t, 5, 1, []byte("AAXYZ"))
	if _, err := game.Play(PlayerOne, []int{0}); err != nil {
		t.Fatalf("play first word: %v", err)
	}
	before := game.Board().Tiles()
	if _, err := game.Play(PlayerTwo, []int{1}); !errors.Is(err, ErrRepeatedWord) {
		t.Fatalf("error = %v, want repeated word", err)
	}
	if !reflect.DeepEqual(game.Board().Tiles(), before) || game.CurrentPlayer() != PlayerTwo {
		t.Fatal("repeated word changed game state")
	}
}

func TestPlayRejectsPrefixOfPreviouslyPlayedWord(t *testing.T) {
	game := newTestGame(t, 6, 1, []byte("DOGSXY"))
	if _, err := game.Play(PlayerOne, []int{0, 1, 2, 3}); err != nil {
		t.Fatalf("play first word: %v", err)
	}
	if _, err := game.Play(PlayerTwo, []int{0, 1, 2}); !errors.Is(err, ErrPrefixWord) {
		t.Fatalf("error = %v, want prefix word", err)
	}
}

func TestCompletionReportsWinnerAndRejectsFurtherMoves(t *testing.T) {
	game := newTestGame(t, 3, 1, []byte("ABC"))
	if _, err := game.Play(PlayerOne, []int{0, 1}); err != nil {
		t.Fatalf("player one move: %v", err)
	}
	if _, err := game.Play(PlayerTwo, []int{2}); err != nil {
		t.Fatalf("player two move: %v", err)
	}

	result, ok := game.Outcome()
	if !ok {
		t.Fatal("completed game has no result")
	}
	want := Result{PlayerOneScore: 2, PlayerTwoScore: 1, Winner: PlayerOne}
	if result != want || game.Status() != StatusCompleted {
		t.Fatalf("result/status = %#v/%q, want %#v/completed", result, game.Status(), want)
	}
	if _, err := game.Play(PlayerOne, []int{0}); !errors.Is(err, ErrGameCompleted) {
		t.Fatalf("post-completion error = %v, want game completed", err)
	}
}

func TestCompletionReportsTie(t *testing.T) {
	game := newTestGame(t, 4, 1, []byte("ABCD"))
	if _, err := game.Play(PlayerOne, []int{0, 1}); err != nil {
		t.Fatalf("player one move: %v", err)
	}
	if _, err := game.Play(PlayerTwo, []int{2, 3}); err != nil {
		t.Fatalf("player two move: %v", err)
	}

	result, ok := game.Outcome()
	want := Result{PlayerOneScore: 2, PlayerTwoScore: 2, Winner: Neutral, Tie: true}
	if !ok || result != want {
		t.Fatalf("result = %#v, %t, want %#v, true", result, ok, want)
	}
}

func TestNewRejectsInvalidCollaboratorsAndBoards(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		height    int
		generator BoardGenerator
		validator WordValidator
		wantError error
	}{
		{name: "missing generator", width: 1, height: 1, validator: MVPWordValidator{}, wantError: ErrInvalidConfig},
		{name: "missing validator", width: 1, height: 1, generator: fixedGenerator{letters: []byte("A")}, wantError: ErrInvalidConfig},
		{name: "wrong letter count", width: 2, height: 1, generator: fixedGenerator{letters: []byte("A")}, validator: MVPWordValidator{}, wantError: ErrInvalidBoard},
		{name: "invalid letter", width: 1, height: 1, generator: fixedGenerator{letters: []byte("q")}, validator: MVPWordValidator{}, wantError: ErrInvalidBoard},
		{name: "generator error", width: 1, height: 1, generator: fixedGenerator{err: io.ErrUnexpectedEOF}, validator: MVPWordValidator{}, wantError: io.ErrUnexpectedEOF},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.width, test.height, test.generator, test.validator)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func newTestGame(t *testing.T, width, height int, letters []byte) *Game {
	t.Helper()
	game, err := New(width, height, fixedGenerator{letters: letters}, MVPWordValidator{})
	if err != nil {
		t.Fatalf("new game: %v", err)
	}
	return game
}

func gameWithProtectedCenter(t *testing.T) *Game {
	t.Helper()
	game := newTestGame(t, 3, 3, []byte("ABCDEFGHI"))
	for _, index := range []int{1, 3, 4, 5, 7} {
		game.board.tiles[index].Owner = PlayerOne
	}
	game.board.recalculateProtection()
	game.currentPlayer = PlayerTwo
	if !game.board.tiles[4].Protected {
		t.Fatal("test setup did not protect center")
	}
	return game
}
