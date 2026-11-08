// Package sqlite persists Letterpress game state in SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	generated "letterpress/backend/internal/store/sqlite/db"
	"letterpress/backend/internal/store/sqlite/migrations"
)

const busyTimeout = 5 * time.Second

var (
	// ErrVersionConflict indicates that a game changed after the caller read it.
	ErrVersionConflict = errors.New("game version conflict")
	// ErrGameNotFound indicates that a transactional operation has no game row.
	ErrGameNotFound = errors.New("game not found")
	// ErrPlayerNotFound indicates that a registered player has no seat in a game.
	ErrPlayerNotFound = errors.New("player not found")
	// ErrAllocationTransition indicates that an allocation cannot leave its current state.
	ErrAllocationTransition = errors.New("invalid allocation transition")
	// ErrAllocationParticipant indicates that completion participants do not match the allocation.
	ErrAllocationParticipant = errors.New("invalid allocation participant")
)

// Game is the current persisted state of a game.
type Game struct {
	ID            string
	BoardLetters  []byte
	TileOwners    []byte
	CurrentPlayer int64
	Status        string
	Version       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// GameSeat assigns one registered player to one numbered seat in a game.
type GameSeat struct {
	GameID    string
	Seat      int64
	PlayerID  string
	CreatedAt time.Time
}

// ActiveGameSummary is the persisted data needed to build one lobby row.
type ActiveGameSummary struct {
	ID            string
	TileOwners    []byte
	CurrentPlayer int64
	Status        string
	Version       int64
	UpdatedAt     time.Time
	PlayerSeat    int64
	OpponentID    string
	OpponentName  string
}

// GameAllocation is a durable request to assign two players to one game.
type GameAllocation struct {
	ID                  string
	RequesterID         string
	OpponentID          string
	Mode                string
	AllowedOpponentType string
	State               string
	IdempotencyKey      string
	GameID              string
	FailureReason       string
	ExpiresAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// SpecifiedChallenge is one pending allocation with public participant names.
type SpecifiedChallenge struct {
	AllocationID  string
	RequesterID   string
	RequesterName string
	OpponentID    string
	OpponentName  string
	ExpiresAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// RegisteredPlayer is a durable name-based identity independent of a game.
type RegisteredPlayer struct {
	ID             string
	DisplayName    string
	NormalizedName string
	CreatedAt      time.Time
}

// PlayerSession is one independently revocable registered-player session.
// TokenHash is the only credential persisted.
type PlayerSession struct {
	TokenHash [32]byte
	PlayerID  string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Move is an accepted move in turn order.
type Move struct {
	ID          int64
	GameID      string
	Player      int64
	TurnNumber  int64
	TileIndexes []int64
	Word        string
	CreatedAt   time.Time
}

// GameState is the mutable game state written after an accepted move.
type GameState struct {
	TileOwners    []byte
	CurrentPlayer int64
	Status        string
	UpdatedAt     time.Time
}

// MoveOperation validates a move from the state read inside its write
// transaction and returns the accepted move and resulting state.
type MoveOperation func(Game, GameSeat, []Move) (Move, GameState, error)

// Repository owns a SQLite connection pool and generated queries.
type Repository struct {
	database *sql.DB
	queries  *generated.Queries
}

// Open opens a SQLite database, configures it for the web service, and applies
// every pending migration. The caller must close the returned repository.
func Open(ctx context.Context, path string) (*Repository, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}

	values := url.Values{}
	values.Add("_pragma", "foreign_keys(1)")
	values.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	dsn := "file:" + filepath.ToSlash(path) + "?" + values.Encode()

	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)

	cleanup := func(openErr error) (*Repository, error) {
		_ = database.Close()
		return nil, openErr
	}
	if err := database.PingContext(ctx); err != nil {
		return cleanup(fmt.Errorf("ping sqlite database: %w", err))
	}
	if _, err := database.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		return cleanup(fmt.Errorf("enable sqlite WAL mode: %w", err))
	}
	if err := migrate(ctx, database); err != nil {
		return cleanup(err)
	}

	return &Repository{database: database, queries: generated.New(database)}, nil
}

func migrate(ctx context.Context, database *sql.DB) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migrations.Files)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Close releases the repository's database connections.
func (repository *Repository) Close() error {
	return repository.database.Close()
}

