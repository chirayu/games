package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"letterpress/backend/internal/app"
	"letterpress/backend/internal/store/sqlite"
)

func TestNameEntryCreatesOrLogsInAndSessionsLogoutIndependently(t *testing.T) {
	handler, repository, now := newIntegrationHandler(t, true)

	firstResponse, firstPlayer := enterPlayer(t, handler, "  Blue   Fox  ")
	if firstResponse.Code != http.StatusCreated || firstPlayer.Name != "Blue Fox" || firstPlayer.ID == "" {
		t.Fatalf("first entry = %d/%#v, want created Blue Fox", firstResponse.Code, firstPlayer)
	}
	firstCookie := requirePlayerSessionCookie(t, firstResponse, true, now.Add(sessionLifetime))
	assertRegisteredSessionHash(t, repository, firstCookie.Value, firstPlayer.ID, now)

	secondResponse, secondPlayer := enterPlayer(t, handler, "blue fox")
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second entry status = %d, want 200; body = %s", secondResponse.Code, secondResponse.Body.String())
	}
	if secondPlayer != firstPlayer {
		t.Fatalf("second player = %#v, want %#v", secondPlayer, firstPlayer)
	}
	secondCookie := requirePlayerSessionCookie(t, secondResponse, true, now.Add(sessionLifetime))
	if secondCookie.Value == firstCookie.Value {
		t.Fatal("independent browsers received the same session token")
	}

	if current := getCurrentPlayer(t, handler, firstCookie, http.StatusOK); current != firstPlayer {
		t.Fatalf("current player = %#v, want %#v", current, firstPlayer)
	}
	logoutRequest := newMutationRequest(http.MethodDelete, "/api/v1/player-sessions/current", nil)
	logoutRequest.AddCookie(firstCookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204; body = %s", logoutResponse.Code, logoutResponse.Body.String())
	}
	cleared := logoutResponse.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != playerSessionCookieName || cleared[0].MaxAge != -1 || cleared[0].Path != "/" {
		t.Fatalf("cleared cookie = %#v", cleared)
	}
	getCurrentPlayer(t, handler, firstCookie, http.StatusUnauthorized)
	if current := getCurrentPlayer(t, handler, secondCookie, http.StatusOK); current != firstPlayer {
		t.Fatalf("second browser after logout = %#v, want %#v", current, firstPlayer)
	}
}

func TestPlayerSessionRejectsInvalidNamesJSONOriginAndExpiredCredentials(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	for _, name := range []string{"", "   ", "Blue!", "José", "abcdefghijklmnopqrstuvwxy"} {
		response, _ := enterPlayer(t, handler, name)
		assertAPIError(t, response, http.StatusUnprocessableEntity, "invalid_player_name")
		if len(response.Result().Cookies()) != 0 {
			t.Fatalf("invalid name %q set a cookie", name)
		}
	}
	botLogin, _ := enterPlayer(t, handler, "Balanced Bot")
	assertAPIError(t, botLogin, http.StatusUnprocessableEntity, "invalid_player_name")
	if len(botLogin.Result().Cookies()) != 0 {
		t.Fatal("managed bot name entry set a public session cookie")
	}

	badJSON := newMutationRequest(http.MethodPost, "/api/v1/player-sessions", bytes.NewBufferString(`{"name":true}`))
	badJSON.Header.Set("Content-Type", "application/json")
	badJSONResponse := httptest.NewRecorder()
	handler.ServeHTTP(badJSONResponse, badJSON)
	assertAPIError(t, badJSONResponse, http.StatusBadRequest, "invalid_request")

	badOrigin := httptest.NewRequest(http.MethodPost, testOrigin+"/api/v1/player-sessions", bytes.NewBufferString(`{"name":"Alice"}`))
	badOrigin.Header.Set("Origin", "https://attacker.example")
	badOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(badOriginResponse, badOrigin)
	assertAPIError(t, badOriginResponse, http.StatusForbidden, "invalid_origin")

	getCurrentPlayer(t, handler, nil, http.StatusUnauthorized)

	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "expiring.db"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	now := time.Date(2026, time.November, 8, 6, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	expiringHandler := NewHandler(testServiceAt(repository, clock), Config{InsecureCookies: true, Now: clock})
	loginResponse, _ := enterPlayer(t, expiringHandler, "Alice")
	cookie := requirePlayerSessionCookie(t, loginResponse, false, now.Add(sessionLifetime))
	now = now.Add(sessionLifetime + time.Second)
	getCurrentPlayer(t, expiringHandler, cookie, http.StatusUnauthorized)
}

func TestLogoutRequiresAValidOriginAndSession(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	request := httptest.NewRequest(http.MethodDelete, testOrigin+"/api/v1/player-sessions/current", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusForbidden, "invalid_origin")

	request = newMutationRequest(http.MethodDelete, "/api/v1/player-sessions/current", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusUnauthorized, "player_session_required")
	if cookies := response.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("invalid logout clear cookie = %#v", cookies)
	}
}

func enterPlayer(t *testing.T, handler http.Handler, name string) (*httptest.ResponseRecorder, app.RegisteredPlayer) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		t.Fatalf("encode name entry: %v", err)
	}
	request := newMutationRequest(http.MethodPost, "/api/v1/player-sessions", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK && response.Code != http.StatusCreated {
		return response, app.RegisteredPlayer{}
	}
	var envelope struct {
		Data app.RegisteredPlayer `json:"data"`
	}
	decodeResponse(t, response, &envelope)
	return response, envelope.Data
}

func getCurrentPlayer(t *testing.T, handler http.Handler, cookie *http.Cookie, status int) app.RegisteredPlayer {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/players/me", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if status != http.StatusOK {
		assertAPIError(t, response, status, "player_session_required")
		return app.RegisteredPlayer{}
	}
	if response.Code != status || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("current player response = %d/%q", response.Code, response.Header().Get("Cache-Control"))
	}
	var envelope struct {
		Data app.RegisteredPlayer `json:"data"`
	}
	decodeResponse(t, response, &envelope)
	return envelope.Data
}

func requirePlayerSessionCookie(
	t *testing.T,
	response *httptest.ResponseRecorder,
	secure bool,
	expires time.Time,
) *http.Cookie {
	t.Helper()
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != playerSessionCookieName || cookie.Value == "" || cookie.Path != "/" ||
		!cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode ||
		cookie.MaxAge != int(sessionLifetime.Seconds()) || !cookie.Expires.Equal(expires) {
		t.Fatalf("session cookie = %#v", cookie)
	}
	return cookie
}

func assertRegisteredSessionHash(
	t *testing.T,
	repository *sqlite.Repository,
	token, playerID string,
	now time.Time,
) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Fatalf("decode session token = %d/%v", len(raw), err)
	}
	hash := sha256.Sum256(raw)
	player, err := repository.GetRegisteredPlayerBySessionHash(context.Background(), hash, now)
	if err != nil || player.ID != playerID {
		t.Fatalf("stored session player = %#v/%v", player, err)
	}
	if reflect.DeepEqual(hash[:], raw) {
		t.Fatal("stored session hash equals raw credential")
	}
}
