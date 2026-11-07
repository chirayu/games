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

// FixedBoardGenerator returns a configured board for deterministic tests and
// development previews. Production uses CryptoBoardGenerator.
type FixedBoardGenerator struct {
	letters []byte
}

// NewFixedBoardGenerator validates and copies row-major A-Z letters.
func NewFixedBoardGenerator(letters string) (FixedBoardGenerator, error) {
	if letters == "" {
		return FixedBoardGenerator{}, fmt.Errorf("%w: fixed board is empty", ErrInvalidBoard)
	}
	for index := range letters {
		if letters[index] < 'A' || letters[index] > 'Z' {
			return FixedBoardGenerator{}, fmt.Errorf("%w: fixed board letter %d is not A-Z", ErrInvalidBoard, index)
		}
	}
	return FixedBoardGenerator{letters: []byte(letters)}, nil
}

// Generate implements BoardGenerator.
func (generator FixedBoardGenerator) Generate(width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 || width > int(^uint(0)>>1)/height || len(generator.letters) != width*height {
		return nil, fmt.Errorf("%w: fixed board dimensions do not match", ErrInvalidBoard)
	}
	return append([]byte(nil), generator.letters...), nil
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
