package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"letterpress/backend/internal/app"
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

func TestCreateGameAndExchangeDistinctPlayerSessions(t *testing.T) {
	handler, repository, now := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	if created.PlayerOneToken == created.PlayerTwoToken {
		t.Fatal("player invitation tokens are identical")
	}
	game, err := repository.GetGame(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("get created game: %v", err)
	}
	if len(game.BoardLetters) != 25 || len(game.TileOwners) != 25 || game.CurrentPlayer != 1 || game.Status != "active" {
		t.Fatalf("created game = %#v, want a fresh 5x5 active game", game)
	}
	players, err := repository.ListPlayers(context.Background(), created.GameID)
	if err != nil {
		t.Fatalf("list created players: %v", err)
	}
	if len(players) != 2 || players[0].Seat != 1 || players[1].Seat != 2 {
		t.Fatalf("players = %#v, want seats 1 and 2", players)
	}
	assertStoredHash(t, created.PlayerOneToken, players[0].TokenHash)
	assertStoredHash(t, created.PlayerTwoToken, players[1].TokenHash)

	for _, test := range []struct {
		name  string
		token string
		seat  int64
	}{
		{name: "player one", token: created.PlayerOneToken, seat: 1},
		{name: "player two", token: created.PlayerTwoToken, seat: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := exchangeSession(t, handler, created.GameID, test.token)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			var body struct {
				Data struct {
					GameID string `json:"gameId"`
					Seat   int64  `json:"seat"`
				} `json:"data"`
			}
			decodeResponse(t, response, &body)
			if body.Data.GameID != created.GameID || body.Data.Seat != test.seat {
				t.Fatalf("session = %#v, want game %q seat %d", body.Data, created.GameID, test.seat)
			}

			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies = %d, want 1", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != sessionCookieName(created.GameID) || cookie.Value != test.token {
				t.Fatalf("session cookie name/value = %q/%q", cookie.Name, cookie.Value)
			}
			if cookie.Path != gameAPIPath(created.GameID) || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("session cookie attributes = %#v", cookie)
			}
			if cookie.MaxAge != int(sessionLifetime.Seconds()) || !cookie.Expires.Equal(now.Add(sessionLifetime)) {
				t.Fatalf("session cookie lifetime = %d/%s", cookie.MaxAge, cookie.Expires)
			}
		})
	}
}

func TestSessionRejectsModifiedAndCrossGameSecrets(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, false)
	first := createGame(t, handler)
	second := createGame(t, handler)

	modified := first.PlayerOneToken
	if modified[0] == 'A' {
		modified = "B" + modified[1:]
	} else {
		modified = "A" + modified[1:]
	}
	for _, test := range []struct {
		name   string
		gameID string
		token  string
	}{
		{name: "modified", gameID: first.GameID, token: modified},
		{name: "other game", gameID: second.GameID, token: first.PlayerOneToken},
		{name: "malformed", gameID: first.GameID, token: "not-a-secret"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := exchangeSession(t, handler, test.gameID, test.token)
			assertAPIError(t, response, http.StatusUnauthorized, "invalid_invitation")
			if len(response.Result().Cookies()) != 0 {
				t.Fatal("rejected invitation set a cookie")
			}
		})
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

	playerOne := loadGame(t, handler, created.GameID, sessionCookie(t, handler, created.GameID, created.PlayerOneToken))
	playerTwo := loadGame(t, handler, created.GameID, sessionCookie(t, handler, created.GameID, created.PlayerTwoToken))
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
	request.AddCookie(&http.Cookie{Name: sessionCookieName(created.GameID), Value: "invalid"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusUnauthorized, "unauthorized")

	missingID := "missing-game"
	request = httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/games/"+missingID, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName(missingID), Value: created.PlayerOneToken})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusNotFound, "game_not_found")
}

