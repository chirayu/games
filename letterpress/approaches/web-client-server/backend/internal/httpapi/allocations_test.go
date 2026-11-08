package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"letterpress/backend/internal/app"
	"letterpress/backend/internal/game"
	"letterpress/backend/internal/store/sqlite"
)

func TestAllocationAPIIsIdempotentPrivateAndCancellable(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	service := handler.(*testHTTPHandler).service
	alice, aliceCookie := loginLobbyPlayer(t, handler, "Alice")
	bob, bobCookie := loginLobbyPlayer(t, handler, "Bob")
	_, eveCookie := loginLobbyPlayer(t, handler, "Eve")

	created := createAllocationRequest(t, handler, aliceCookie, "challenge-1", "specified", bob.ID, http.StatusCreated)
	if created.RequesterID != alice.ID || created.OpponentID != bob.ID || created.State != "pending_acceptance" || created.Mode != "specified" {
		t.Fatalf("created allocation = %#v", created)
	}
	retried := createAllocationRequest(t, handler, aliceCookie, "challenge-1", "specified", bob.ID, http.StatusOK)
	if retried.ID != created.ID {
		t.Fatalf("retry allocation ID = %q, want %q", retried.ID, created.ID)
	}
	createAllocationRequest(t, handler, aliceCookie, "challenge-1", "random", "", http.StatusConflict)

	if got := getAllocationRequest(t, handler, bobCookie, created.ID, http.StatusOK); got.ID != created.ID {
		t.Fatalf("opponent allocation = %#v", got)
	}
	getAllocationRequest(t, handler, eveCookie, created.ID, http.StatusNotFound)
	deleteAllocationRequest(t, handler, bobCookie, created.ID, http.StatusNotFound)
	cancelled := deleteAllocationRequest(t, handler, aliceCookie, created.ID, http.StatusOK)
	if cancelled.State != "cancelled" {
		t.Fatalf("cancelled state = %q", cancelled.State)
	}
	deleteAllocationRequest(t, handler, aliceCookie, created.ID, http.StatusConflict)
	if _, err := service.CompleteAllocation(context.Background(), created.ID, bob.ID); !errors.Is(err, app.ErrInvalidAllocationState) {
		t.Fatalf("complete cancelled allocation error = %v, want invalid state", err)
	}

	random := createAllocationRequest(t, handler, aliceCookie, "random-1", "random", "", http.StatusCreated)
	bot := createAllocationRequest(t, handler, aliceCookie, "bot-1", "bot", "", http.StatusCreated)
	if random.State != "queued" || bot.State != "queued" {
		t.Fatalf("reserved allocation states = %q/%q", random.State, bot.State)
	}
	createAllocationRequest(t, handler, nil, "anonymous", "random", "", http.StatusUnauthorized)
}

func TestConcurrentAllocationCompletionCreatesOneAccessibleGame(t *testing.T) {
	handler, repository, _ := newIntegrationHandler(t, true)
	service := handler.(*testHTTPHandler).service
	alice, aliceCookie := loginLobbyPlayer(t, handler, "Alice")
	bob, bobCookie := loginLobbyPlayer(t, handler, "Bob")
	allocation := createAllocationRequest(t, handler, aliceCookie, "complete-1", "specified", bob.ID, http.StatusCreated)

	start := make(chan struct{})
	results := make(chan app.GameAllocation, 2)
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := service.CompleteAllocation(context.Background(), allocation.ID, bob.ID)
			results <- result
			errors <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent completion: %v", err)
		}
	}
	gameID := ""
	for result := range results {
		if result.State != "allocated" || result.GameID == "" {
			t.Fatalf("completion result = %#v", result)
		}
		if gameID == "" {
			gameID = result.GameID
		} else if result.GameID != gameID {
			t.Fatalf("completion game IDs = %q and %q", gameID, result.GameID)
		}
	}
	seats, err := repository.ListGameSeats(context.Background(), gameID)
	if err != nil || len(seats) != 2 || seats[0].PlayerID != alice.ID || seats[1].PlayerID != bob.ID {
		t.Fatalf("allocated seats = %#v, err %v", seats, err)
	}
	if view := loadGame(t, handler, gameID, aliceCookie); view.PlayerSeat != 1 {
		t.Fatalf("requester seat = %d, want 1", view.PlayerSeat)
	}
	if view := loadGame(t, handler, gameID, bobCookie); view.PlayerSeat != 2 {
		t.Fatalf("opponent seat = %d, want 2", view.PlayerSeat)
	}
	if again, err := service.CompleteAllocation(context.Background(), allocation.ID, bob.ID); err != nil || again.GameID != gameID {
		t.Fatalf("idempotent completion = %#v, err %v", again, err)
	}
}

