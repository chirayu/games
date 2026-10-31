package game

import "fmt"

// Board stores the current letters, ownership, and derived protection state.
// Its accessors return snapshots so rule changes remain controlled by Game.
type Board struct {
	width  int
	height int
	tiles  []Tile
}

func newBoard(width, height int, letters []byte) (Board, error) {
	if width <= 0 || height <= 0 || width > int(^uint(0)>>1)/height {
		return Board{}, fmt.Errorf("%w: dimensions must be positive", ErrInvalidBoard)
	}
	if len(letters) != width*height {
		return Board{}, fmt.Errorf("%w: got %d letters for %dx%d board", ErrInvalidBoard, len(letters), width, height)
	}

	tiles := make([]Tile, len(letters))
	for index, letter := range letters {
		if letter < 'A' || letter > 'Z' {
			return Board{}, fmt.Errorf("%w: tile %d has non-alphabetic letter %q", ErrInvalidBoard, index, letter)
		}
		tiles[index].Letter = letter
	}
	return Board{width: width, height: height, tiles: tiles}, nil
}

// Width returns the number of columns.
func (board Board) Width() int { return board.width }

// Height returns the number of rows.
func (board Board) Height() int { return board.height }

// Len returns the number of tiles.
func (board Board) Len() int { return len(board.tiles) }

// Tiles returns a copy of every tile in row-major order.
func (board Board) Tiles() []Tile {
	return append([]Tile(nil), board.tiles...)
}

// Tile returns the tile at a row-major index.
func (board Board) Tile(index int) (Tile, bool) {
	if index < 0 || index >= len(board.tiles) {
		return Tile{}, false
	}
	return board.tiles[index], true
}

// Letters returns the board letters in row-major order.
func (board Board) Letters() []byte {
	letters := make([]byte, len(board.tiles))
	for index, tile := range board.tiles {
		letters[index] = tile.Letter
	}
	return letters
}

func (board Board) clone() Board {
	return Board{width: board.width, height: board.height, tiles: board.Tiles()}
}

func (board *Board) recalculateProtection() {
	protected := make([]bool, len(board.tiles))
	for index, tile := range board.tiles {
		if tile.Owner == Neutral {
			continue
		}
		protected[index] = true
		for _, neighbor := range board.neighbors(index) {
			if board.tiles[neighbor].Owner != tile.Owner {
				protected[index] = false
				break
			}
		}
	}
	for index := range board.tiles {
		board.tiles[index].Protected = protected[index]
	}
}

func (board Board) neighbors(index int) []int {
	row, column := index/board.width, index%board.width
	neighbors := make([]int, 0, 4)
	if row > 0 {
		neighbors = append(neighbors, index-board.width)
	}
	if row+1 < board.height {
		neighbors = append(neighbors, index+board.width)
	}
	if column > 0 {
		neighbors = append(neighbors, index-1)
	}
	if column+1 < board.width {
		neighbors = append(neighbors, index+1)
	}
	return neighbors
}
