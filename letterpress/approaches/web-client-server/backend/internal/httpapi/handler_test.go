package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"letterpress/backend/internal/app"
	"letterpress/backend/internal/game"
	"letterpress/backend/internal/store/sqlite"
)

const testOrigin = "https://letterpress.test"

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewHandler(nil, Config{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	decodeResponse(t, response, &body)
	if body.Data.Status != "ok" {
		t.Fatalf("status = %q, want ok", body.Data.Status)
	}
}

func TestHealthRejectsOtherMethods(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	response := httptest.NewRecorder()

	NewHandler(nil, Config{}).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestReadinessChecksSQLite(t *testing.T) {
	handler, repository, _ := newIntegrationHandler(t, true)
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ready status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close repository: %v", err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusServiceUnavailable, "not_ready")
}

func TestFrontendAssetsAndSPAFallbackShareTheAPIOrigin(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html":        {Data: []byte("<html><body>Letterpress app</body></html>")},
		"assets/app-123.js": {Data: []byte("console.log('ready')")},
	}
	handler := NewHandler(nil, Config{Frontend: frontend})
	for _, requestPath := range []string{"/", "/games/game-123"} {
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Letterpress app") {
			t.Fatalf("GET %s = %d %q", requestPath, response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("GET %s Cache-Control = %q", requestPath, response.Header().Get("Cache-Control"))
		}
	}
	assetRequest := httptest.NewRequest(http.MethodGet, "/assets/app-123.js", nil)
	assetResponse := httptest.NewRecorder()
	handler.ServeHTTP(assetResponse, assetRequest)
	if assetResponse.Code != http.StatusOK || !strings.Contains(assetResponse.Body.String(), "ready") {
		t.Fatalf("asset = %d %q", assetResponse.Code, assetResponse.Body.String())
	}
	if assetResponse.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset Cache-Control = %q", assetResponse.Header().Get("Cache-Control"))
	}
	apiRequest := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Code != http.StatusNotFound {
		t.Fatalf("unknown API status = %d, want 404", apiResponse.Code)
	}
}

func TestAssignedGamePersistsRegisteredPlayerSeats(t *testing.T) {
	handler, repository, now := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	game, err := repository.GetGame(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("get created game: %v", err)
	}
	if len(game.BoardLetters) != 25 || len(game.TileOwners) != 25 || game.CurrentPlayer != 1 || game.Status != "active" {
		t.Fatalf("created game = %#v, want a fresh 5x5 active game", game)
	}
	players, err := repository.ListGameSeats(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("list created players: %v", err)
	}
	if len(players) != 2 || players[0].Seat != 1 || players[1].Seat != 2 {
		t.Fatalf("players = %#v, want seats 1 and 2", players)
	}
	if players[0].PlayerID != created.PlayerOne.ID || players[1].PlayerID != created.PlayerTwo.ID {
		t.Fatalf("seat players = %#v, want %q/%q", players, created.PlayerOne.ID, created.PlayerTwo.ID)
	}
	if !created.PlayerOneCookie.Expires.Equal(now.Add(sessionLifetime)) || !created.PlayerTwoCookie.Expires.Equal(now.Add(sessionLifetime)) {
		t.Fatal("player session lifetime changed while assigning a game")
	}
}

func TestRemovedDirectCreationAndGameSessionRoutesAreUnavailable(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, false)
	for _, test := range []struct {
		target string
		status int
	}{
		{target: "/api/v1/games", status: http.StatusMethodNotAllowed},
		{target: "/api/v1/games/unknown/sessions", status: http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, newMutationRequest(http.MethodPost, test.target, strings.NewReader(`{}`)))
		if response.Code != test.status {
			t.Fatalf("POST %s = %d, want %d", test.target, response.Code, test.status)
		}
	}
}