// Health verifies that SQLite can serve a request.
func (repository *Repository) Health(ctx context.Context) error {
	if err := repository.database.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite database: %w", err)
	}
	return nil
}

// CreateRegisteredPlayerSession atomically finds or creates a player by
// normalized name and records a new independent session for that identity.
func (repository *Repository) CreateRegisteredPlayerSession(
	ctx context.Context,
	candidate RegisteredPlayer,
	session PlayerSession,
) (RegisteredPlayer, bool, error) {
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return RegisteredPlayer{}, false, fmt.Errorf("acquire player session connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return RegisteredPlayer{}, false, fmt.Errorf("begin player session transaction: %w", err)
	}
	defer func() { _, _ = connection.ExecContext(context.Background(), "ROLLBACK") }()
	queries := generated.New(connection)

	rows, err := queries.CreateRegisteredPlayer(ctx, generated.CreateRegisteredPlayerParams{
		ID: candidate.ID, DisplayName: candidate.DisplayName,
		NormalizedName: candidate.NormalizedName, CreatedAt: candidate.CreatedAt,
	})
	if err != nil {
		return RegisteredPlayer{}, false, fmt.Errorf("create registered player: %w", err)
	}
	row, err := queries.GetRegisteredPlayerByNormalizedName(ctx, candidate.NormalizedName)
	if err != nil {
		return RegisteredPlayer{}, false, fmt.Errorf("get registered player: %w", err)
	}
	player := registeredPlayerFromRow(row)
	if err := queries.CreatePlayerSession(ctx, generated.CreatePlayerSessionParams{
		TokenHash: session.TokenHash[:], PlayerID: player.ID,
		ExpiresAt: session.ExpiresAt, CreatedAt: session.CreatedAt,
	}); err != nil {
		return RegisteredPlayer{}, false, fmt.Errorf("create player session: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return RegisteredPlayer{}, false, fmt.Errorf("commit player session transaction: %w", err)
	}
	return player, rows == 1, nil
}

// GetRegisteredPlayerBySessionHash authenticates one unexpired session.
func (repository *Repository) GetRegisteredPlayerBySessionHash(
	ctx context.Context,
	tokenHash [32]byte,
	now time.Time,
) (RegisteredPlayer, error) {
	row, err := repository.queries.GetRegisteredPlayerBySessionHash(ctx, generated.GetRegisteredPlayerBySessionHashParams{
		TokenHash: tokenHash[:], ExpiresAt: now,
	})
	if err != nil {
		return RegisteredPlayer{}, fmt.Errorf("get registered player by session hash: %w", err)
	}
	return registeredPlayerFromRow(row), nil
}

// GetRegisteredPlayerByID resolves a public identity selected for allocation.
func (repository *Repository) GetRegisteredPlayerByID(ctx context.Context, id string) (RegisteredPlayer, error) {
	row, err := repository.queries.GetRegisteredPlayerByID(ctx, id)
	if err != nil {
		return RegisteredPlayer{}, fmt.Errorf("get registered player by ID: %w", err)
	}
	return registeredPlayerFromRow(row), nil
}

// DeletePlayerSession revokes only the session identified by tokenHash.
func (repository *Repository) DeletePlayerSession(ctx context.Context, tokenHash [32]byte) error {
	if _, err := repository.queries.DeletePlayerSession(ctx, tokenHash[:]); err != nil {
		return fmt.Errorf("delete player session: %w", err)
	}
	return nil
}

// CreateGameAllocation atomically creates or retrieves a requester's idempotent allocation.
func (repository *Repository) CreateGameAllocation(ctx context.Context, candidate GameAllocation) (GameAllocation, bool, error) {
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("acquire allocation connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return GameAllocation{}, false, fmt.Errorf("begin allocation transaction: %w", err)
	}
	defer func() { _, _ = connection.ExecContext(context.Background(), "ROLLBACK") }()
	queries := generated.New(connection)
	rows, err := queries.CreateGameAllocation(ctx, generated.CreateGameAllocationParams{
		ID: candidate.ID, RequesterID: candidate.RequesterID, OpponentID: nullableString(candidate.OpponentID),
		Mode: candidate.Mode, AllowedOpponentType: candidate.AllowedOpponentType,
		State: candidate.State, IdempotencyKey: candidate.IdempotencyKey,
		GameID: nullableString(candidate.GameID), FailureReason: nullableString(candidate.FailureReason),
		ExpiresAt: nullableTime(candidate.ExpiresAt), CreatedAt: candidate.CreatedAt, UpdatedAt: candidate.UpdatedAt,
	})
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("create game allocation: %w", err)
	}
	row, err := queries.GetGameAllocationByRequesterKey(ctx, generated.GetGameAllocationByRequesterKeyParams{
		RequesterID: candidate.RequesterID, IdempotencyKey: candidate.IdempotencyKey,
	})
	if errors.Is(err, sql.ErrNoRows) && candidate.Mode == "random" {
		row, err = queries.GetEquivalentQueuedRandomAllocation(ctx, generated.GetEquivalentQueuedRandomAllocationParams{
			RequesterID: candidate.RequesterID, AllowedOpponentType: candidate.AllowedOpponentType,
		})
	}
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("get idempotent allocation: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return GameAllocation{}, false, fmt.Errorf("commit allocation transaction: %w", err)
	}
	return allocationFromRow(row), rows == 1, nil
}

