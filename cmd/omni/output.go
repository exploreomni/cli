package main

import (
	"fmt"
	"net/http"

	"github.com/exploreomni/omni-cli/internal/output"
)

func outputResponse(resp *http.Response, compact bool) error {
	if resp.StatusCode >= 400 {
		// Read error body and print to stderr
		err := output.JSON(resp.Body, compact)
		if err != nil {
			output.Error(resp.StatusCode, fmt.Sprintf("HTTP %d", resp.StatusCode))
		}
		return fmt.Errorf("API returned HTTP %d", resp.StatusCode)
	}

	// 204 No Content
	if resp.StatusCode == 204 {
		fmt.Println("{}")
		return nil
	}

	return output.JSON(resp.Body, compact)
}
