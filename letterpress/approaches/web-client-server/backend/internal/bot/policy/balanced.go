// Package policy ranks legal bot move candidates without applying moves.
package policy

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"letterpress/backend/internal/bot/search"
	"letterpress/backend/internal/game"
)

const (
	// BalancedName and BalancedVersion identify the immutable baseline policy.
	BalancedName    = "balanced"
	BalancedVersion = 1

	variationLimit   = 3
	qualityPercent   = 85
	neutralWeight    = 12
	opponentWeight   = 20
	protectionWeight = 8
	breakWeight      = 8
	advantageWeight  = 3
)

var ErrNoLegalMove = errors.New("no legal bot move")

// CandidateSource supplies moves already validated against a snapshot.
type CandidateSource interface {
	Candidates(game.Snapshot) ([]search.Candidate, error)
}

// Seed identifies persisted inputs used for reproducible controlled variation.
type Seed struct {
	GameID         string
	ProfileVersion int64
	TurnNumber     int
}

// Score describes the balanced policy's independently testable features.
type Score struct {
	NeutralCapture           int
	OpponentCapture          int
	ProtectionGained         int
	OpponentProtectionBroken int
	ResultingScoreAdvantage  int
	Total                    int
}

// Balanced selects legal candidates using version-one balanced scoring.
type Balanced struct {
	source CandidateSource
	logger *slog.Logger
}

// NewBalanced creates the version-one full-dictionary balanced policy.
func NewBalanced(source CandidateSource, logger *slog.Logger) (*Balanced, error) {
	if source == nil {
		return nil, errors.New("candidate source is required")
	}
	return &Balanced{source: source, logger: logger}, nil
}

// NewBalancedEnglish creates the production policy over the full embedded
// dictionary knowledge profile.
func NewBalancedEnglish(logger *slog.Logger) (*Balanced, error) {
	enumerator, err := search.NewEnglish()
	if err != nil {
		return nil, err
	}
	return NewBalanced(enumerator, logger)
}

// Select returns only ordered tile indexes. Applying and revalidating them is
// the responsibility of the authoritative game service.
func (policy *Balanced) Select(ctx context.Context, snapshot game.Snapshot, seed Seed) ([]int, error) {
	started := time.Now()
	if seed.GameID == "" || seed.ProfileVersion <= 0 || seed.TurnNumber <= 0 {
		return nil, errors.New("persisted decision seed is incomplete")
	}
	candidates, err := policy.source.Candidates(snapshot)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrNoLegalMove
	}
	before, err := game.AnalyzeOwnership(snapshot.Width, snapshot.Height, snapshot.Owners)
	if err != nil {
		return nil, err
	}
	type rankedCandidate struct {
		candidate search.Candidate
		score     Score
	}
	top := make([]rankedCandidate, 0, variationLimit)
	for _, candidate := range candidates {
		score, scoreErr := scoreCandidate(snapshot, before, candidate, false)
		if scoreErr != nil {
			return nil, scoreErr
		}
		ranked := rankedCandidate{candidate: candidate, score: score}
		position := len(top)
		for index := range top {
			if ranked.score.Total > top[index].score.Total ||
				(ranked.score.Total == top[index].score.Total && indexesLess(ranked.candidate.TileIndexes, top[index].candidate.TileIndexes)) {
				position = index
				break
			}
		}
		if position >= variationLimit {
			continue
		}
		top = append(top, rankedCandidate{})
		copy(top[position+1:], top[position:])
		top[position] = ranked
		if len(top) > variationLimit {
			top = top[:variationLimit]
		}
	}

	eligible := 1
	for eligible < len(top) && withinQuality(top[eligible].score.Total, top[0].score.Total) {
		eligible++
	}
	selected := deterministicIndex(seed, eligible)
	if policy.logger != nil {
		policy.logger.Log(ctx, slog.LevelInfo, "bot decision selected",
			"policy", BalancedName,
			"policy_version", BalancedVersion,
			"profile_version", seed.ProfileVersion,
			"turn_number", seed.TurnNumber,
			"candidate_count", len(candidates),
			"eligible_count", eligible,
			"selected_rank", selected+1,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}
	return append([]int(nil), top[selected].candidate.TileIndexes...), nil
}

// ScoreCandidate calculates version-one balanced features from immutable input.
func ScoreCandidate(snapshot game.Snapshot, candidate search.Candidate) (Score, error) {
	before, err := game.AnalyzeOwnership(snapshot.Width, snapshot.Height, snapshot.Owners)
	if err != nil {
		return Score{}, err
	}
	return scoreCandidate(snapshot, before, candidate, true)
}

func scoreCandidate(snapshot game.Snapshot, before game.OwnershipAnalysis, candidate search.Candidate, validateIndexes bool) (Score, error) {
	if len(candidate.Outcome.Owners) != len(snapshot.Owners) || len(candidate.Outcome.Protected) != len(snapshot.Owners) {
		return Score{}, errors.New("candidate outcome dimensions do not match snapshot")
	}
	opponent := game.PlayerOne
	if snapshot.CurrentPlayer == game.PlayerOne {
		opponent = game.PlayerTwo
	} else if snapshot.CurrentPlayer != game.PlayerTwo {
		return Score{}, game.ErrInvalidPlayer
	}
	result := Score{}
	var seen map[int]struct{}
	if validateIndexes {
		seen = make(map[int]struct{}, len(candidate.TileIndexes))
	}
	for _, index := range candidate.TileIndexes {
		if index < 0 || index >= len(snapshot.Owners) {
			return Score{}, fmt.Errorf("%w: %d", game.ErrInvalidTile, index)
		}
		if validateIndexes {
			if _, duplicate := seen[index]; duplicate {
				return Score{}, fmt.Errorf("%w: %d", game.ErrDuplicateTile, index)
			}
			seen[index] = struct{}{}
		}
		if before.Protected[index] {
			continue
		}
		switch snapshot.Owners[index] {
		case game.Neutral:
			result.NeutralCapture++
		case opponent:
			result.OpponentCapture++
		}
	}
	for index, owner := range candidate.Outcome.Owners {
		if owner == snapshot.CurrentPlayer && candidate.Outcome.Protected[index] &&
			!(snapshot.Owners[index] == snapshot.CurrentPlayer && before.Protected[index]) {
			result.ProtectionGained++
		}
		if snapshot.Owners[index] == opponent && before.Protected[index] && !candidate.Outcome.Protected[index] {
			result.OpponentProtectionBroken++
		}
	}
	if snapshot.CurrentPlayer == game.PlayerOne {
		result.ResultingScoreAdvantage = candidate.Outcome.PlayerOneScore - candidate.Outcome.PlayerTwoScore
	} else {
		result.ResultingScoreAdvantage = candidate.Outcome.PlayerTwoScore - candidate.Outcome.PlayerOneScore
	}
	result.Total = result.NeutralCapture*neutralWeight +
		result.OpponentCapture*opponentWeight +
		result.ProtectionGained*protectionWeight +
		result.OpponentProtectionBroken*breakWeight +
		result.ResultingScoreAdvantage*advantageWeight
	return result, nil
}

func withinQuality(score, best int) bool {
	if best <= 0 {
		return score == best
	}
	return score*100 >= best*qualityPercent
}

func deterministicIndex(seed Seed, count int) int {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00%d", seed.GameID, seed.ProfileVersion, seed.TurnNumber, BalancedName, BalancedVersion)))
	return int(binary.BigEndian.Uint64(digest[:8]) % uint64(count))
}

func indexesLess(left, right []int) bool {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := 0; index < limit; index++ {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return len(left) < len(right)
}
