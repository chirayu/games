package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"letterpress/backend/internal/app"
)

var ErrVersionConflict = errors.New("bot API version conflict")

// APIError is a safe decoded failure from the shared game API.
type APIError struct {
	Status int
	Code   string
}

func (failure *APIError) Error() string {
	return fmt.Sprintf("bot API request failed with status %d and code %q", failure.Status, failure.Code)
}

// Client is an ordinary authenticated HTTP client for the shared game routes.
type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

func NewClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("bot API base URL must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("bot API base URL must be HTTP(S)")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("bot runtime token is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: parsed, token: token, httpClient: httpClient}, nil
}

func (client *Client) GetGame(ctx context.Context, gameID string) (app.GameView, error) {
	return client.request(ctx, http.MethodGet, gameID, false, nil)
}

func (client *Client) SubmitMove(ctx context.Context, gameID string, tileIndexes []int, expectedVersion int64) (app.GameView, error) {
	body := struct {
		TileIndexes     []int `json:"tileIndexes"`
		ExpectedVersion int64 `json:"expectedVersion"`
	}{TileIndexes: tileIndexes, ExpectedVersion: expectedVersion}
	return client.request(ctx, http.MethodPost, gameID, true, body)
}

func (client *Client) request(ctx context.Context, method, gameID string, move bool, body any) (app.GameView, error) {
	endpoint := client.baseURL.JoinPath("api", "v1", "games", gameID)
	if move {
		endpoint = endpoint.JoinPath("moves")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return app.GameView{}, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return app.GameView{}, err
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return app.GameView{}, err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 1<<20)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		var envelope struct {
			Data app.GameView `json:"data"`
		}
		if err := json.NewDecoder(limited).Decode(&envelope); err != nil {
			return app.GameView{}, fmt.Errorf("decode bot API game: %w", err)
		}
		return envelope.Data, nil
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(limited).Decode(&envelope)
	if response.StatusCode == http.StatusConflict && envelope.Error.Code == "version_conflict" {
		return app.GameView{}, ErrVersionConflict
	}
	return app.GameView{}, &APIError{Status: response.StatusCode, Code: envelope.Error.Code}
}