// MatchRandomAllocation claims the oldest compatible queue entry and creates one shared game.
func (repository *Repository) MatchRandomAllocation(ctx context.Context, allocationID string, game Game) (GameAllocation, bool, error) {
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("acquire matchmaking connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return GameAllocation{}, false, fmt.Errorf("begin matchmaking transaction: %w", err)
	}
	defer func() { _, _ = connection.ExecContext(context.Background(), "ROLLBACK") }()
	queries := generated.New(connection)
	row, err := queries.GetGameAllocation(ctx, allocationID)
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("get matchmaking allocation: %w", err)
	}
	allocation := allocationFromRow(row)
	if allocation.State == "allocated" {
		if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
			return GameAllocation{}, false, fmt.Errorf("commit allocated matchmaking read: %w", err)
		}
		return allocation, true, nil
	}
	if allocation.Mode != "random" || allocation.State != "queued" || allocation.AllowedOpponentType != "human" {
		return GameAllocation{}, false, ErrAllocationTransition
	}
	candidateRow, err := queries.GetRandomMatchCandidate(ctx, generated.GetRandomMatchCandidateParams{
		AllowedOpponentType: allocation.AllowedOpponentType, RequesterID: allocation.RequesterID, ID: allocation.ID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
			return GameAllocation{}, false, fmt.Errorf("commit unmatched allocation: %w", err)
		}
		return allocation, false, nil
	}
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("find random match candidate: %w", err)
	}
	candidate := allocationFromRow(candidateRow)
	if err := queries.CreateGame(ctx, generated.CreateGameParams{
		ID: game.ID, BoardLetters: game.BoardLetters, TileOwners: game.TileOwners,
		CurrentPlayer: game.CurrentPlayer, Status: game.Status, Version: game.Version,
		CreatedAt: game.CreatedAt, UpdatedAt: game.UpdatedAt,
	}); err != nil {
		return GameAllocation{}, false, fmt.Errorf("create matched game: %w", err)
	}
	for seat, playerID := range []string{candidate.RequesterID, allocation.RequesterID} {
		if err := queries.CreateGameSeat(ctx, generated.CreateGameSeatParams{
			GameID: game.ID, Seat: int64(seat + 1), PlayerID: playerID, CreatedAt: game.CreatedAt,
		}); err != nil {
			return GameAllocation{}, false, fmt.Errorf("create matched game seat: %w", err)
		}
	}
	updates := []generated.AllocateRandomGameAllocationParams{
		{OpponentID: nullableString(allocation.RequesterID), GameID: nullableString(game.ID), UpdatedAt: game.UpdatedAt, ID: candidate.ID},
		{OpponentID: nullableString(candidate.RequesterID), GameID: nullableString(game.ID), UpdatedAt: game.UpdatedAt, ID: allocation.ID},
	}
	for _, update := range updates {
		rows, err := queries.AllocateRandomGameAllocation(ctx, update)
		if err != nil || rows != 1 {
			if err == nil {
				err = ErrAllocationTransition
			}
			return GameAllocation{}, false, fmt.Errorf("claim random allocation: %w", err)
		}
	}
	matchedRow, err := queries.GetGameAllocation(ctx, allocation.ID)
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("get matched allocation: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return GameAllocation{}, false, fmt.Errorf("commit random match: %w", err)
	}
	return allocationFromRow(matchedRow), true, nil
}

