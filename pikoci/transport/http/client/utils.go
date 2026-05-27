package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"

	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
)

var successCodeRe = regexp.MustCompile(`2\d\d`)

// Request sends an authenticated JSON HTTP request to the given URL.
// The body is JSON-encoded and the response is decoded into resp.
// If the server signals a stale token via X-Refresh-Token, a background
// token refresh is attempted.
func (c *Client) Request(ctx context.Context, method, url string, body, resp interface{}) error {
	buff := bytes.NewBuffer(nil)
	err := json.NewEncoder(buff).Encode(body)
	if err != nil {
		return fmt.Errorf("failed to Marshal body on %q: %w", url, err)
	}
	req, err := http.NewRequest(method, url, buff)
	if err != nil {
		return fmt.Errorf("failed to create request %q: %w", url, err)
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.jwt))

	// Fetch Request
	hresp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to do request %q: %w", url, err)
	}
	defer hresp.Body.Close()

	if !successCodeRe.MatchString(strconv.Itoa(hresp.StatusCode)) {
		var eresp thttp.ErrorResponse
		err = json.NewDecoder(hresp.Body).Decode(&eresp)
		if err != nil {
			return fmt.Errorf("failed to read all body on %q: %w", url, err)
		}
		return fmt.Errorf("response error on %q: %s", url, eresp.Err)
	} else if hresp.StatusCode != http.StatusNoContent {
		err = json.NewDecoder(hresp.Body).Decode(resp)
		if err != nil {
			return fmt.Errorf("failed to decode body on %q: %w", url, err)
		}
	}

	if hresp.Header.Get("X-Refresh-Token") == "true" {
		c.refreshToken(ctx)
	}

	return nil
}

func (c *Client) refreshToken(ctx context.Context) {
	var resp thttp.RefreshTokenResponse

	err := c.Request(ctx, http.MethodPost, fmt.Sprintf("%s/refresh-token", c.url), nil, &resp)
	if err != nil {
		return
	}

	if resp.Err != "" || resp.Data.JWT == "" {
		return
	}

	c.jwt = resp.Data.JWT

	if c.configPath != "" {
		_ = os.WriteFile(c.configPath, []byte(c.jwt), 0600)
	}
}
