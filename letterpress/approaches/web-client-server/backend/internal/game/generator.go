package game

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	playableBoardTiles = 25
	randomUint32Space  = uint64(1) << 32
)

// BoardGenerator supplies row-major letters for a board.
type BoardGenerator interface {
	Generate(width, height int) ([]byte, error)
}

// FixedBoardGenerator returns a configured board for deterministic tests and
// development previews. Production uses PlayableBoardGenerator.
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

// WeightedLetter is one product-owned sampling weight. Relative weights matter;
// they do not need to sum to a particular value.
type WeightedLetter struct {
	Letter byte
	Weight uint32
}

// BoardGenerationPolicy keeps the complete constrained-bag policy together.
type BoardGenerationPolicy struct {
	Width               int
	Height              int
	MinVowels           int
	MaxVowels           int
	MaxDifficultLetters int
	MaxCopiesPerLetter  int
	Vowels              []WeightedLetter
	Consonants          []WeightedLetter
	DifficultLetters    []byte
}

// DefaultBoardGenerationPolicy returns an independent copy of the production
// 5x5 policy documented in the root game rules.
func DefaultBoardGenerationPolicy() BoardGenerationPolicy {
	return BoardGenerationPolicy{
		Width:               5,
		Height:              5,
		MinVowels:           5,
		MaxVowels:           7,
		MaxDifficultLetters: 2,
		MaxCopiesPerLetter:  4,
		Vowels: []WeightedLetter{
			{Letter: 'A', Weight: 82},
			{Letter: 'E', Weight: 127},
			{Letter: 'I', Weight: 70},
			{Letter: 'O', Weight: 75},
			{Letter: 'U', Weight: 28},
		},
		Consonants: []WeightedLetter{
			{Letter: 'B', Weight: 15},
			{Letter: 'C', Weight: 28},
			{Letter: 'D', Weight: 43},
			{Letter: 'F', Weight: 22},
			{Letter: 'G', Weight: 20},
			{Letter: 'H', Weight: 61},
			{Letter: 'J', Weight: 2},
			{Letter: 'K', Weight: 8},
			{Letter: 'L', Weight: 40},
			{Letter: 'M', Weight: 24},
			{Letter: 'N', Weight: 67},
			{Letter: 'P', Weight: 19},
			{Letter: 'Q', Weight: 1},
			{Letter: 'R', Weight: 60},
			{Letter: 'S', Weight: 63},
			{Letter: 'T', Weight: 91},
			{Letter: 'V', Weight: 10},
			{Letter: 'W', Weight: 24},
			{Letter: 'X', Weight: 2},
			{Letter: 'Y', Weight: 20},
			{Letter: 'Z', Weight: 1},
		},
		DifficultLetters: []byte{'J', 'Q', 'X', 'Z'},
	}
}

// PlayableBoardGenerator builds and shuffles a constrained weighted letter bag.
type PlayableBoardGenerator struct {
	policy    BoardGenerationPolicy
	reader    io.Reader
	difficult [26]bool
}