// GetGameAllocation retrieves one allocation without applying authorization policy.
func (repository *Repository) GetGameAllocation(ctx context.Context, id string) (GameAllocation, error) {
	row, err := repository.queries.GetGameAllocation(ctx, id)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("get game allocation: %w", err)
	}
	return allocationFromRow(row), nil
}

// CancelGameAllocation moves a requester's nonterminal allocation to cancelled.
func (repository *Repository) CancelGameAllocation(ctx context.Context, id, requesterID string, updatedAt time.Time) (GameAllocation, error) {
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("acquire cancellation connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return GameAllocation{}, fmt.Errorf("begin cancellation transaction: %w", err)
	}
	defer func() { _, _ = connection.ExecContext(context.Background(), "ROLLBACK") }()
	queries := generated.New(connection)
	if _, err := queries.ExpireGameAllocation(ctx, generated.ExpireGameAllocationParams{Now: updatedAt, ID: id}); err != nil {
		return GameAllocation{}, fmt.Errorf("expire allocation before cancellation: %w", err)
	}
	rows, err := queries.CancelGameAllocation(ctx, generated.CancelGameAllocationParams{
		UpdatedAt: updatedAt, ID: id, RequesterID: requesterID,
	})
	if err != nil {
		return GameAllocation{}, fmt.Errorf("cancel game allocation: %w", err)
	}
	if rows != 1 {
		return GameAllocation{}, ErrAllocationTransition
	}
	row, err := queries.GetGameAllocation(ctx, id)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("get cancelled allocation: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return GameAllocation{}, fmt.Errorf("commit allocation cancellation: %w", err)
	}
	return allocationFromRow(row), nil
}

// RefreshGameAllocation applies lazy expiry and returns the observable state.
func (repository *Repository) RefreshGameAllocation(ctx context.Context, id string, now time.Time) (GameAllocation, error) {
	if _, err := repository.queries.ExpireGameAllocation(ctx, generated.ExpireGameAllocationParams{Now: now, ID: id}); err != nil {
		return GameAllocation{}, fmt.Errorf("expire game allocation: %w", err)
	}
	return repository.GetGameAllocation(ctx, id)
}

// DeclineSpecifiedAllocation lets only the intended opponent decline a live challenge.
func (repository *Repository) DeclineSpecifiedAllocation(ctx context.Context, id, opponentID string, now time.Time) (GameAllocation, error) {
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("acquire decline connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return GameAllocation{}, fmt.Errorf("begin decline transaction: %w", err)
	}
	defer func() { _, _ = connection.ExecContext(context.Background(), "ROLLBACK") }()
	queries := generated.New(connection)
	if _, err := queries.ExpireGameAllocation(ctx, generated.ExpireGameAllocationParams{Now: now, ID: id}); err != nil {
		return GameAllocation{}, fmt.Errorf("expire allocation before decline: %w", err)
	}
	rows, err := queries.DeclineSpecifiedAllocation(ctx, generated.DeclineSpecifiedAllocationParams{
		UpdatedAt: now, ID: id, OpponentID: nullableString(opponentID),
	})
	if err != nil {
		return GameAllocation{}, fmt.Errorf("decline specified allocation: %w", err)
	}
	if rows != 1 {
		return GameAllocation{}, ErrAllocationTransition
	}
	row, err := queries.GetGameAllocation(ctx, id)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("get declined allocation: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return GameAllocation{}, fmt.Errorf("commit allocation decline: %w", err)
	}
	return allocationFromRow(row), nil
}

