package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"letterpress/backend/internal/app"
	"letterpress/backend/internal/store/sqlite"
)

func TestActiveGameListIsParticipantScopedOrderedAndPaginated(t *testing.T) {
	handler, repository, now := newIntegrationHandler(t, true)
	service := handler.(*testHTTPHandler).service
	alice, aliceCookie := loginLobbyPlayer(t, handler, "Alice")
	bob, bobCookie := loginLobbyPlayer(t, handler, "Bob")
	carol, _ := loginLobbyPlayer(t, handler, "Carol")
	dana, danaCookie := loginLobbyPlayer(t, handler, "Dana")

	firstID, err := service.CreateGame(context.Background(), alice.ID, bob.ID)
	if err != nil {
		t.Fatalf("create first game: %v", err)
	}
	secondID, err := service.CreateGame(context.Background(), alice.ID, carol.ID)
	if err != nil {
		t.Fatalf("create second game: %v", err)
	}
	completedID, err := service.CreateGame(context.Background(), alice.ID, dana.ID)
	if err != nil {
		t.Fatalf("create completed game: %v", err)
	}
	owners := make([]byte, 25)
	owners[0] = 1
	if err := repository.ApplyMove(context.Background(), firstID, 0, sqlite.Move{
		GameID: firstID, Player: 1, TurnNumber: 1, TileIndexes: []int64{0}, Word: "A", CreatedAt: now.Add(time.Minute),
	}, sqlite.GameState{TileOwners: owners, CurrentPlayer: 2, Status: "active", UpdatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("advance first game: %v", err)
	}
	completedOwners := make([]byte, 25)
	for index := range completedOwners {
		completedOwners[index] = 1
	}
	if err := repository.ApplyMove(context.Background(), completedID, 0, sqlite.Move{
		GameID: completedID, Player: 1, TurnNumber: 1, TileIndexes: []int64{0}, Word: "A", CreatedAt: now.Add(2 * time.Minute),
	}, sqlite.GameState{TileOwners: completedOwners, CurrentPlayer: 2, Status: "completed", UpdatedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("complete third game: %v", err)
	}

	firstPage := getActiveGames(t, handler, aliceCookie, "?limit=1", http.StatusOK)
	if len(firstPage.Items) != 1 || firstPage.Items[0].ID != firstID || firstPage.NextCursor == "" {
		t.Fatalf("first page = %#v", firstPage)
	}
	if firstPage.Items[0].Opponent.ID != bob.ID || firstPage.Items[0].Opponent.Name != "Bob" || firstPage.Items[0].Opponent.Type != "human" {
		t.Fatalf("first opponent = %#v", firstPage.Items[0].Opponent)
	}
	if firstPage.Items[0].PlayerSeat != 1 || firstPage.Items[0].ActivePlayer != 2 || firstPage.Items[0].Scores.PlayerOne != 1 || firstPage.Items[0].Version != 1 {
		t.Fatalf("first summary = %#v", firstPage.Items[0])
	}
	secondPage := getActiveGames(t, handler, aliceCookie, "?limit=1&cursor="+url.QueryEscape(firstPage.NextCursor), http.StatusOK)
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID != secondID || secondPage.NextCursor != "" {
		t.Fatalf("second page = %#v", secondPage)
	}
	bobPage := getActiveGames(t, handler, bobCookie, "", http.StatusOK)
	if len(bobPage.Items) != 1 || bobPage.Items[0].ID != firstID || bobPage.Items[0].PlayerSeat != 2 {
		t.Fatalf("bob page = %#v", bobPage)
	}
	if page := getActiveGames(t, handler, danaCookie, "", http.StatusOK); len(page.Items) != 0 {
		t.Fatalf("completed game leaked into active list: %#v", page)
	}
	getActiveGames(t, handler, nil, "", http.StatusUnauthorized)
	getActiveGames(t, handler, aliceCookie, "?cursor=invalid", http.StatusBadRequest)
}

func TestPlayerSearchIsBoundedCaseInsensitiveAndPublic(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	_, aliceCookie := loginLobbyPlayer(t, handler, "Alice")
	albert, _ := loginLobbyPlayer(t, handler, "Albert")
	cal, _ := loginLobbyPlayer(t, handler, "Cal")
	loginLobbyPlayer(t, handler, "Zed")

	page, raw := searchLobbyPlayers(t, handler, aliceCookie, "?q=AL&limit=1", http.StatusOK)
	if len(page.Items) != 1 || page.Items[0].ID != albert.ID || page.Items[0].Type != "human" || page.NextCursor == "" {
		t.Fatalf("first search page = %#v", page)
	}
	if strings.Contains(raw, "normalized") || strings.Contains(raw, "session") || strings.Contains(raw, "createdAt") {
		t.Fatalf("search exposed private fields: %s", raw)
	}
	next, _ := searchLobbyPlayers(t, handler, aliceCookie, "?q=al&limit=1&cursor="+url.QueryEscape(page.NextCursor), http.StatusOK)
	if len(next.Items) != 1 || next.Items[0].ID != cal.ID {
		t.Fatalf("second search page = %#v", next)
	}
	self, _ := searchLobbyPlayers(t, handler, aliceCookie, "?q=alice", http.StatusOK)
	if len(self.Items) != 0 {
		t.Fatalf("current player appeared in search: %#v", self)
	}
	searchLobbyPlayers(t, handler, nil, "?q=al", http.StatusUnauthorized)
	searchLobbyPlayers(t, handler, aliceCookie, "?q=%21", http.StatusUnprocessableEntity)
}

func loginLobbyPlayer(t *testing.T, handler http.Handler, name string) (app.RegisteredPlayer, *http.Cookie) {
	t.Helper()
	response, player := enterPlayer(t, handler, name)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login %q cookies = %d", name, len(cookies))
	}
	return player, cookies[0]
}

func getActiveGames(t *testing.T, handler http.Handler, cookie *http.Cookie, query string, status int) app.Page[app.ActiveGameSummary] {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/games"+query, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if status != http.StatusOK {
		if status == http.StatusUnauthorized {
			assertAPIError(t, response, status, "player_session_required")
		} else {
			assertAPIError(t, response, status, "invalid_pagination")
		}
		return app.Page[app.ActiveGameSummary]{}
	}
	if response.Code != status || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("active games response = %d/%q", response.Code, response.Header().Get("Cache-Control"))
	}
	var envelope struct {
		Data app.Page[app.ActiveGameSummary] `json:"data"`
	}
	decodeResponse(t, response, &envelope)
	return envelope.Data
}

func searchLobbyPlayers(t *testing.T, handler http.Handler, cookie *http.Cookie, query string, status int) (app.Page[app.PublicPlayer], string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/players"+query, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	raw := response.Body.String()
	if status != http.StatusOK {
		response.Body = bytes.NewBufferString(raw)
		code := "invalid_player_search"
		if status == http.StatusUnauthorized {
			code = "player_session_required"
		}
		assertAPIError(t, response, status, code)
		return app.Page[app.PublicPlayer]{}, raw
	}
	if response.Code != status {
		t.Fatalf("search response = %d, want %d; body = %s", response.Code, status, raw)
	}
	var envelope struct {
		Data app.Page[app.PublicPlayer] `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	return envelope.Data, raw
}
