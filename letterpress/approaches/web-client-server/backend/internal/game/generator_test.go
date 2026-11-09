package game

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
	"reflect"
	"testing"
)

func TestPlayableBoardGeneratorEnforcesHardConstraints(t *testing.T) {
	tests := []struct {
		name          string
		firstValue    uint32
		wantVowels    int
		entropyValues []uint32
	}{
		{name: "minimum vowels", firstValue: 0, wantVowels: 5},
		{name: "maximum vowels", firstValue: 2, wantVowels: 7},
		{
			name:          "difficult letters reach their combined cap",
			firstValue:    1,
			wantVowels:    6,
			entropyValues: append(append([]uint32{1}, repeatedUint32(6, 0)...), 189, 189),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := append([]uint32{test.firstValue}, repeatedUint32(64, 0)...)
			if len(test.entropyValues) > 0 {
				values = append(append([]uint32(nil), test.entropyValues...), repeatedUint32(64, 0)...)
			}
			generator := newTestPlayableGenerator(t, values...)
			letters, err := generator.Generate(5, 5)
			if err != nil {
				t.Fatalf("generate board: %v", err)
			}
			assertPlayableBoard(t, letters, test.wantVowels)
			if test.name == "difficult letters reach their combined cap" && countDifficult(letters) != 2 {
				t.Fatalf("difficult letters = %d, want 2 in %q", countDifficult(letters), letters)
			}
		})
	}
}

func TestPlayableBoardGeneratorIsReproducibleWithFixedEntropy(t *testing.T) {
	values := make([]uint32, 64)
	for index := range values {
		values[index] = uint32(index * 37)
	}
	first := newTestPlayableGenerator(t, values...)
	second := newTestPlayableGenerator(t, values...)
	firstBoard, err := first.Generate(5, 5)
	if err != nil {
		t.Fatalf("generate first board: %v", err)
	}
	secondBoard, err := second.Generate(5, 5)
	if err != nil {
		t.Fatalf("generate second board: %v", err)
	}
	if !reflect.DeepEqual(firstBoard, secondBoard) {
		t.Fatalf("fixed entropy boards differ: %q and %q", firstBoard, secondBoard)
	}
}

func TestPlayableBoardGeneratorLargeSampleDistributionIsDeterministic(t *testing.T) {
	const sampleBoards = 10_000
	sample := func(seed int64) [26]int {
		t.Helper()
		generator, err := NewPlayableBoardGeneratorWithPolicy(DefaultBoardGenerationPolicy(), rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatalf("new sampled generator: %v", err)
		}
		totals := [26]int{}
		for boardIndex := 0; boardIndex < sampleBoards; boardIndex++ {
			letters, generateErr := generator.Generate(5, 5)
			if generateErr != nil {
				t.Fatalf("generate sampled board %d: %v", boardIndex, generateErr)
			}
			counts := [26]int{}
			vowels := 0
			difficult := 0
			for _, letter := range letters {
				counts[letter-'A']++
				totals[letter-'A']++
				switch letter {
				case 'A', 'E', 'I', 'O', 'U':
					vowels++
				case 'J', 'Q', 'X', 'Z':
					difficult++
				}
			}
			if vowels < 5 || vowels > 7 || difficult > 2 {
				t.Fatalf("sampled board %d has %d vowels and %d difficult letters", boardIndex, vowels, difficult)
			}
			for letter, count := range counts {
				if count > 4 {
					t.Fatalf("sampled board %d has %d copies of %c", boardIndex, count, 'A'+letter)
				}
			}
		}
		return totals
	}

	first := sample(14)
	second := sample(14)
	if first != second {
		t.Fatalf("same seed produced different totals: %v and %v", first, second)
	}
	for index, count := range first {
		if count == 0 {
			t.Fatalf("configured letter %c was unreachable in %d boards", 'A'+index, sampleBoards)
		}
	}
	if first['E'-'A'] <= first['U'-'A'] || first['T'-'A'] <= first['J'-'A'] || first['N'-'A'] <= first['K'-'A'] {
		t.Fatalf("sample does not reflect common-over-rare weighting: E/U=%d/%d T/J=%d/%d N/K=%d/%d",
			first['E'-'A'], first['U'-'A'], first['T'-'A'], first['J'-'A'], first['N'-'A'], first['K'-'A'])
	}
}

func TestPlayableBoardGeneratorShufflesCompletedBag(t *testing.T) {
	generator := newTestPlayableGenerator(t, 0, 0, 0)
	letters := []byte("ABCD")
	if err := generator.shuffle(letters); err != nil {
		t.Fatalf("shuffle: %v", err)
	}
	if string(letters) != "BCDA" {
		t.Fatalf("zero-index Fisher-Yates shuffle = %q, want BCDA", letters)
	}
}

