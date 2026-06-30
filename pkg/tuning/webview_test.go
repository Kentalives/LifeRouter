package tuning

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Kentalives/LifeRouter/internal/grid"
)

func TestOpenVisualizerWritesHTMLAndCallsBrowser(t *testing.T) {
	var openedPaths []string

	browserOpener = func(path string) error {
		openedPaths = append(openedPaths, path)

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected visualizer file to be readable: %v", err)
		}

		html := string(content)
		for _, want := range []string{
			"Cost Grid Visualizer",
			"gridCanvas",
			`"costs":[1,2,3,4,5,6]`,
			`"rows":2`,
			`"cols":3`,
		} {
			if !strings.Contains(html, want) {
				t.Fatalf("expected visualizer HTML to contain %q", want)
			}
		}

		return nil
	}
	t.Cleanup(func() {
		browserOpener = openBrowser
		for _, path := range openedPaths {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("removing temporary visualizer file: %v", err)
			}
		}
	})

	view := RasterizedGrid{
		Rows:  2,
		Cols:  3,
		Cells: []grid.Cost{1, 2, 3, 4, 5, 6},
	}
	if err := view.OpenVisualizer(); err != nil {
		t.Fatalf("OpenVisualizer returned error: %v", err)
	}
	if len(openedPaths) != 1 {
		t.Fatalf("expected one browser open call, got %d", len(openedPaths))
	}
}

func TestOpenVisualizerRejectsInvalidGridBeforeOpeningBrowser(t *testing.T) {
	tests := []struct {
		name string
		grid RasterizedGrid
	}{
		{
			name: "zero rows",
			grid: RasterizedGrid{Rows: 0, Cols: 2, Cells: []grid.Cost{1, 2}},
		},
		{
			name: "zero cols",
			grid: RasterizedGrid{Rows: 2, Cols: 0, Cells: []grid.Cost{1, 2}},
		},
		{
			name: "cell count mismatch",
			grid: RasterizedGrid{Rows: 2, Cols: 2, Cells: []grid.Cost{1, 2, 3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			browserOpener = func(path string) error {
				called = true
				return nil
			}
			t.Cleanup(func() {
				browserOpener = openBrowser
			})

			if err := tt.grid.OpenVisualizer(); err == nil {
				t.Fatal("expected OpenVisualizer to return an error")
			}
			if called {
				t.Fatal("expected invalid grid to fail before opening browser")
			}
		})
	}
}

func TestOpenVisualizerReturnsBrowserError(t *testing.T) {
	wantErr := errors.New("browser failed")
	var openedPath string

	browserOpener = func(path string) error {
		openedPath = path
		return wantErr
	}
	t.Cleanup(func() {
		browserOpener = openBrowser
		if openedPath != "" {
			if err := os.Remove(openedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("removing temporary visualizer file: %v", err)
			}
		}
	})

	view := RasterizedGrid{
		Rows:  1,
		Cols:  1,
		Cells: []grid.Cost{1},
	}
	if err := view.OpenVisualizer(); !errors.Is(err, wantErr) {
		t.Fatalf("expected browser error %q, got %v", wantErr, err)
	}
	if openedPath == "" {
		t.Fatal("expected OpenVisualizer to call browser opener")
	}
}
