package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// ANSI helpers.
const (
	bold   = "\033[1m"
	dim    = "\033[2m"
	reset  = "\033[0m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	cyan   = "\033[36m"
	clearLine = "\033[2K\r"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// isTTY returns true if stdout is an interactive terminal.
func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// spinner shows an animated spinner with a message.
// Call stop() to finish — it replaces the spinner with a final message.
type spinner struct {
	mu      sync.Mutex
	msg     string
	done    chan struct{}
	stopped bool
}

func startSpinner(msg string) *spinner {
	s := &spinner{msg: msg, done: make(chan struct{})}
	if !isTTY() {
		fmt.Printf("  %s...\n", msg)
		return s
	}
	go func() {
		i := 0
		for {
			select {
			case <-s.done:
				return
			default:
				s.mu.Lock()
				m := s.msg
				s.mu.Unlock()
				fmt.Printf("%s  %s%s%s %s", clearLine, cyan, spinFrames[i%len(spinFrames)], reset, m)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
	return s
}

// update changes the spinner message while it's running.
func (s *spinner) update(msg string) {
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// stop finishes the spinner with a success message.
func (s *spinner) stop(finalMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.done)
	if isTTY() {
		fmt.Printf("%s  %s✓%s %s\n", clearLine, green, reset, finalMsg)
	}
}

// fail finishes the spinner with an error message.
func (s *spinner) fail(finalMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.done)
	if isTTY() {
		fmt.Printf("%s  %s✗%s %s\n", clearLine, red, reset, finalMsg)
	} else {
		fmt.Printf("  FAILED: %s\n", finalMsg)
	}
}

// step prints a completed step.
func step(msg string) {
	fmt.Printf("  %s✓%s %s\n", green, reset, msg)
}

// header prints a section header.
func header(msg string) {
	fmt.Printf("\n%s%s%s\n", bold, msg, reset)
}

// kvPrint prints a key-value pair with aligned formatting.
func kvPrint(key, value string) {
	fmt.Printf("  %s%-10s%s %s\n", dim, key, reset, value)
}