// NewPlayableBoardGenerator returns the validated production generator. The
// default policy is compile-time product configuration, so invalid defaults are
// a programming error rather than a runtime condition.
func NewPlayableBoardGenerator() PlayableBoardGenerator {
	generator, err := NewPlayableBoardGeneratorWithPolicy(DefaultBoardGenerationPolicy(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return generator
}

// NewPlayableBoardGeneratorWithPolicy validates a complete policy and accepts
// an injectable entropy source for deterministic domain tests.
func NewPlayableBoardGeneratorWithPolicy(policy BoardGenerationPolicy, reader io.Reader) (PlayableBoardGenerator, error) {
	if err := validateBoardGenerationPolicy(policy); err != nil {
		return PlayableBoardGenerator{}, err
	}
	if reader == nil {
		reader = rand.Reader
	}
	policy.Vowels = append([]WeightedLetter(nil), policy.Vowels...)
	policy.Consonants = append([]WeightedLetter(nil), policy.Consonants...)
	policy.DifficultLetters = append([]byte(nil), policy.DifficultLetters...)

	generator := PlayableBoardGenerator{policy: policy, reader: reader}
	for _, letter := range policy.DifficultLetters {
		generator.difficult[letter-'A'] = true
	}
	return generator, nil
}

// Generate implements BoardGenerator.
func (generator PlayableBoardGenerator) Generate(width, height int) ([]byte, error) {
	if width != generator.policy.Width || height != generator.policy.Height {
		return nil, fmt.Errorf("%w: playable board dimensions must be %dx%d", ErrInvalidBoard, generator.policy.Width, generator.policy.Height)
	}

	vowelOffset, err := generator.randomBounded(uint32(generator.policy.MaxVowels - generator.policy.MinVowels + 1))
	if err != nil {
		return nil, err
	}
	vowelCount := generator.policy.MinVowels + int(vowelOffset)
	letters := make([]byte, 0, width*height)
	counts := [26]int{}
	difficultCount := 0

	for range vowelCount {
		letter, drawErr := generator.drawWeighted(generator.policy.Vowels, counts, difficultCount)
		if drawErr != nil {
			return nil, drawErr
		}
		letters = append(letters, letter)
		counts[letter-'A']++
	}
	for len(letters) < width*height {
		letter, drawErr := generator.drawWeighted(generator.policy.Consonants, counts, difficultCount)
		if drawErr != nil {
			return nil, drawErr
		}
		letters = append(letters, letter)
		counts[letter-'A']++
		if generator.difficult[letter-'A'] {
			difficultCount++
		}
	}
	if err := generator.shuffle(letters); err != nil {
		return nil, err
	}
	return letters, nil
}

func (generator PlayableBoardGenerator) drawWeighted(entries []WeightedLetter, counts [26]int, difficultCount int) (byte, error) {
	var total uint32
	for _, entry := range entries {
		if counts[entry.Letter-'A'] >= generator.policy.MaxCopiesPerLetter {
			continue
		}
		if generator.difficult[entry.Letter-'A'] && difficultCount >= generator.policy.MaxDifficultLetters {
			continue
		}
		if ^uint32(0)-total < entry.Weight {
			return 0, fmt.Errorf("%w: eligible weight total overflows", ErrGenerationFailed)
		}
		total += entry.Weight
	}
	if total == 0 {
		return 0, fmt.Errorf("%w: policy has no eligible letters", ErrGenerationFailed)
	}

	target, err := generator.randomBounded(total)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if counts[entry.Letter-'A'] >= generator.policy.MaxCopiesPerLetter {
			continue
		}
		if generator.difficult[entry.Letter-'A'] && difficultCount >= generator.policy.MaxDifficultLetters {
			continue
		}
		if target < entry.Weight {
			return entry.Letter, nil
		}
		target -= entry.Weight
	}
	return 0, fmt.Errorf("%w: weighted draw did not select a letter", ErrGenerationFailed)
}

func (generator PlayableBoardGenerator) shuffle(letters []byte) error {
	for index := len(letters) - 1; index > 0; index-- {
		other, err := generator.randomBounded(uint32(index + 1))
		if err != nil {
			return err
		}
		letters[index], letters[other] = letters[other], letters[index]
	}
	return nil
}

// randomBounded uses rejection sampling over a full uint32 range, avoiding
// modulo bias for both weighted draws and Fisher-Yates indexes.
func (generator PlayableBoardGenerator) randomBounded(bound uint32) (uint32, error) {
	if bound == 0 {
		return 0, fmt.Errorf("%w: random bound must be positive", ErrGenerationFailed)
	}
	limit := randomUint32Space - randomUint32Space%uint64(bound)
	var randomBytes [4]byte
	for {
		if _, err := io.ReadFull(generator.reader, randomBytes[:]); err != nil {
			return 0, fmt.Errorf("%w: %w", ErrGenerationFailed, err)
		}
		value := binary.BigEndian.Uint32(randomBytes[:])
		if uint64(value) < limit {
			return value % bound, nil
		}
	}
}

func validateBoardGenerationPolicy(policy BoardGenerationPolicy) error {
	if policy.Width <= 0 || policy.Height <= 0 || policy.Width > int(^uint(0)>>1)/policy.Height || policy.Width*policy.Height != playableBoardTiles {
		return fmt.Errorf("%w: playable policy must describe exactly 25 tiles", ErrInvalidConfig)
	}
	if policy.MinVowels < 0 || policy.MinVowels > policy.MaxVowels || policy.MaxVowels > playableBoardTiles {
		return fmt.Errorf("%w: invalid vowel range", ErrInvalidConfig)
	}
	if policy.MaxCopiesPerLetter <= 0 || policy.MaxDifficultLetters < 0 {
		return fmt.Errorf("%w: invalid letter caps", ErrInvalidConfig)
	}

	seen := [26]bool{}
	vowelSet := [26]bool{}
	for _, letter := range []byte{'A', 'E', 'I', 'O', 'U'} {
		vowelSet[letter-'A'] = true
	}
	if err := validateWeights(policy.Vowels, vowelSet, true, &seen); err != nil {
		return err
	}
	if err := validateWeights(policy.Consonants, vowelSet, false, &seen); err != nil {
		return err
	}
	for _, present := range seen {
		if !present {
			return fmt.Errorf("%w: policy must assign a positive weight to every A-Z letter", ErrInvalidConfig)
		}
	}

	difficult := [26]bool{}
	for _, letter := range policy.DifficultLetters {
		if letter < 'A' || letter > 'Z' || vowelSet[letter-'A'] || difficult[letter-'A'] {
			return fmt.Errorf("%w: invalid difficult-letter set", ErrInvalidConfig)
		}
		difficult[letter-'A'] = true
	}
	for _, letter := range []byte{'J', 'Q', 'X', 'Z'} {
		if !difficult[letter-'A'] {
			return fmt.Errorf("%w: difficult-letter set must be J Q X Z", ErrInvalidConfig)
		}
	}
	if len(policy.DifficultLetters) != 4 {
		return fmt.Errorf("%w: difficult-letter set must be J Q X Z", ErrInvalidConfig)
	}
	if len(policy.Vowels)*policy.MaxCopiesPerLetter < policy.MaxVowels {
		return fmt.Errorf("%w: vowel capacity cannot satisfy the range", ErrInvalidConfig)
	}
	nonDifficult := len(policy.Consonants) - len(policy.DifficultLetters)
	difficultCapacity := len(policy.DifficultLetters) * policy.MaxCopiesPerLetter
	if difficultCapacity > policy.MaxDifficultLetters {
		difficultCapacity = policy.MaxDifficultLetters
	}
	if nonDifficult*policy.MaxCopiesPerLetter+difficultCapacity < playableBoardTiles-policy.MinVowels {
		return fmt.Errorf("%w: consonant capacity cannot fill the board", ErrInvalidConfig)
	}
	return nil
}

func validateWeights(entries []WeightedLetter, vowels [26]bool, wantVowels bool, seen *[26]bool) error {
	if len(entries) == 0 {
		return fmt.Errorf("%w: letter weights are required", ErrInvalidConfig)
	}
	var total uint64
	for _, entry := range entries {
		if entry.Letter < 'A' || entry.Letter > 'Z' || entry.Weight == 0 || vowels[entry.Letter-'A'] != wantVowels || seen[entry.Letter-'A'] {
			return fmt.Errorf("%w: invalid or duplicate weight for %q", ErrInvalidConfig, entry.Letter)
		}
		total += uint64(entry.Weight)
		if total > uint64(^uint32(0)) {
			return fmt.Errorf("%w: letter weight total overflows", ErrInvalidConfig)
		}
		seen[entry.Letter-'A'] = true
	}
	return nil
}
