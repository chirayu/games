package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"letterpress/backend/internal/app"
)

const sessionLifetime = 180 * 24 * time.Hour

// Config controls security-sensitive HTTP behavior.
type Config struct {
	InsecureCookies bool
	AllowedOrigin   string
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
	mux.HandleFunc("POST /api/v1/games", handler.createGame)
	mux.HandleFunc("GET /api/v1/games/{gameID}", handler.getGame)
	mux.HandleFunc("POST /api/v1/games/{gameID}/sessions", handler.exchangeSession)
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

func (handler *handler) createGame(response http.ResponseWriter, request *http.Request) {
	if !handler.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	created, err := handler.service.CreateGame(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "could not create game")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusCreated, map[string]any{"data": map[string]string{
		"gameId":         created.ID,
		"playerOneToken": created.PlayerOneToken,
		"playerTwoToken": created.PlayerTwoToken,
	}})
}

func (handler *handler) exchangeSession(response http.ResponseWriter, request *http.Request) {
	if !handler.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Token == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "a valid token is required")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return
	}

	gameID := request.PathValue("gameID")
	session, err := handler.service.ExchangeSession(request.Context(), gameID, input.Token)
	if errors.Is(err, app.ErrInvalidSecret) {
		writeError(response, http.StatusUnauthorized, "invalid_invitation", "invitation is invalid")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "could not establish session")
		return
	}

	http.SetCookie(response, &http.Cookie{
		Name:     sessionCookieName(gameID),
		Value:    session.Token,
		Path:     gameAPIPath(gameID),
		Expires:  handler.config.Now().UTC().Add(sessionLifetime),
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   !handler.config.InsecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, map[string]any{"data": map[string]any{
		"gameId": session.GameID,
		"seat":   session.Seat,
	}})
}

func (handler *handler) getGame(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	gameID := request.PathValue("gameID")
	token := ""
	if cookie, err := request.Cookie(sessionCookieName(gameID)); err == nil {
		token = cookie.Value
	}
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

func sessionCookieName(gameID string) string {
	return "letterpress_session_" + gameID
}

func gameAPIPath(gameID string) string {
	return fmt.Sprintf("/api/v1/games/%s", url.PathEscape(gameID))
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{"error": errorBody{Code: code, Message: message}})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
