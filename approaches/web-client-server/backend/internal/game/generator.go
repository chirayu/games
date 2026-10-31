package game

import (
	"crypto/rand"
	"fmt"
	"io"
)

const unbiasedAlphabetRange = byte(26 * (256 / 26))

// BoardGenerator supplies row-major letters for a board.
type BoardGenerator interface {
	Generate(width, height int) ([]byte, error)
}

// CryptoBoardGenerator generates independent, uniformly distributed A-Z
// letters using the operating system's cryptographically secure random source.
type CryptoBoardGenerator struct {
	reader io.Reader
}

// NewCryptoBoardGenerator returns the MVP board generator.
func NewCryptoBoardGenerator() CryptoBoardGenerator {
	return CryptoBoardGenerator{reader: rand.Reader}
}

func newCryptoBoardGenerator(reader io.Reader) CryptoBoardGenerator {
	return CryptoBoardGenerator{reader: reader}
}

// Generate implements BoardGenerator. Rejection sampling avoids modulo bias.
func (generator CryptoBoardGenerator) Generate(width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 || width > int(^uint(0)>>1)/height {
		return nil, fmt.Errorf("%w: dimensions must be positive", ErrInvalidBoard)
	}
	reader := generator.reader
	if reader == nil {
		reader = rand.Reader
	}

	letters := make([]byte, width*height)
	randomByte := []byte{0}
	for index := range letters {
		for {
			if _, err := io.ReadFull(reader, randomByte); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrGenerationFailed, err)
			}
			if randomByte[0] >= unbiasedAlphabetRange {
				continue
			}
			letters[index] = 'A' + randomByte[0]%26
			break
		}
	}
	return letters, nil
}
