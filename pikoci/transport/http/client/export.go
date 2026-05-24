package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// ExportDatabase downloads the database export from the server and writes it to outputPath.
func (c *Client) ExportDatabase(ctx context.Context, outputPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/admin/export", c.url), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.jwt))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("export failed (status %d): %s", resp.StatusCode, string(body))
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	return nil
}