func TestGetGameReturnsSharedBoardWithPlayerPerspective(t *testing.T) {
	handler, repository, now := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	stored, err := repository.GetGame(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("get created game: %v", err)
	}
	owners := make([]byte, 25)
	for _, index := range []int{0, 1, 5} {
		owners[index] = 1
	}
	for _, index := range []int{19, 23, 24} {
		owners[index] = 2
	}
	if err := repository.ApplyMove(context.Background(), created.GameID, 0, sqlite.Move{
		GameID: created.GameID, Player: 1, TurnNumber: 1, TileIndexes: []int64{0, 1, 5},
		Word: string([]byte{stored.BoardLetters[0], stored.BoardLetters[1], stored.BoardLetters[5]}), CreatedAt: now,
	}, sqlite.GameState{TileOwners: owners, CurrentPlayer: 2, Status: "active", UpdatedAt: now}); err != nil {
		t.Fatalf("seed game state: %v", err)
	}

	playerOne := loadGame(t, handler, created.GameID, created.PlayerOneCookie)
	playerTwo := loadGame(t, handler, created.GameID, created.PlayerTwoCookie)
	if playerOne.PlayerSeat != 1 || playerTwo.PlayerSeat != 2 {
		t.Fatalf("player seats = %d/%d, want 1/2", playerOne.PlayerSeat, playerTwo.PlayerSeat)
	}
	playerOne.PlayerSeat = 0
	playerTwo.PlayerSeat = 0
	if !reflect.DeepEqual(playerOne, playerTwo) {
		t.Fatalf("player board views differ:\nplayer one: %#v\nplayer two: %#v", playerOne, playerTwo)
	}
	if playerOne.Width != 5 || playerOne.Height != 5 || len(playerOne.Tiles) != 25 {
		t.Fatalf("board dimensions = %dx%d with %d tiles", playerOne.Width, playerOne.Height, len(playerOne.Tiles))
	}
	if playerOne.ActivePlayer != 2 || playerOne.Version != 1 || playerOne.Status != "active" {
		t.Fatalf("state = active player %d, version %d, status %q", playerOne.ActivePlayer, playerOne.Version, playerOne.Status)
	}
	if playerOne.Scores.PlayerOne != 3 || playerOne.Scores.PlayerTwo != 3 {
		t.Fatalf("scores = %#v, want 3/3", playerOne.Scores)
	}
	if !playerOne.Tiles[0].Protected || !playerOne.Tiles[24].Protected || playerOne.Tiles[1].Protected {
		t.Fatalf("derived protection is incorrect: tiles 0=%t, 24=%t, 1=%t", playerOne.Tiles[0].Protected, playerOne.Tiles[24].Protected, playerOne.Tiles[1].Protected)
	}
}