func TestSpecifiedChallengeActionsAndPendingLists(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	alice, aliceCookie := loginLobbyPlayer(t, handler, "Alice")
	bob, bobCookie := loginLobbyPlayer(t, handler, "Bob")
	_, eveCookie := loginLobbyPlayer(t, handler, "Eve")

	challenge := createAllocationRequest(t, handler, aliceCookie, "accept-me", "specified", bob.ID, http.StatusCreated)
	assertPendingChallenge(t, handler, aliceCookie, challenge.ID, "outgoing")
	assertPendingChallenge(t, handler, bobCookie, challenge.ID, "incoming")
	actionAllocationRequest(t, handler, eveCookie, challenge.ID, "accept", http.StatusNotFound)
	actionAllocationRequest(t, handler, aliceCookie, challenge.ID, "accept", http.StatusNotFound)
	accepted := actionAllocationRequest(t, handler, bobCookie, challenge.ID, "accept", http.StatusOK)
	if accepted.State != "allocated" || accepted.GameID == "" || accepted.RequesterID != alice.ID {
		t.Fatalf("accepted allocation = %#v", accepted)
	}
	actionAllocationRequest(t, handler, bobCookie, challenge.ID, "decline", http.StatusConflict)

	declined := createAllocationRequest(t, handler, aliceCookie, "decline-me", "specified", bob.ID, http.StatusCreated)
	got := actionAllocationRequest(t, handler, bobCookie, declined.ID, "decline", http.StatusOK)
	if got.State != "declined" {
		t.Fatalf("declined allocation = %#v", got)
	}
	assertNoPendingChallenge(t, handler, aliceCookie, declined.ID)
}

func TestSpecifiedChallengeExpiresAfterSevenDays(t *testing.T) {
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "letterpress.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	now := time.Date(2026, time.November, 1, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	service := app.NewServiceWithDependenciesAndClock(repository, game.NewCryptoBoardGenerator(), game.MVPWordValidator{}, clock)
	handler := newTestHTTPHandler(service, Config{InsecureCookies: true, AllowedOrigin: testOrigin, Now: clock})
	_, aliceCookie := loginLobbyPlayer(t, handler, "Alice")
	bob, bobCookie := loginLobbyPlayer(t, handler, "Bob")
	challenge := createAllocationRequest(t, handler, aliceCookie, "expires", "specified", bob.ID, http.StatusCreated)
	if challenge.ExpiresAt == nil || !challenge.ExpiresAt.Equal(now.Add(7*24*time.Hour)) {
		t.Fatalf("challenge expiry = %v", challenge.ExpiresAt)
	}

	now = now.Add(7*24*time.Hour + time.Second)
	expired := getAllocationRequest(t, handler, bobCookie, challenge.ID, http.StatusOK)
	if expired.State != "expired" {
		t.Fatalf("allocation state = %q, want expired", expired.State)
	}
	actionAllocationRequest(t, handler, bobCookie, challenge.ID, "accept", http.StatusConflict)
	deleteAllocationRequest(t, handler, aliceCookie, challenge.ID, http.StatusConflict)
}

