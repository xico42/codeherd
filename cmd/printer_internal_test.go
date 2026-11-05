package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"text/tabwriter"
)

func TestPrinter_writesOutput(t *testing.T) {
	var buf bytes.Buffer
	p := newPrinter(&buf)
	p.Printf("%s=%d ", "a", 1)
	p.Println("done")

	if got := buf.String(); got != "a=1 done\n" {
		t.Errorf("output = %q, want %q", got, "a=1 done\n")
	}
	if err := p.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

// countingFailWriter fails every write and counts the attempts.
type countingFailWriter struct{ calls int }

var errWriteFailed = errors.New("write failed")

func (w *countingFailWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, errWriteFailed
}

func TestPrinter_keepsFirstErrorAndStopsWriting(t *testing.T) {
	w := &countingFailWriter{}
	p := newPrinter(w)
	p.Printf("first")
	p.Println("second")
	p.Printf("third")

	if !errors.Is(p.Err(), errWriteFailed) {
		t.Errorf("Err() = %v, want it to wrap the write error", p.Err())
	}
	if w.calls != 1 {
		t.Errorf("writer called %d times, want 1 (writes after a failure are skipped)", w.calls)
	}
}

func TestFlushTable_alignsColumns(t *testing.T) {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	p := newPrinter(tw)
	p.Printf("a\tb\n")
	p.Printf("long\tc\n")

	if err := flushTable(tw, p); err != nil {
		t.Fatalf("flushTable() = %v, want nil", err)
	}
	if got := buf.String(); got != "a     b\nlong  c\n" {
		t.Errorf("output = %q, want aligned columns", got)
	}
}

func TestFlushTable_reportsFlushError(t *testing.T) {
	tw := tabwriter.NewWriter(&countingFailWriter{}, 0, 0, 2, ' ', 0)
	p := newPrinter(tw)
	p.Printf("a\tb\n") // buffered by the tabwriter, so this write succeeds

	err := flushTable(tw, p)
	if !errors.Is(err, errWriteFailed) || !strings.Contains(err.Error(), "flushing output") {
		t.Errorf("flushTable() = %v, want a wrapped flush error", err)
	}
}

func TestFlushTable_reportsEarlierWriteError(t *testing.T) {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	p := newPrinter(&countingFailWriter{})
	p.Printf("lost")

	err := flushTable(tw, p)
	if !errors.Is(err, errWriteFailed) || !strings.Contains(err.Error(), "writing output") {
		t.Errorf("flushTable() = %v, want the printer's write error", err)
	}
}