func TestGetGameReportsUnauthorizedAndMissingStates(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	created := createGame(t, handler)

	request := httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/games/"+created.GameID, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusUnauthorized, "unauthorized")

	request = httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/games/"+created.GameID, nil)
	request.AddCookie(&http.Cookie{Name: playerSessionCookieName, Value: "invalid"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusUnauthorized, "unauthorized")

	missingID := "missing-game"
	request = httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/games/"+missingID, nil)
	request.AddCookie(created.PlayerOneCookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusUnauthorized, "unauthorized")
}

func TestSubmitMovePersistsDerivedWordAndProtectsOwnership(t *testing.T) {
	handler, repository, _ := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	playerOneCookie := created.PlayerOneCookie
	playerTwoCookie := created.PlayerTwoCookie
	initial := loadGame(t, handler, created.GameID, playerOneCookie)

	first := submitMove(t, handler, created.GameID, playerOneCookie, []int{0, 1, 5}, 0)
	firstView := decodeGameView(t, first, http.StatusOK)
	if firstView.Version != 1 || firstView.ActivePlayer != 2 || firstView.Scores.PlayerOne != 3 {
		t.Fatalf("first move state = version %d, active %d, scores %#v", firstView.Version, firstView.ActivePlayer, firstView.Scores)
	}
	if !firstView.Tiles[0].Protected || firstView.Tiles[0].Owner != 1 {
		t.Fatalf("corner after first move = %#v, want protected Player 1 tile", firstView.Tiles[0])
	}

	second := submitMove(t, handler, created.GameID, playerTwoCookie, []int{0, 2, 3, 4}, 1)
	secondView := decodeGameView(t, second, http.StatusOK)
	if secondView.Version != 2 || secondView.ActivePlayer != 1 || secondView.Scores.PlayerOne != 3 || secondView.Scores.PlayerTwo != 3 {
		t.Fatalf("second move state = version %d, active %d, scores %#v", secondView.Version, secondView.ActivePlayer, secondView.Scores)
	}
	if secondView.Tiles[0].Owner != 1 || !secondView.Tiles[0].Protected {
		t.Fatalf("opponent changed protected corner: %#v", secondView.Tiles[0])
	}
	for _, index := range []int{2, 3, 4} {
		if secondView.Tiles[index].Owner != 2 {
			t.Fatalf("tile %d owner = %d, want Player 2", index, secondView.Tiles[index].Owner)
		}
	}

	moves, err := repository.ListMoves(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("list moves: %v", err)
	}
	if len(moves) != 2 {
		t.Fatalf("move count = %d, want 2", len(moves))
	}
	wantFirstWord := initial.Tiles[0].Letter + initial.Tiles[1].Letter + initial.Tiles[5].Letter
	wantSecondWord := initial.Tiles[0].Letter + initial.Tiles[2].Letter + initial.Tiles[3].Letter + initial.Tiles[4].Letter
	if moves[0].Word != wantFirstWord || moves[1].Word != wantSecondWord {
		t.Fatalf("stored words = %q/%q, want %q/%q", moves[0].Word, moves[1].Word, wantFirstWord, wantSecondWord)
	}
}

func TestSubmitMoveReturnsStructuredRuleAndVersionErrors(t *testing.T) {
	handler, repository, _ := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	playerOneCookie := created.PlayerOneCookie
	playerTwoCookie := created.PlayerTwoCookie

	assertAPIError(t, submitMove(t, handler, created.GameID, nil, []int{0}, 0), http.StatusUnauthorized, "unauthorized")
	assertAPIError(t, submitMove(t, handler, created.GameID, playerOneCookie, []int{0, 0}, 0), http.StatusUnprocessableEntity, "duplicate_tile")
	assertAPIError(t, submitMove(t, handler, created.GameID, playerOneCookie, nil, 0), http.StatusUnprocessableEntity, "invalid_word")
	assertAPIError(t, submitMove(t, handler, created.GameID, playerOneCookie, []int{25}, 0), http.StatusUnprocessableEntity, "invalid_tile")
	assertAPIError(t, submitMove(t, handler, created.GameID, playerTwoCookie, []int{0}, 0), http.StatusConflict, "out_of_turn")

	decodeGameView(t, submitMove(t, handler, created.GameID, playerOneCookie, []int{0, 1, 2}, 0), http.StatusOK)
	assertAPIError(t, submitMove(t, handler, created.GameID, playerTwoCookie, []int{0, 1, 2}, 1), http.StatusUnprocessableEntity, "repeated_word")
	assertAPIError(t, submitMove(t, handler, created.GameID, playerTwoCookie, []int{0, 1}, 1), http.StatusUnprocessableEntity, "prefix_word")
	assertAPIError(t, submitMove(t, handler, created.GameID, playerTwoCookie, []int{3, 4}, 0), http.StatusConflict, "version_conflict")

	moves, err := repository.ListMoves(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("list moves: %v", err)
	}
	if len(moves) != 1 {
		t.Fatalf("rejected requests persisted %d moves, want 1", len(moves))
	}
}

func TestDictionaryValidationAcceptsEnglishAndRejectsGibberishAtomically(t *testing.T) {
	handler, repository := newDictionaryIntegrationHandler(t, filepath.Join(t.TempDir(), "letterpress.db"))
	created := createGame(t, handler)
	playerOne := created.PlayerOneCookie
	playerTwo := created.PlayerTwoCookie

	accepted := decodeGameView(t, submitMove(t, handler, created.GameID, playerOne, []int{0, 1, 2}, 0), http.StatusOK)
	if len(accepted.Moves) != 1 || accepted.Moves[0].Word != "CAT" || accepted.Version != 1 {
		t.Fatalf("accepted dictionary move = %#v", accepted)
	}
	storedBefore, err := repository.GetGame(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("get state before rejection: %v", err)
	}
	movesBefore, err := repository.ListMoves(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("list moves before rejection: %v", err)
	}

	assertAPIError(t, submitMove(t, handler, created.GameID, playerTwo, []int{3, 4, 6, 7, 8}, 1), http.StatusUnprocessableEntity, "invalid_word")
	storedAfter, err := repository.GetGame(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("get state after rejection: %v", err)
	}
	movesAfter, err := repository.ListMoves(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("list moves after rejection: %v", err)
	}
	if !reflect.DeepEqual(storedBefore, storedAfter) || !reflect.DeepEqual(movesBefore, movesAfter) {
		t.Fatalf("invalid word changed state:\nbefore %#v / %#v\nafter %#v / %#v", storedBefore, movesBefore, storedAfter, movesAfter)
	}

	decodeGameView(t, submitMove(t, handler, created.GameID, playerTwo, []int{3, 4, 5}, 1), http.StatusOK)
	assertAPIError(t, submitMove(t, handler, created.GameID, playerOne, []int{0, 1, 2}, 2), http.StatusUnprocessableEntity, "repeated_word")
	assertAPIError(t, submitMove(t, handler, created.GameID, playerOne, []int{0, 1}, 2), http.StatusUnprocessableEntity, "prefix_word")
}

func TestDictionaryValidationRemainsDeterministicAcrossRestart(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "letterpress.db")
	handler, repository := newDictionaryIntegrationHandler(t, databasePath)
	created := createGame(t, handler)
	playerOne := created.PlayerOneCookie
	playerTwo := created.PlayerTwoCookie
	decodeGameView(t, submitMove(t, handler, created.GameID, playerOne, []int{0, 1, 2}, 0), http.StatusOK)
	if err := repository.Close(); err != nil {
		t.Fatalf("close dictionary repository: %v", err)
	}

	restarted, _ := newDictionaryIntegrationHandler(t, databasePath)
	assertAPIError(t, submitMove(t, restarted, created.GameID, playerTwo, []int{3, 4, 6, 7, 8}, 1), http.StatusUnprocessableEntity, "invalid_word")
	view := decodeGameView(t, submitMove(t, restarted, created.GameID, playerTwo, []int{3, 4, 5}, 1), http.StatusOK)
	if len(view.Moves) != 2 || view.Moves[1].Word != "DOG" {
		t.Fatalf("post-restart move history = %#v", view.Moves)
	}
}