func TestSpecifiedChallengeAcceptCancelRaceHasOneWinner(t *testing.T) {
	handler, _, _ := newIntegrationHandler(t, true)
	_, aliceCookie := loginLobbyPlayer(t, handler, "Alice")
	bob, bobCookie := loginLobbyPlayer(t, handler, "Bob")
	challenge := createAllocationRequest(t, handler, aliceCookie, "race", "specified", bob.ID, http.StatusCreated)

	start := make(chan struct{})
	statuses := make(chan int, 2)
	var workers sync.WaitGroup
	requests := []struct {
		method string
		path   string
		cookie *http.Cookie
	}{
		{http.MethodPost, "/api/v1/game-allocations/" + challenge.ID + "/accept", bobCookie},
		{http.MethodDelete, "/api/v1/game-allocations/" + challenge.ID, aliceCookie},
	}
	for _, candidate := range requests {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			request := newMutationRequest(candidate.method, candidate.path, nil)
			request.AddCookie(candidate.cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			statuses <- response.Code
		}()
	}
	close(start)
	workers.Wait()
	close(statuses)
	successes := 0
	conflicts := 0
	for status := range statuses {
		switch status {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("race status = %d", status)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("race successes/conflicts = %d/%d, want 1/1", successes, conflicts)
	}
}

func actionAllocationRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, id, action string, status int) app.GameAllocation {
	t.Helper()
	request := newMutationRequest(http.MethodPost, "/api/v1/game-allocations/"+id+"/"+action, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if status != http.StatusOK {
		code := "allocation_not_found"
		if status == http.StatusConflict {
			code = "invalid_allocation_state"
		}
		assertAPIError(t, response, status, code)
		return app.GameAllocation{}
	}
	if response.Code != status {
		t.Fatalf("%s allocation status = %d, want %d", action, response.Code, status)
	}
	return decodeAllocation(t, response)
}

func pendingChallenges(t *testing.T, handler http.Handler, cookie *http.Cookie) []app.ChallengeSummary {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/game-allocations", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list challenges status = %d; body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Items []app.ChallengeSummary `json:"items"`
		} `json:"data"`
	}
	decodeResponse(t, response, &envelope)
	return envelope.Data.Items
}

func assertPendingChallenge(t *testing.T, handler http.Handler, cookie *http.Cookie, id, direction string) {
	t.Helper()
	for _, challenge := range pendingChallenges(t, handler, cookie) {
		if challenge.AllocationID == id && challenge.Direction == direction {
			return
		}
	}
	t.Fatalf("pending challenge %q with direction %q not found", id, direction)
}

func assertNoPendingChallenge(t *testing.T, handler http.Handler, cookie *http.Cookie, id string) {
	t.Helper()
	for _, challenge := range pendingChallenges(t, handler, cookie) {
		if challenge.AllocationID == id {
			t.Fatalf("terminal challenge %q remains pending", id)
		}
	}
}

func createAllocationRequest(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	key, mode, opponentID string,
	status int,
) app.GameAllocation {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"mode": mode, "opponentId": opponentID})
	request := newMutationRequest(http.MethodPost, "/api/v1/game-allocations", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if status != http.StatusOK && status != http.StatusCreated {
		code := "idempotency_conflict"
		if status == http.StatusUnauthorized {
			code = "player_session_required"
		}
		if status == http.StatusUnprocessableEntity {
			code = "invalid_allocation"
		}
		assertAPIError(t, response, status, code)
		return app.GameAllocation{}
	}
	if response.Code != status {
		t.Fatalf("create allocation status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
	return decodeAllocation(t, response)
}

func getAllocationRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, id string, status int) app.GameAllocation {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/game-allocations/"+id, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if status != http.StatusOK {
		assertAPIError(t, response, status, "allocation_not_found")
		return app.GameAllocation{}
	}
	if response.Code != status {
		t.Fatalf("get allocation status = %d, want %d", response.Code, status)
	}
	return decodeAllocation(t, response)
}

func deleteAllocationRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, id string, status int) app.GameAllocation {
	t.Helper()
	request := newMutationRequest(http.MethodDelete, "/api/v1/game-allocations/"+id, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if status != http.StatusOK {
		code := "allocation_not_found"
		if status == http.StatusConflict {
			code = "invalid_allocation_state"
		}
		assertAPIError(t, response, status, code)
		return app.GameAllocation{}
	}
	return decodeAllocation(t, response)
}

func decodeAllocation(t *testing.T, response *httptest.ResponseRecorder) app.GameAllocation {
	t.Helper()
	var envelope struct {
		Data app.GameAllocation `json:"data"`
	}
	decodeResponse(t, response, &envelope)
	return envelope.Data
}
