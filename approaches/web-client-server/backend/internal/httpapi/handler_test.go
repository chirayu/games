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