func TestConcurrentDictionaryValidationCreatesOneTurn(t *testing.T) {
	handler, repository := newDictionaryIntegrationHandler(t, filepath.Join(t.TempDir(), "letterpress.db"))
	created := createGame(t, handler)
	cookie := created.PlayerOneCookie
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, indexes := range [][]int{{0, 1, 2}, {6, 7, 8}} {
		indexes := indexes
		go func() {
			<-start
			responses <- submitMove(t, handler, created.GameID, cookie, indexes, 0)
		}()
	}
	close(start)
	first, second := <-responses, <-responses
	if !((first.Code == http.StatusOK && second.Code == http.StatusConflict) || (second.Code == http.StatusOK && first.Code == http.StatusConflict)) {
		t.Fatalf("concurrent dictionary statuses = %d/%d, want 200/409", first.Code, second.Code)
	}
	moves, err := repository.ListMoves(context.Background(), created.GameID)
	if err != nil || len(moves) != 1 {
		t.Fatalf("concurrent dictionary moves = %#v, err %v", moves, err)
	}
}

func TestConcurrentDoubleSubmissionCreatesOneTurn(t *testing.T) {
	handler, repository, _ := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	cookie := created.PlayerOneCookie

	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, indexes := range [][]int{{0, 1}, {2, 3}} {
		indexes := indexes
		go func() {
			<-start
			responses <- submitMove(t, handler, created.GameID, cookie, indexes, 0)
		}()
	}
	close(start)
	first, second := <-responses, <-responses
	statuses := []int{first.Code, second.Code}
	if !((statuses[0] == http.StatusOK && statuses[1] == http.StatusConflict) || (statuses[1] == http.StatusOK && statuses[0] == http.StatusConflict)) {
		t.Fatalf("concurrent statuses = %v, want 200 and 409", statuses)
	}
	conflict := first
	if conflict.Code == http.StatusOK {
		conflict = second
	}
	assertAPIError(t, conflict, http.StatusConflict, "version_conflict")
	moves, err := repository.ListMoves(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("list moves: %v", err)
	}
	if len(moves) != 1 {
		t.Fatalf("move count = %d, want 1", len(moves))
	}
}

