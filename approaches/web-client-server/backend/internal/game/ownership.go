package game

import "fmt"

// OwnershipAnalysis contains state derived from the authoritative tile owners.
type OwnershipAnalysis struct {
	Protected      []bool
	PlayerOneScore int
	PlayerTwoScore int
}

// AnalyzeOwnership derives protection and scores without HTTP or persistence
// dependencies.
func AnalyzeOwnership(width, height int, owners []Player) (OwnershipAnalysis, error) {
	if width <= 0 || height <= 0 || width > int(^uint(0)>>1)/height || len(owners) != width*height {
		return OwnershipAnalysis{}, fmt.Errorf("%w: ownership dimensions do not match", ErrInvalidBoard)
	}

	board := Board{width: width, height: height, tiles: make([]Tile, len(owners))}
	analysis := OwnershipAnalysis{Protected: make([]bool, len(owners))}
	for index, owner := range owners {
		switch owner {
		case Neutral:
		case PlayerOne:
			analysis.PlayerOneScore++
		case PlayerTwo:
			analysis.PlayerTwoScore++
		default:
			return OwnershipAnalysis{}, fmt.Errorf("%w: tile %d has owner %d", ErrInvalidPlayer, index, owner)
		}
		board.tiles[index].Owner = owner
	}
	board.recalculateProtection()
	for index, tile := range board.tiles {
		analysis.Protected[index] = tile.Protected
	}
	return analysis, nil
}
