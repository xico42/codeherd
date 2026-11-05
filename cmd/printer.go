package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// printer writes command output and keeps the first write error, so a command
// can print its progress freely and report a failed write once, on return.
// Once a write fails, later writes are skipped. When a command fails for
// another reason, that error is the one it returns.
type printer struct {
	w   io.Writer
	err error
}

func newPrinter(w io.Writer) *printer {
	return &printer{w: w}
}

func (p *printer) Printf(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

func (p *printer) Println(a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintln(p.w, a...)
	}
}

// Err returns the first write error, if any.
func (p *printer) Err() error {
	if p.err != nil {
		return fmt.Errorf("writing output: %w", p.err)
	}
	return nil
}

// flushTable flushes a tabwriter whose rows were written through p.
func flushTable(t *tabwriter.Writer, p *printer) error {
	if err := p.Err(); err != nil {
		return err
	}
	if err := t.Flush(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}
	return nil
}