func TestFinalMovePersistsWinnerHistoryAndRejectsFurtherPlay(t *testing.T) {
	handler, repository, _ := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	playerOneCookie := created.PlayerOneCookie
	playerTwoCookie := created.PlayerTwoCookie
	indexes := make([]int, 25)
	for index := range indexes {
		indexes[index] = index
	}

	completed := decodeGameView(t, submitMove(t, handler, created.GameID, playerOneCookie, indexes, 0), http.StatusOK)
	if completed.Status != "completed" || completed.Version != 1 || completed.Result == nil {
		t.Fatalf("completed state = status %q version %d result %#v", completed.Status, completed.Version, completed.Result)
	}
	if completed.Result.Winner != 1 || completed.Result.Tie || completed.Result.PlayerOneScore != 25 || completed.Result.PlayerTwoScore != 0 {
		t.Fatalf("result = %#v, want Player 1 25-0", completed.Result)
	}
	if len(completed.Moves) != 1 || completed.Moves[0].TurnNumber != 1 || len(completed.Moves[0].TileIndexes) != 25 {
		t.Fatalf("move history = %#v, want one complete-board move", completed.Moves)
	}

	playerTwo := loadGame(t, handler, created.GameID, playerTwoCookie)
	if !reflect.DeepEqual(completed.Result, playerTwo.Result) || !reflect.DeepEqual(completed.Moves, playerTwo.Moves) {
		t.Fatalf("players received different final views: %#v / %#v", completed, playerTwo)
	}
	assertAPIError(t, submitMove(t, handler, created.GameID, playerOneCookie, []int{0}, 1), http.StatusConflict, "game_completed")

	stored, err := repository.GetGame(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("get completed game: %v", err)
	}
	if stored.Status != "completed" || stored.Version != 1 {
		t.Fatalf("stored final state = status %q version %d", stored.Status, stored.Version)
	}
}

func TestGameAndSessionResumeAfterRepositoryRestart(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "letterpress.db")
	repository, err := sqlite.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	now := time.Date(2026, time.November, 2, 8, 0, 0, 0, time.UTC)
	service := testService(repository)
	handler := newTestHTTPHandler(service, Config{InsecureCookies: true, Now: func() time.Time { return now }})
	created := createGame(t, handler)
	cookie := created.PlayerOneCookie
	before := loadGame(t, handler, created.GameID, cookie)
	if err := repository.Close(); err != nil {
		t.Fatalf("close repository: %v", err)
	}

	reopened, err := sqlite.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatalf("reopen repository: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restartedHandler := NewHandler(testService(reopened), Config{InsecureCookies: true, Now: func() time.Time { return now.Add(time.Hour) }})
	after := loadGame(t, restartedHandler, created.GameID, cookie)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed across restart:\nbefore: %#v\nafter: %#v", before, after)
	}
}

func TestLocalDevelopmentCanDisableSecureCookie(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, false)
	created := createGame(t, handler)
	if created.PlayerOneCookie.Secure || created.PlayerTwoCookie.Secure {
		t.Fatalf("local development cookies should not be Secure: %#v / %#v", created.PlayerOneCookie, created.PlayerTwoCookie)
	}
}