func TestSubmitMovePersistsDerivedWordAndProtectsOwnership(t *testing.T) {
	handler, repository, _ := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	playerOneCookie := sessionCookie(t, handler, created.GameID, created.PlayerOneToken)
	playerTwoCookie := sessionCookie(t, handler, created.GameID, created.PlayerTwoToken)
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
	playerOneCookie := sessionCookie(t, handler, created.GameID, created.PlayerOneToken)
	playerTwoCookie := sessionCookie(t, handler, created.GameID, created.PlayerTwoToken)

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

func TestConcurrentDoubleSubmissionCreatesOneTurn(t *testing.T) {
	handler, repository, _ := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	cookie := sessionCookie(t, handler, created.GameID, created.PlayerOneToken)

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
	playerOneCookie := sessionCookie(t, handler, created.GameID, created.PlayerOneToken)
	playerTwoCookie := sessionCookie(t, handler, created.GameID, created.PlayerTwoToken)
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
	handler := NewHandler(app.NewService(repository), Config{InsecureCookies: true, Now: func() time.Time { return now }})
	created := createGame(t, handler)
	cookie := sessionCookie(t, handler, created.GameID, created.PlayerOneToken)
	before := loadGame(t, handler, created.GameID, cookie)
	if err := repository.Close(); err != nil {
		t.Fatalf("close repository: %v", err)
	}

	reopened, err := sqlite.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatalf("reopen repository: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restartedHandler := NewHandler(app.NewService(reopened), Config{InsecureCookies: true, Now: func() time.Time { return now.Add(time.Hour) }})
	after := loadGame(t, restartedHandler, created.GameID, cookie)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed across restart:\nbefore: %#v\nafter: %#v", before, after)
	}
}

func TestLocalDevelopmentCanDisableSecureCookie(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, false)
	created := createGame(t, handler)
	response := exchangeSession(t, handler, created.GameID, created.PlayerOneToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatalf("local development cookie = %#v, want one non-Secure cookie", cookies)
	}
}

func TestMutationsRequireAnAllowedOrigin(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	for _, origin := range []string{"", "https://attacker.example"} {
		request := httptest.NewRequest(http.MethodPost, testOrigin+"/api/v1/games", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assertAPIError(t, response, http.StatusForbidden, "invalid_origin")
	}
}

func TestSessionRejectsInvalidJSONWithConsistentError(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	created := createGame(t, handler)
	request := newMutationRequest(http.MethodPost, fmt.Sprintf("/api/v1/games/%s/sessions", created.GameID), strings.NewReader(`{"token":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "invalid_request")
}

type createdGameResponse struct {
	GameID         string `json:"gameId"`
	PlayerOneToken string `json:"playerOneToken"`
	PlayerTwoToken string `json:"playerTwoToken"`
}

func newIntegrationHandler(t *testing.T, secureCookies bool) (http.Handler, *sqlite.Repository, time.Time) {
	t.Helper()
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "letterpress.db"))
	if err != nil {
		t.Fatalf("open test repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	now := time.Date(2026, time.October, 31, 9, 0, 0, 0, time.UTC)
	return NewHandler(app.NewService(repository), Config{InsecureCookies: !secureCookies, Now: func() time.Time { return now }}), repository, now
}

func createGame(t *testing.T, handler http.Handler) createdGameResponse {
	t.Helper()
	request := newMutationRequest(http.MethodPost, "/api/v1/games", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	var body struct {
		Data createdGameResponse `json:"data"`
	}
	decodeResponse(t, response, &body)
	return body.Data
}

func exchangeSession(t *testing.T, handler http.Handler, gameID, token string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		t.Fatalf("encode session request: %v", err)
	}
	request := newMutationRequest(http.MethodPost, fmt.Sprintf("/api/v1/games/%s/sessions", gameID), bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func sessionCookie(t *testing.T, handler http.Handler, gameID, token string) *http.Cookie {
	t.Helper()
	response := exchangeSession(t, handler, gameID, token)
	if response.Code != http.StatusOK {
		t.Fatalf("exchange session status = %d, want %d", response.Code, http.StatusOK)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session cookies = %d, want 1", len(cookies))
	}
	return cookies[0]
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

func assertStoredHash(t *testing.T, token string, stored [32]byte) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("secret length = %d, want 32", len(raw))
	}
	want := sha256.Sum256(raw)
	if !reflect.DeepEqual(stored, want) {
		t.Fatalf("stored hash = %x, want %x", stored, want)
	}
	if strings.Contains(string(stored[:]), token) {
		t.Fatal("stored credential contains the raw invitation token")
	}
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
