package main

import (
	"math/rand"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/exploreomni/omni-cli/internal/config"
	"golang.org/x/term"
)

// spinnerPhrases are the rotating suffix messages shown next to the spinner
// while the CLI waits on the Omni API. Short, playful, BI-flavored.
var spinnerPhrases = []string{
	"consulting the warehouse…",
	"waking up the query planner…",
	"aligning facts and dimensions…",
	"joining tables in the back…",
	"asking the semantic layer nicely…",
	"compiling models…",
	"measuring twice, querying once…",
	"herding rows…",
	"squinting at the query plan…",
	"reticulating splines…",
	"summoning rows…",
	"negotiating with the warehouse…",
	"convincing BigQuery to hurry…",
	"teaching the LLM OmniSQL…",
	"crunching numbers…",
}

type spinnerHandle struct {
	s    *spinner.Spinner
	stop chan struct{}
}

// Stop halts the animation and the phrase-rotation goroutine. Safe to call
// on a nil receiver so callers can write `defer h.Stop()` unconditionally.
func (h *spinnerHandle) Stop() {
	if h == nil {
		return
	}
	close(h.stop)
	h.s.Stop()
}

// maybeStartSpinner returns a running spinner, or nil if the environment
// doesn't warrant one (non-TTY stderr, or non-human output format).
func maybeStartSpinner(format string) *spinnerHandle {
	if format != config.FormatHuman {
		return nil
	}
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return nil
	}
	s := spinner.New(spinner.CharSets[14], 80*time.Millisecond, spinner.WithWriter(os.Stderr))
	s.Suffix = " " + pickPhrase()
	s.Start()

	stop := make(chan struct{})
	go rotatePhrases(s, stop)
	return &spinnerHandle{s: s, stop: stop}
}

func rotatePhrases(s *spinner.Spinner, stop <-chan struct{}) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.Suffix = " " + pickPhrase()
		}
	}
}

func pickPhrase() string {
	return spinnerPhrases[rand.Intn(len(spinnerPhrases))]
}
