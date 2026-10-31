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
	// ErrPlayerNotFound indicates that a capability does not match a player seat.
	ErrPlayerNotFound = errors.New("player not found")
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

// Player is one seat in a game. TokenHash is the only credential persisted.
type Player struct {
	GameID    string
	Seat      int64
	TokenHash [32]byte
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
type MoveOperation func(Game, Player, []Move) (Move, GameState, error)

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

// CreateGame atomically creates a game and both player seats.
func (repository *Repository) CreateGame(ctx context.Context, game Game, players [2]Player) error {
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

	for index, player := range players {
		if player.GameID != game.ID {
			return fmt.Errorf("player %d belongs to game %q, want %q", index+1, player.GameID, game.ID)
		}
		if err := queries.CreatePlayer(ctx, generated.CreatePlayerParams{
			GameID:    player.GameID,
			Seat:      player.Seat,
			TokenHash: player.TokenHash[:],
			CreatedAt: player.CreatedAt,
		}); err != nil {
			return fmt.Errorf("create player %d: %w", player.Seat, err)
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

// ListPlayers retrieves both seats in stable seat order.
func (repository *Repository) ListPlayers(ctx context.Context, gameID string) ([]Player, error) {
	rows, err := repository.queries.ListPlayers(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("list players: %w", err)
	}
	players := make([]Player, 0, len(rows))
	for _, row := range rows {
		var tokenHash [32]byte
		copy(tokenHash[:], row.TokenHash)
		players = append(players, Player{
			GameID:    row.GameID,
			Seat:      row.Seat,
			TokenHash: tokenHash,
			CreatedAt: row.CreatedAt,
		})
	}
	return players, nil
}

// GetPlayerByTokenHash finds a seat by its already-hashed access token.
func (repository *Repository) GetPlayerByTokenHash(ctx context.Context, gameID string, tokenHash [32]byte) (Player, error) {
	row, err := repository.queries.GetPlayerByTokenHash(ctx, generated.GetPlayerByTokenHashParams{
		GameID: gameID, TokenHash: tokenHash[:],
	})
	if err != nil {
		return Player{}, fmt.Errorf("get player by token hash: %w", err)
	}
	var storedHash [32]byte
	copy(storedHash[:], row.TokenHash)
	return Player{GameID: row.GameID, Seat: row.Seat, TokenHash: storedHash, CreatedAt: row.CreatedAt}, nil
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
	tokenHash [32]byte,
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
	playerRow, err := queries.GetPlayerByTokenHash(ctx, generated.GetPlayerByTokenHashParams{
		GameID: gameID, TokenHash: tokenHash[:],
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPlayerNotFound
	}
	if err != nil {
		return fmt.Errorf("authenticate move player: %w", err)
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
	var token [32]byte
	copy(token[:], playerRow.TokenHash)
	player := Player{GameID: playerRow.GameID, Seat: playerRow.Seat, TokenHash: token, CreatedAt: playerRow.CreatedAt}
	move, state, err := operation(storedGame, player, moves)
	if err != nil {
		return err
	}
	if move.GameID != gameID || move.Player != player.Seat || move.TurnNumber != int64(len(moves)+1) {
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
