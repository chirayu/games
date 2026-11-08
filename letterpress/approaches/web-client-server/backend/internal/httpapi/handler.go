package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"letterpress/backend/internal/app"
	"letterpress/backend/internal/game"
)

const sessionLifetime = 180 * 24 * time.Hour

const playerSessionCookieName = "letterpress_player_session"

// Config controls security-sensitive HTTP behavior.
type Config struct {
	InsecureCookies bool
	AllowedOrigin   string
	Frontend        fs.FS
	Now             func() time.Time
}

// NewHandler returns the HTTP API for the Letterpress service.
func NewHandler(service *app.Service, config Config) http.Handler {
	if config.Now == nil {
		config.Now = time.Now
	}
	handler := &handler{service: service, config: config}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", handler.ready)
	mux.HandleFunc("POST /api/v1/player-sessions", handler.enterPlayer)
	mux.HandleFunc("DELETE /api/v1/player-sessions/current", handler.logoutPlayer)
	mux.HandleFunc("GET /api/v1/players/me", handler.currentPlayer)
	mux.HandleFunc("GET /api/v1/players", handler.searchPlayers)
	mux.HandleFunc("GET /api/v1/games", handler.listActiveGames)
	mux.HandleFunc("GET /api/v1/games/{gameID}", handler.getGame)
	mux.HandleFunc("POST /api/v1/games/{gameID}/moves", handler.submitMove)
	mux.HandleFunc("POST /api/v1/game-allocations", handler.createAllocation)
	mux.HandleFunc("GET /api/v1/game-allocations", handler.listPendingChallenges)
	mux.HandleFunc("GET /api/v1/game-allocations/{allocationID}", handler.getAllocation)
	mux.HandleFunc("DELETE /api/v1/game-allocations/{allocationID}", handler.cancelAllocation)
	mux.HandleFunc("POST /api/v1/game-allocations/{allocationID}/accept", handler.acceptSpecifiedChallenge)
	mux.HandleFunc("POST /api/v1/game-allocations/{allocationID}/decline", handler.declineSpecifiedChallenge)
	if config.Frontend != nil {
		mux.HandleFunc("GET /", handler.frontend)
	}
	return mux
}

type handler struct {
	service *app.Service
	config  Config
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"data": map[string]string{"status": "ok"}})
}

func (handler *handler) ready(response http.ResponseWriter, request *http.Request) {
	if handler.service == nil || handler.service.Ready(request.Context()) != nil {
		writeError(response, http.StatusServiceUnavailable, "not_ready", "service is not ready")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": map[string]string{"status": "ready"}})
}

func (handler *handler) frontend(response http.ResponseWriter, request *http.Request) {
	requested := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if requested == "." || requested == "" {
		requested = "index.html"
	}
	if strings.HasPrefix(requested, "api/") || requested == "healthz" || requested == "readyz" {
		http.NotFound(response, request)
		return
	}
	content, err := fs.ReadFile(handler.config.Frontend, requested)
	if err != nil && !strings.Contains(path.Base(requested), ".") {
		requested = "index.html"
		content, err = fs.ReadFile(handler.config.Frontend, requested)
	}
	if err != nil {
		http.NotFound(response, request)
		return
	}
	if requested == "index.html" {
		response.Header().Set("Cache-Control", "no-cache")
	} else {
		response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(response, request, requested, time.Time{}, bytes.NewReader(content))
}

func (handler *handler) enterPlayer(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if !handler.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "a player name is required")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return
	}

	login, err := handler.service.EnterPlayer(request.Context(), input.Name)
	if errors.Is(err, app.ErrInvalidPlayerName) {
		writeError(response, http.StatusUnprocessableEntity, "invalid_player_name", "name must be 1–24 characters using letters, numbers, spaces, hyphens, or underscores")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "could not establish player session")
		return
	}
	handler.setPlayerSessionCookie(response, login.Token, login.ExpiresAt, int(sessionLifetime.Seconds()))
	status := http.StatusOK
	if login.Created {
		status = http.StatusCreated
	}
	writeJSON(response, status, map[string]any{"data": login.Player})
}

func (handler *handler) currentPlayer(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	player, err := handler.service.CurrentPlayer(request.Context(), handler.playerSessionToken(request))
	if errors.Is(err, app.ErrInvalidPlayerSession) {
		writeError(response, http.StatusUnauthorized, "player_session_required", "a valid player session is required")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "could not load player session")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": player})
}

