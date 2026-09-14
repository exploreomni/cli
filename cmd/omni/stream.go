package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/exploreomni/omni-cli/internal/auth"
	"github.com/exploreomni/omni-cli/internal/config"
	"github.com/exploreomni/omni-cli/internal/output"
	"github.com/exploreomni/omni-cli/internal/result"
)

// maxWaitPolls bounds re-waits; each query/wait call blocks server-side.
const maxWaitPolls = 30

func isQueryStream(resp *http.Response) bool {
	return resp.StatusCode < 400 && strings.HasPrefix(resp.Header.Get("Content-Type"), "text/ndjson")
}

// renderStream renders a query stream as a table or chart, waiting on any
// job still running. JSON output never comes here.
func renderStream(cfg *config.ResolvedConfig, resp *http.Response, format string, compact bool, chart *output.ChartOptions, stdout, stderr io.Writer) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	st, err := result.Parse(data)
	if err != nil {
		return err
	}

	for polls := 0; len(st.Remaining) > 0; polls++ {
		if polls >= maxWaitPolls {
			return fmt.Errorf("query still running after %d waits (jobs %s); try again with `omni query wait --job-ids %s`",
				polls, strings.Join(st.Remaining, ","), strings.Join(st.Remaining, ","))
		}
		more, err := waitForJobs(cfg, format, compact, stderr, st.Remaining)
		if err != nil {
			return err
		}
		st.Sets = append(st.Sets, more.Sets...)
		st.Failures = append(st.Failures, more.Failures...)
		st.Remaining = more.Remaining
	}

	// A job that failed doesn't void the ones that didn't: render what
	// completed, then report the failure and exit non-zero. With nothing
	// rendered, stdout stays empty and only the failure is reported.
	failed := st.Err()
	if len(st.Sets) == 0 && failed != nil {
		return failed
	}

	// Render everything before writing anything, so a failure on a later
	// set leaves stdout empty rather than half a result.
	var out bytes.Buffer
	if len(st.Sets) == 0 {
		fmt.Fprintln(&out, "No results.")
	}
	for i, set := range st.Sets {
		if i > 0 {
			fmt.Fprintln(&out)
		}
		if chart != nil {
			if err := output.ResultChart(&out, set, *chart); err != nil {
				return err
			}
		} else {
			output.ResultTable(&out, set)
		}
	}
	if u := resp.Header.Get("X-Omni-Workbook-Url"); u != "" {
		output.ChartLink(&out, u)
	}
	if _, err := stdout.Write(out.Bytes()); err != nil {
		return err
	}
	return failed
}

func waitForJobs(cfg *config.ResolvedConfig, format string, compact bool, stderr io.Writer, ids []string) (*result.Stream, error) {
	sp := maybeStartSpinner(format)
	resp, err := auth.Do(cfg, http.MethodGet, "/api/v1/query/wait?jobIds="+url.QueryEscape(strings.Join(ids, ",")), nil)
	sp.Stop()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading query/wait response: %w", err)
	}
	if resp.StatusCode >= 400 {
		body := jsonBody(data)
		detail := extractErrorDetail(body, data, resp.StatusCode)
		writeError(stderr, format, resp.StatusCode, detail, body, compact)
		return nil, &apiError{status: resp.StatusCode}
	}
	return result.Parse(data)
}