func TestPlayableBoardGeneratorReportsSamplingAndShuffleEntropyFailures(t *testing.T) {
	policy := DefaultBoardGenerationPolicy()
	sampling, err := NewPlayableBoardGeneratorWithPolicy(policy, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("new sampling generator: %v", err)
	}
	if _, err := sampling.Generate(5, 5); !errors.Is(err, ErrGenerationFailed) || !errors.Is(err, io.EOF) {
		t.Fatalf("sampling error = %v, want generation failure wrapping EOF", err)
	}

	shuffle, err := NewPlayableBoardGeneratorWithPolicy(policy, uint32Reader(repeatedUint32(26, 0)...))
	if err != nil {
		t.Fatalf("new shuffle generator: %v", err)
	}
	if _, err := shuffle.Generate(5, 5); !errors.Is(err, ErrGenerationFailed) || !errors.Is(err, io.EOF) {
		t.Fatalf("shuffle error = %v, want generation failure wrapping EOF", err)
	}
}

func TestPlayableBoardGeneratorRejectsInvalidDimensions(t *testing.T) {
	generator := NewPlayableBoardGenerator()
	if _, err := generator.Generate(4, 5); !errors.Is(err, ErrInvalidBoard) {
		t.Fatalf("dimension error = %v, want invalid board", err)
	}
}

func TestPlayableBoardGeneratorRejectsInvalidPolicies(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BoardGenerationPolicy)
	}{
		{name: "not 25 tiles", mutate: func(policy *BoardGenerationPolicy) { policy.Width = 4 }},
		{name: "reversed vowel range", mutate: func(policy *BoardGenerationPolicy) { policy.MinVowels = 8 }},
		{name: "non-positive copy cap", mutate: func(policy *BoardGenerationPolicy) { policy.MaxCopiesPerLetter = 0 }},
		{name: "missing letter weight", mutate: func(policy *BoardGenerationPolicy) { policy.Vowels = policy.Vowels[:4] }},
		{name: "duplicate letter weight", mutate: func(policy *BoardGenerationPolicy) { policy.Vowels[1].Letter = 'A' }},
		{name: "zero letter weight", mutate: func(policy *BoardGenerationPolicy) { policy.Vowels[0].Weight = 0 }},
		{name: "wrong difficult set", mutate: func(policy *BoardGenerationPolicy) { policy.DifficultLetters = []byte{'J', 'Q', 'X'} }},
		{
			name: "insufficient capacity",
			mutate: func(policy *BoardGenerationPolicy) {
				policy.MinVowels = 5
				policy.MaxVowels = 5
				policy.MaxCopiesPerLetter = 1
			},
		},
		{
			name: "weight total overflow",
			mutate: func(policy *BoardGenerationPolicy) {
				policy.Vowels[0].Weight = ^uint32(0)
				policy.Vowels[1].Weight = 1
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := DefaultBoardGenerationPolicy()
			test.mutate(&policy)
			if _, err := NewPlayableBoardGeneratorWithPolicy(policy, uint32Reader(0)); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want invalid config", err)
			}
		})
	}
}

func newTestPlayableGenerator(t *testing.T, values ...uint32) PlayableBoardGenerator {
	t.Helper()
	generator, err := NewPlayableBoardGeneratorWithPolicy(DefaultBoardGenerationPolicy(), uint32Reader(values...))
	if err != nil {
		t.Fatalf("new playable generator: %v", err)
	}
	return generator
}

func uint32Reader(values ...uint32) io.Reader {
	buffer := bytes.NewBuffer(make([]byte, 0, len(values)*4))
	for _, value := range values {
		_ = binary.Write(buffer, binary.BigEndian, value)
	}
	return buffer
}

func repeatedUint32(count int, value uint32) []uint32 {
	values := make([]uint32, count)
	for index := range values {
		values[index] = value
	}
	return values
}

func assertPlayableBoard(t *testing.T, letters []byte, wantVowels int) {
	t.Helper()
	if len(letters) != 25 {
		t.Fatalf("letter count = %d, want 25", len(letters))
	}
	counts := [26]int{}
	vowels := 0
	for _, letter := range letters {
		if letter < 'A' || letter > 'Z' {
			t.Fatalf("letter %q is not A-Z", letter)
		}
		counts[letter-'A']++
		if bytes.ContainsRune([]byte("AEIOU"), rune(letter)) {
			vowels++
		}
	}
	if vowels != wantVowels {
		t.Fatalf("vowels = %d, want %d in %q", vowels, wantVowels, letters)
	}
	if countDifficult(letters) > 2 {
		t.Fatalf("difficult letters = %d, want at most 2 in %q", countDifficult(letters), letters)
	}
	for index, count := range counts {
		if count > 4 {
			t.Fatalf("letter %c count = %d, want at most 4", 'A'+index, count)
		}
	}
}

func countDifficult(letters []byte) int {
	count := 0
	for _, letter := range letters {
		if bytes.ContainsRune([]byte("JQXZ"), rune(letter)) {
			count++
		}
	}
	return count
}