func (handler *handler) logoutPlayer(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if !handler.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	err := handler.service.LogoutPlayer(request.Context(), handler.playerSessionToken(request))
	handler.setPlayerSessionCookie(response, "", time.Unix(1, 0).UTC(), -1)
	if errors.Is(err, app.ErrInvalidPlayerSession) {
		writeError(response, http.StatusUnauthorized, "player_session_required", "a valid player session is required")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "could not end player session")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *handler) playerSessionToken(request *http.Request) string {
	cookie, err := request.Cookie(playerSessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (handler *handler) setPlayerSessionCookie(
	response http.ResponseWriter,
	value string,
	expires time.Time,
	maxAge int,
) {
	http.SetCookie(response, &http.Cookie{
		Name: playerSessionCookieName, Value: value, Path: "/",
		Expires: expires, MaxAge: maxAge, HttpOnly: true,
		Secure: !handler.config.InsecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func (handler *handler) listActiveGames(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	limit, ok := pageLimit(request)
	if !ok {
		writeError(response, http.StatusBadRequest, "invalid_pagination", "limit must be between 1 and 50")
		return
	}
	page, err := handler.service.ListActiveGames(
		request.Context(), handler.playerSessionToken(request), request.URL.Query().Get("cursor"), limit,
	)
	switch {
	case errors.Is(err, app.ErrInvalidPlayerSession):
		writeError(response, http.StatusUnauthorized, "player_session_required", "a valid player session is required")
	case errors.Is(err, app.ErrInvalidPagination):
		writeError(response, http.StatusBadRequest, "invalid_pagination", "cursor or limit is invalid")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error", "could not list active games")
	default:
		writeJSON(response, http.StatusOK, map[string]any{"data": page})
	}
}

func (handler *handler) searchPlayers(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	limit, ok := pageLimit(request)
	if !ok {
		writeError(response, http.StatusBadRequest, "invalid_pagination", "limit must be between 1 and 50")
		return
	}
	page, err := handler.service.SearchPlayers(
		request.Context(), handler.playerSessionToken(request), request.URL.Query().Get("q"),
		request.URL.Query().Get("cursor"), limit,
	)
	switch {
	case errors.Is(err, app.ErrInvalidPlayerSession):
		writeError(response, http.StatusUnauthorized, "player_session_required", "a valid player session is required")
	case errors.Is(err, app.ErrInvalidPlayerSearch):
		writeError(response, http.StatusUnprocessableEntity, "invalid_player_search", "enter a valid player name fragment")
	case errors.Is(err, app.ErrInvalidPagination):
		writeError(response, http.StatusBadRequest, "invalid_pagination", "cursor or limit is invalid")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error", "could not search players")
	default:
		writeJSON(response, http.StatusOK, map[string]any{"data": page})
	}
}

func pageLimit(request *http.Request) (int, bool) {
	raw := request.URL.Query().Get("limit")
	if raw == "" {
		return 20, true
	}
	limit, err := strconv.Atoi(raw)
	return limit, err == nil && limit >= 1 && limit <= 50
}

func (handler *handler) createAllocation(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if !handler.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	var input struct {
		Mode                 string   `json:"mode"`
		OpponentID           string   `json:"opponentId"`
		AllowedOpponentTypes []string `json:"allowedOpponentTypes"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "mode and optional opponentId are required")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return
	}
	allocation, created, err := handler.service.CreateAllocation(
		request.Context(), handler.playerSessionToken(request), request.Header.Get("Idempotency-Key"),
		input.Mode, input.OpponentID, input.AllowedOpponentTypes,
	)
	if handler.writeAllocationError(response, err) {
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(response, status, map[string]any{"data": allocation})
}

func (handler *handler) getAllocation(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	allocation, err := handler.service.GetAllocation(
		request.Context(), handler.playerSessionToken(request), request.PathValue("allocationID"),
	)
	if handler.writeAllocationError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": allocation})
}

func (handler *handler) listPendingChallenges(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	items, err := handler.service.ListPendingChallenges(
		request.Context(), handler.playerSessionToken(request),
	)
	if handler.writeAllocationError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": map[string]any{"items": items}})
}

func (handler *handler) acceptSpecifiedChallenge(response http.ResponseWriter, request *http.Request) {
	handler.transitionSpecifiedChallenge(response, request, handler.service.AcceptSpecifiedChallenge)
}

func (handler *handler) declineSpecifiedChallenge(response http.ResponseWriter, request *http.Request) {
	handler.transitionSpecifiedChallenge(response, request, handler.service.DeclineSpecifiedChallenge)
}

func (handler *handler) transitionSpecifiedChallenge(
	response http.ResponseWriter,
	request *http.Request,
	transition func(context.Context, string, string) (app.GameAllocation, error),
) {
	response.Header().Set("Cache-Control", "no-store")
	if !handler.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	allocation, err := transition(
		request.Context(), handler.playerSessionToken(request), request.PathValue("allocationID"),
	)
	if handler.writeAllocationError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": allocation})
}

func (handler *handler) cancelAllocation(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if !handler.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	allocation, err := handler.service.CancelAllocation(
		request.Context(), handler.playerSessionToken(request), request.PathValue("allocationID"),
	)
	if handler.writeAllocationError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": allocation})
}

func (handler *handler) writeAllocationError(response http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, app.ErrInvalidPlayerSession):
		writeError(response, http.StatusUnauthorized, "player_session_required", "a valid player session is required")
	case errors.Is(err, app.ErrAllocationNotFound):
		writeError(response, http.StatusNotFound, "allocation_not_found", "allocation was not found")
	case errors.Is(err, app.ErrInvalidAllocation):
		writeError(response, http.StatusUnprocessableEntity, "invalid_allocation", "allocation mode, opponent, or idempotency key is invalid")
	case errors.Is(err, app.ErrIdempotencyConflict):
		writeError(response, http.StatusConflict, "idempotency_conflict", "idempotency key was already used for another allocation")
	case errors.Is(err, app.ErrInvalidAllocationState):
		writeError(response, http.StatusConflict, "invalid_allocation_state", "allocation cannot transition from its current state")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error", "allocation request failed")
	}
	return true
}

func (handler *handler) getGame(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	gameID := request.PathValue("gameID")
	token := handler.playerSessionToken(request)
	view, err := handler.service.GetGame(request.Context(), gameID, token)
	switch {
	case errors.Is(err, app.ErrGameNotFound):
		writeError(response, http.StatusNotFound, "game_not_found", "game was not found")
		return
	case errors.Is(err, app.ErrUnauthorized):
		writeError(response, http.StatusUnauthorized, "unauthorized", "a valid player session is required")
		return
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error", "could not load game")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": view})
}

func (handler *handler) submitMove(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if !handler.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	var input struct {
		TileIndexes     []int  `json:"tileIndexes"`
		ExpectedVersion *int64 `json:"expectedVersion"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.ExpectedVersion == nil || *input.ExpectedVersion < 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "tileIndexes and a non-negative expectedVersion are required")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return
	}
	gameID := request.PathValue("gameID")
	token := handler.playerSessionToken(request)
	view, err := handler.service.SubmitMove(request.Context(), gameID, token, input.TileIndexes, *input.ExpectedVersion)
	switch {
	case errors.Is(err, app.ErrGameNotFound):
		writeError(response, http.StatusNotFound, "game_not_found", "game was not found")
	case errors.Is(err, app.ErrUnauthorized):
		writeError(response, http.StatusUnauthorized, "unauthorized", "a valid player session is required")
	case errors.Is(err, app.ErrVersionConflict):
		writeError(response, http.StatusConflict, "version_conflict", "the board changed; refresh and try again")
	case errors.Is(err, game.ErrOutOfTurn):
		writeError(response, http.StatusConflict, "out_of_turn", "it is not your turn")
	case errors.Is(err, game.ErrGameCompleted):
		writeError(response, http.StatusConflict, "game_completed", "the game is already complete")
	case errors.Is(err, game.ErrDuplicateTile):
		writeError(response, http.StatusUnprocessableEntity, "duplicate_tile", "a tile can be selected only once")
	case errors.Is(err, game.ErrInvalidTile):
		writeError(response, http.StatusUnprocessableEntity, "invalid_tile", "a selected tile does not exist")
	case errors.Is(err, game.ErrInvalidWord):
		writeError(response, http.StatusUnprocessableEntity, "invalid_word", "select at least one tile")
	case errors.Is(err, game.ErrRepeatedWord):
		writeError(response, http.StatusUnprocessableEntity, "repeated_word", "that word has already been played")
	case errors.Is(err, game.ErrPrefixWord):
		writeError(response, http.StatusUnprocessableEntity, "prefix_word", "that word is a prefix of a previous word")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error", "could not submit move")
	default:
		writeJSON(response, http.StatusOK, map[string]any{"data": view})
	}
}

func (handler *handler) validOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" {
		return false
	}
	if handler.config.AllowedOrigin != "" {
		return strings.EqualFold(strings.TrimSuffix(origin, "/"), strings.TrimSuffix(handler.config.AllowedOrigin, "/"))
	}
	return strings.EqualFold(parsed.Host, request.Host) && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{"error": errorBody{Code: code, Message: message}})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