func TestMutationsRequireAnAllowedOrigin(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	for _, origin := range []string{"", "https://attacker.example"} {
		request := httptest.NewRequest(http.MethodPost, testOrigin+"/api/v1/games/"+created.GameID+"/moves", strings.NewReader(`{}`))
		request.Header.Set("Origin", origin)
		request.AddCookie(created.PlayerOneCookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assertAPIError(t, response, http.StatusForbidden, "invalid_origin")
	}
}

func TestRegisteredNonMemberCannotDiscoverOrAccessGame(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	first := createGame(t, handler)
	second := createGame(t, handler)
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/games/"+first.GameID, nil)
	request.AddCookie(second.PlayerOneCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusUnauthorized, "unauthorized")
}

type createdGameResponse struct {
	GameID          string
	PlayerOne       app.RegisteredPlayer
	PlayerTwo       app.RegisteredPlayer
	PlayerOneCookie *http.Cookie
	PlayerTwoCookie *http.Cookie
}

type testHTTPHandler struct {
	http.Handler
	service *app.Service
}

func newTestHTTPHandler(service *app.Service, config Config) http.Handler {
	return &testHTTPHandler{Handler: NewHandler(service, config), service: service}
}

func newIntegrationHandler(t *testing.T, secureCookies bool) (http.Handler, *sqlite.Repository, time.Time) {
	t.Helper()
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "letterpress.db"))
	if err != nil {
		t.Fatalf("open test repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	now := time.Date(2026, time.October, 31, 9, 0, 0, 0, time.UTC)
	service := testServiceAt(repository, func() time.Time { return now })
	return newTestHTTPHandler(service, Config{InsecureCookies: !secureCookies, Now: func() time.Time { return now }}), repository, now
}

func testService(repository *sqlite.Repository) *app.Service {
	return app.NewServiceWithDependencies(repository, game.NewCryptoBoardGenerator(), game.MVPWordValidator{})
}

func testServiceAt(repository *sqlite.Repository, now func() time.Time) *app.Service {
	return app.NewServiceWithDependenciesAndClock(repository, game.NewCryptoBoardGenerator(), game.MVPWordValidator{}, now)
}

func newDictionaryIntegrationHandler(t *testing.T, databasePath string) (http.Handler, *sqlite.Repository) {
	t.Helper()
	repository, err := sqlite.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatalf("open dictionary repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	generator, err := game.NewFixedBoardGenerator("CATDOGSUNMAPEXTRAORDINARY")
	if err != nil {
		t.Fatalf("create dictionary test board: %v", err)
	}
	service := app.NewServiceWithDependencies(repository, generator, game.EnglishDictionaryValidator())
	now := time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC)
	return newTestHTTPHandler(service, Config{InsecureCookies: true, Now: func() time.Time { return now }}), repository
}

var createdGameSequence atomic.Uint64

func createGame(t *testing.T, handler http.Handler) createdGameResponse {
	t.Helper()
	testHandler, ok := handler.(*testHTTPHandler)
	if !ok {
		t.Fatal("game assignment requires the integration test handler")
	}
	sequence := createdGameSequence.Add(1)
	firstResponse, first := enterPlayer(t, handler, fmt.Sprintf("Player %d A", sequence))
	secondResponse, second := enterPlayer(t, handler, fmt.Sprintf("Player %d B", sequence))
	firstCookies, secondCookies := firstResponse.Result().Cookies(), secondResponse.Result().Cookies()
	if len(firstCookies) != 1 || len(secondCookies) != 1 {
		t.Fatalf("player session cookies = %d/%d, want 1/1", len(firstCookies), len(secondCookies))
	}
	gameID, err := testHandler.service.CreateGame(context.Background(), first.ID, second.ID)
	if err != nil {
		t.Fatalf("assign game: %v", err)
	}
	return createdGameResponse{
		GameID: gameID, PlayerOne: first, PlayerTwo: second,
		PlayerOneCookie: firstCookies[0], PlayerTwoCookie: secondCookies[0],
	}
}

func loadGame(t *testing.T, handler http.Handler, gameID string, cookie *http.Cookie) app.GameView {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/games/"+gameID, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("get game status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	var body struct {
		Data app.GameView `json:"data"`
	}
	decodeResponse(t, response, &body)
	return body.Data
}

func submitMove(
	t *testing.T,
	handler http.Handler,
	gameID string,
	cookie *http.Cookie,
	tileIndexes []int,
	expectedVersion int64,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"tileIndexes": tileIndexes, "expectedVersion": expectedVersion})
	if err != nil {
		t.Fatalf("encode move request: %v", err)
	}
	request := newMutationRequest(http.MethodPost, fmt.Sprintf("/api/v1/games/%s/moves", gameID), bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeGameView(t *testing.T, response *httptest.ResponseRecorder, status int) app.GameView {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
	var body struct {
		Data app.GameView `json:"data"`
	}
	decodeResponse(t, response, &body)
	return body.Data
}

func newMutationRequest(method, path string, body io.Reader) *http.Request {
	var request *http.Request
	if body == nil {
		request = httptest.NewRequest(method, testOrigin+path, nil)
	} else {
		request = httptest.NewRequest(method, testOrigin+path, body)
	}
	request.Header.Set("Origin", testOrigin)
	return request
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", response.Header().Get("Content-Type"))
	}
	var body struct {
		Error errorBody `json:"error"`
	}
	decodeResponse(t, response, &body)
	if body.Error.Code != code || body.Error.Message == "" {
		t.Fatalf("error = %#v, want code %q and a message", body.Error, code)
	}
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
