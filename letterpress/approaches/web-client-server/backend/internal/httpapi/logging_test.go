package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLoggingOmitsCapabilitiesAndQueryParameters(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := LogRequests(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		_, _ = response.Write([]byte("ok"))
	}), logger)
	request := httptest.NewRequest(http.MethodPost, "https://play.example/api/v1/games/game-1/moves?token=query-secret", nil)
	request.Header.Set("Cookie", "letterpress_session_game-1=cookie-secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	logged := output.String()
	for _, required := range []string{`"method":"POST"`, `"path":"/api/v1/games/game-1/moves"`, `"status":201`, `"bytes":2`} {
		if !strings.Contains(logged, required) {
			t.Fatalf("log %q does not contain %q", logged, required)
		}
	}
	for _, secret := range []string{"query-secret", "cookie-secret", "token="} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log exposed %q: %s", secret, logged)
		}
	}
}