// ListPendingSpecifiedChallenges returns a bounded participant-scoped feed.
func (repository *Repository) ListPendingSpecifiedChallenges(ctx context.Context, playerID string, now time.Time, limit int) ([]SpecifiedChallenge, error) {
	if _, err := repository.queries.ExpireDueGameAllocations(ctx, now); err != nil {
		return nil, fmt.Errorf("expire due allocations: %w", err)
	}
	rows, err := repository.queries.ListPendingSpecifiedChallenges(ctx, generated.ListPendingSpecifiedChallengesParams{
		Now: nullableTime(&now), PlayerID: playerID, ResultLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list specified challenges: %w", err)
	}
	items := make([]SpecifiedChallenge, 0, len(rows))
	for _, row := range rows {
		items = append(items, SpecifiedChallenge{
			AllocationID: row.ID, RequesterID: row.RequesterID, RequesterName: row.RequesterName,
			OpponentID: row.OpponentID.String, OpponentName: row.OpponentName,
			ExpiresAt: row.ExpiresAt.Time, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return items, nil
}

// CompleteGameAllocation atomically creates one game and assigns deterministic seats.
// Repeated or concurrent completion returns the already allocated resource.
func (repository *Repository) CompleteGameAllocation(
	ctx context.Context,
	allocationID, opponentID string,
	game Game,
	seats [2]GameSeat,
) (GameAllocation, error) {
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("acquire completion connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return GameAllocation{}, fmt.Errorf("begin completion transaction: %w", err)
	}
	defer func() { _, _ = connection.ExecContext(context.Background(), "ROLLBACK") }()
	queries := generated.New(connection)
	row, err := queries.GetGameAllocation(ctx, allocationID)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("get allocation for completion: %w", err)
	}
	allocation := allocationFromRow(row)
	if allocation.ExpiresAt != nil && !game.UpdatedAt.Before(*allocation.ExpiresAt) &&
		(allocation.State == "pending_acceptance" || allocation.State == "queued") {
		if _, err := queries.ExpireGameAllocation(ctx, generated.ExpireGameAllocationParams{Now: game.UpdatedAt, ID: allocationID}); err != nil {
			return GameAllocation{}, fmt.Errorf("expire allocation before completion: %w", err)
		}
		if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
			return GameAllocation{}, fmt.Errorf("commit allocation expiry: %w", err)
		}
		return GameAllocation{}, ErrAllocationTransition
	}
	if allocation.State == "allocated" {
		if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
			return GameAllocation{}, fmt.Errorf("commit idempotent completion: %w", err)
		}
		return allocation, nil
	}
	if allocation.State != "pending_acceptance" && allocation.State != "queued" {
		return GameAllocation{}, ErrAllocationTransition
	}
	if opponentID == "" || opponentID == allocation.RequesterID ||
		(allocation.OpponentID != "" && allocation.OpponentID != opponentID) {
		return GameAllocation{}, ErrAllocationParticipant
	}
	if seats[0].GameID != game.ID || seats[0].Seat != 1 || seats[0].PlayerID != allocation.RequesterID ||
		seats[1].GameID != game.ID || seats[1].Seat != 2 || seats[1].PlayerID != opponentID {
		return GameAllocation{}, ErrAllocationParticipant
	}
	if err := queries.CreateGame(ctx, generated.CreateGameParams{
		ID: game.ID, BoardLetters: cloneBytes(game.BoardLetters), TileOwners: cloneBytes(game.TileOwners),
		CurrentPlayer: game.CurrentPlayer, Status: game.Status, Version: game.Version,
		CreatedAt: game.CreatedAt, UpdatedAt: game.UpdatedAt,
	}); err != nil {
		return GameAllocation{}, fmt.Errorf("create allocated game: %w", err)
	}
	for _, seat := range seats {
		if err := queries.CreateGameSeat(ctx, generated.CreateGameSeatParams{
			GameID: seat.GameID, Seat: seat.Seat, PlayerID: seat.PlayerID, CreatedAt: seat.CreatedAt,
		}); err != nil {
			return GameAllocation{}, fmt.Errorf("create allocated seat %d: %w", seat.Seat, err)
		}
	}
	rows, err := queries.CompleteGameAllocation(ctx, generated.CompleteGameAllocationParams{
		OpponentID: nullableString(opponentID), GameID: nullableString(game.ID), UpdatedAt: game.UpdatedAt, ID: allocationID,
	})
	if err != nil {
		return GameAllocation{}, fmt.Errorf("complete game allocation: %w", err)
	}
	if rows != 1 {
		return GameAllocation{}, ErrAllocationTransition
	}
	row, err = queries.GetGameAllocation(ctx, allocationID)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("get completed allocation: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return GameAllocation{}, fmt.Errorf("commit allocation completion: %w", err)
	}
	return allocationFromRow(row), nil
}

// CreateGame atomically creates a game and both registered-player seats.
func (repository *Repository) CreateGame(ctx context.Context, game Game, seats [2]GameSeat) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create game transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	queries := repository.queries.WithTx(transaction)
	if err := queries.CreateGame(ctx, generated.CreateGameParams{
		ID:            game.ID,
		BoardLetters:  cloneBytes(game.BoardLetters),
		TileOwners:    cloneBytes(game.TileOwners),
		CurrentPlayer: game.CurrentPlayer,
		Status:        game.Status,
		Version:       game.Version,
		CreatedAt:     game.CreatedAt,
		UpdatedAt:     game.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("create game: %w", err)
	}

	for index, seat := range seats {
		if seat.GameID != game.ID {
			return fmt.Errorf("seat %d belongs to game %q, want %q", index+1, seat.GameID, game.ID)
		}
		if err := queries.CreateGameSeat(ctx, generated.CreateGameSeatParams{
			GameID:    seat.GameID,
			Seat:      seat.Seat,
			PlayerID:  seat.PlayerID,
			CreatedAt: seat.CreatedAt,
		}); err != nil {
			return fmt.Errorf("create game seat %d: %w", seat.Seat, err)
		}
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit create game transaction: %w", err)
	}
	return nil
}

// GetGame retrieves the current state of a game.
func (repository *Repository) GetGame(ctx context.Context, id string) (Game, error) {
	game, err := repository.queries.GetGame(ctx, id)
	if err != nil {
		return Game{}, fmt.Errorf("get game: %w", err)
	}
	return Game{
		ID:            game.ID,
		BoardLetters:  cloneBytes(game.BoardLetters),
		TileOwners:    cloneBytes(game.TileOwners),
		CurrentPlayer: game.CurrentPlayer,
		Status:        game.Status,
		Version:       game.Version,
		CreatedAt:     game.CreatedAt,
		UpdatedAt:     game.UpdatedAt,
	}, nil
}

// ListGameSeats retrieves both registered memberships in stable seat order.
func (repository *Repository) ListGameSeats(ctx context.Context, gameID string) ([]GameSeat, error) {
	rows, err := repository.queries.ListGameSeats(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("list game seats: %w", err)
	}
	seats := make([]GameSeat, 0, len(rows))
	for _, row := range rows {
		seats = append(seats, GameSeat{
			GameID: row.GameID, Seat: row.Seat,
			PlayerID: row.PlayerID, CreatedAt: row.CreatedAt,
		})
	}
	return seats, nil
}

// GetGameSeatByPlayer finds a registered player's seat in one game.
func (repository *Repository) GetGameSeatByPlayer(ctx context.Context, gameID, playerID string) (GameSeat, error) {
	row, err := repository.queries.GetGameSeatByPlayer(ctx, generated.GetGameSeatByPlayerParams{
		GameID: gameID, PlayerID: playerID,
	})
	if err != nil {
		return GameSeat{}, fmt.Errorf("get game seat by player: %w", err)
	}
	return GameSeat{GameID: row.GameID, Seat: row.Seat, PlayerID: row.PlayerID, CreatedAt: row.CreatedAt}, nil
}

// ListActiveGamesForPlayer returns one stable, recent-activity page scoped to a participant.
func (repository *Repository) ListActiveGamesForPlayer(
	ctx context.Context,
	playerID string,
	cursorUpdatedAt time.Time,
	cursorID string,
	limit int,
) ([]ActiveGameSummary, error) {
	rows, err := repository.queries.ListActiveGamesForPlayer(ctx, generated.ListActiveGamesForPlayerParams{
		PlayerID: playerID, CursorUpdatedAt: cursorUpdatedAt, CursorID: cursorID, ResultLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list active games for player: %w", err)
	}
	items := make([]ActiveGameSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, ActiveGameSummary{
			ID: row.ID, TileOwners: cloneBytes(row.TileOwners), CurrentPlayer: row.CurrentPlayer,
			Status: row.Status, Version: row.Version, UpdatedAt: row.UpdatedAt,
			PlayerSeat: row.PlayerSeat, OpponentID: row.OpponentID, OpponentName: row.OpponentName,
		})
	}
	return items, nil
}

// SearchRegisteredPlayers returns public identity candidates in stable name order.
func (repository *Repository) SearchRegisteredPlayers(
	ctx context.Context,
	excludedPlayerID, namePattern, cursorName, cursorID string,
	limit int,
) ([]RegisteredPlayer, error) {
	rows, err := repository.queries.SearchRegisteredPlayers(ctx, generated.SearchRegisteredPlayersParams{
		ExcludedPlayerID: excludedPlayerID, NameFragment: namePattern,
		CursorName: cursorName, CursorID: cursorID, ResultLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search registered players: %w", err)
	}
	players := make([]RegisteredPlayer, 0, len(rows))
	for _, row := range rows {
		players = append(players, registeredPlayerFromRow(row))
	}
	return players, nil
}

// ApplyMove atomically records a move and its resulting state. The update only
// succeeds if expectedVersion is still current.
func (repository *Repository) ApplyMove(
	ctx context.Context,
	gameID string,
	expectedVersion int64,
	move Move,
	state GameState,
) error {
	if move.GameID != gameID {
		return fmt.Errorf("move belongs to game %q, want %q", move.GameID, gameID)
	}
	tileIndexes, err := json.Marshal(move.TileIndexes)
	if err != nil {
		return fmt.Errorf("encode tile indexes: %w", err)
	}

	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin move transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	queries := repository.queries.WithTx(transaction)

	rows, err := queries.UpdateGameState(ctx, generated.UpdateGameStateParams{
		TileOwners:    cloneBytes(state.TileOwners),
		CurrentPlayer: state.CurrentPlayer,
		Status:        state.Status,
		UpdatedAt:     state.UpdatedAt,
		ID:            gameID,
		Version:       expectedVersion,
	})
	if err != nil {
		return fmt.Errorf("update game state: %w", err)
	}
	if rows != 1 {
		return ErrVersionConflict
	}

	if err := queries.CreateMove(ctx, generated.CreateMoveParams{
		GameID:      move.GameID,
		Player:      move.Player,
		TurnNumber:  move.TurnNumber,
		TileIndexes: string(tileIndexes),
		Word:        move.Word,
		CreatedAt:   move.CreatedAt,
	}); err != nil {
		return fmt.Errorf("create move: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit move transaction: %w", err)
	}
	return nil
}

// TransactMove authenticates, reads, validates, and persists one move while an
// immediate SQLite write transaction prevents another writer from interleaving.
func (repository *Repository) TransactMove(
	ctx context.Context,
	gameID string,
	playerID string,
	expectedVersion int64,
	operation MoveOperation,
) error {
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire move connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin immediate move transaction: %w", err)
	}
	defer func() { _, _ = connection.ExecContext(context.Background(), "ROLLBACK") }()
	queries := generated.New(connection)

	seatRow, err := queries.GetGameSeatByPlayer(ctx, generated.GetGameSeatByPlayerParams{
		GameID: gameID, PlayerID: playerID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPlayerNotFound
	}
	if err != nil {
		return fmt.Errorf("authorize move player: %w", err)
	}
	gameRow, err := queries.GetGame(ctx, gameID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrGameNotFound
	}
	if err != nil {
		return fmt.Errorf("get game for move: %w", err)
	}
	if gameRow.Version != expectedVersion {
		return ErrVersionConflict
	}
	moveRows, err := queries.ListMoves(ctx, gameID)
	if err != nil {
		return fmt.Errorf("list moves for update: %w", err)
	}
	moves := make([]Move, 0, len(moveRows))
	for _, row := range moveRows {
		var tileIndexes []int64
		if err := json.Unmarshal([]byte(row.TileIndexes), &tileIndexes); err != nil {
			return fmt.Errorf("decode tile indexes for move %d: %w", row.ID, err)
		}
		moves = append(moves, Move{
			ID: row.ID, GameID: row.GameID, Player: row.Player, TurnNumber: row.TurnNumber,
			TileIndexes: tileIndexes, Word: row.Word, CreatedAt: row.CreatedAt,
		})
	}

	storedGame := Game{
		ID: gameRow.ID, BoardLetters: cloneBytes(gameRow.BoardLetters), TileOwners: cloneBytes(gameRow.TileOwners),
		CurrentPlayer: gameRow.CurrentPlayer, Status: gameRow.Status, Version: gameRow.Version,
		CreatedAt: gameRow.CreatedAt, UpdatedAt: gameRow.UpdatedAt,
	}
	seat := GameSeat{
		GameID: seatRow.GameID, Seat: seatRow.Seat,
		PlayerID: seatRow.PlayerID, CreatedAt: seatRow.CreatedAt,
	}
	move, state, err := operation(storedGame, seat, moves)
	if err != nil {
		return err
	}
	if move.GameID != gameID || move.Player != seat.Seat || move.TurnNumber != int64(len(moves)+1) {
		return errors.New("move operation returned inconsistent identity or turn")
	}
	tileIndexes, err := json.Marshal(move.TileIndexes)
	if err != nil {
		return fmt.Errorf("encode tile indexes: %w", err)
	}
	rows, err := queries.UpdateGameState(ctx, generated.UpdateGameStateParams{
		TileOwners: cloneBytes(state.TileOwners), CurrentPlayer: state.CurrentPlayer,
		Status: state.Status, UpdatedAt: state.UpdatedAt, ID: gameID, Version: expectedVersion,
	})
	if err != nil {
		return fmt.Errorf("update game state: %w", err)
	}
	if rows != 1 {
		return ErrVersionConflict
	}
	if err := queries.CreateMove(ctx, generated.CreateMoveParams{
		GameID: move.GameID, Player: move.Player, TurnNumber: move.TurnNumber,
		TileIndexes: string(tileIndexes), Word: move.Word, CreatedAt: move.CreatedAt,
	}); err != nil {
		return fmt.Errorf("create move: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit move transaction: %w", err)
	}
	return nil
}

// ListMoves retrieves accepted moves in turn order.
func (repository *Repository) ListMoves(ctx context.Context, gameID string) ([]Move, error) {
	rows, err := repository.queries.ListMoves(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("list moves: %w", err)
	}
	moves := make([]Move, 0, len(rows))
	for _, row := range rows {
		var tileIndexes []int64
		if err := json.Unmarshal([]byte(row.TileIndexes), &tileIndexes); err != nil {
			return nil, fmt.Errorf("decode tile indexes for move %d: %w", row.ID, err)
		}
		moves = append(moves, Move{
			ID: row.ID, GameID: row.GameID, Player: row.Player, TurnNumber: row.TurnNumber,
			TileIndexes: tileIndexes, Word: row.Word, CreatedAt: row.CreatedAt,
		})
	}
	return moves, nil
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

func registeredPlayerFromRow(row generated.RegisteredPlayer) RegisteredPlayer {
	return RegisteredPlayer{
		ID: row.ID, DisplayName: row.DisplayName,
		NormalizedName: row.NormalizedName, CreatedAt: row.CreatedAt,
	}
}

func allocationFromRow(row generated.GameAllocation) GameAllocation {
	allocation := GameAllocation{
		ID: row.ID, RequesterID: row.RequesterID, Mode: row.Mode,
		AllowedOpponentType: row.AllowedOpponentType, State: row.State,
		IdempotencyKey: row.IdempotencyKey, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.OpponentID.Valid {
		allocation.OpponentID = row.OpponentID.String
	}
	if row.GameID.Valid {
		allocation.GameID = row.GameID.String
	}
	if row.FailureReason.Valid {
		allocation.FailureReason = row.FailureReason.String
	}
	if row.ExpiresAt.Valid {
		expiresAt := row.ExpiresAt.Time
		allocation.ExpiresAt = &expiresAt
	}
	return allocation
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func nullableTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *value, Valid: true}
}
