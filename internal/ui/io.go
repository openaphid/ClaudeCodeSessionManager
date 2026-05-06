package ui

import (
	"io"
	"os"
)

// thin indirections so tests can stub later if needed.
func stdin() io.Reader  { return os.Stdin }
func stdout() io.Writer { return os.Stdout }
func stderr() io.Writer { return os.Stderr }
