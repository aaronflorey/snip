package verify

import (
	"testing"

	"github.com/aaronflorey/snip/internal/filter"
)

func TestRunTestsPassingFilter(t *testing.T) {
	filters := []filter.Filter{
		{
			Name: "test-filter",
			Pipeline: filter.Pipeline{
				{ActionName: "remove_lines", Params: map[string]any{"pattern": `^DEBUG`}},
			},
			Tests: []filter.FilterTest{
				{
					Name:     "removes debug lines",
					Input:    "DEBUG: foo\nINFO: bar\nDEBUG: baz\n",
					Expected: "INFO: bar\n",
				},
			},
		},
	}

	summary := RunTests(filters)

	if summary.TotalFilters != 1 {
		t.Errorf("TotalFilters: got %d, want 1", summary.TotalFilters)
	}
	if summary.TestedFilters != 1 {
		t.Errorf("TestedFilters: got %d, want 1", summary.TestedFilters)
	}
	if summary.TotalTests != 1 {
		t.Errorf("TotalTests: got %d, want 1", summary.TotalTests)
	}
	if summary.Passed != 1 {
		t.Errorf("Passed: got %d, want 1", summary.Passed)
	}
	if summary.Failed != 0 {
		t.Errorf("Failed: got %d, want 0", summary.Failed)
	}
}

func TestRunTestsFailingFilter(t *testing.T) {
	filters := []filter.Filter{
		{
			Name: "fail-filter",
			Pipeline: filter.Pipeline{
				{ActionName: "remove_lines", Params: map[string]any{"pattern": `^DEBUG`}},
			},
			Tests: []filter.FilterTest{
				{
					Name:     "wrong expectation",
					Input:    "DEBUG: foo\nINFO: bar\n",
					Expected: "DEBUG: foo\nINFO: bar\n",
				},
			},
		},
	}

	summary := RunTests(filters)

	if summary.Passed != 0 {
		t.Errorf("Passed: got %d, want 0", summary.Passed)
	}
	if summary.Failed != 1 {
		t.Errorf("Failed: got %d, want 1", summary.Failed)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("Results: got %d, want 1", len(summary.Results))
	}
	if summary.Results[0].Passed {
		t.Error("expected test to fail")
	}
}

func TestRunTestsFilterWithNoTests(t *testing.T) {
	filters := []filter.Filter{
		{
			Name: "no-tests",
			Pipeline: filter.Pipeline{
				{ActionName: "strip_ansi"},
			},
		},
	}

	summary := RunTests(filters)

	if summary.TotalFilters != 1 {
		t.Errorf("TotalFilters: got %d, want 1", summary.TotalFilters)
	}
	if summary.TestedFilters != 0 {
		t.Errorf("TestedFilters: got %d, want 0", summary.TestedFilters)
	}
	if summary.TotalTests != 0 {
		t.Errorf("TotalTests: got %d, want 0", summary.TotalTests)
	}
	if len(summary.UntestdFilters) != 1 {
		t.Errorf("UntestdFilters: got %d, want 1", len(summary.UntestdFilters))
	}
	if summary.UntestdFilters[0] != "no-tests" {
		t.Errorf("UntestdFilters[0]: got %q, want %q", summary.UntestdFilters[0], "no-tests")
	}
}

func TestRunTestsRequireAllBehavior(t *testing.T) {
	// RunTests itself does not enforce --require-all; it just reports.
	// The Run function checks UntestdFilters. We test the data here.
	filters := []filter.Filter{
		{
			Name: "tested",
			Pipeline: filter.Pipeline{
				{ActionName: "on_empty", Params: map[string]any{"message": "ok"}},
			},
			Tests: []filter.FilterTest{
				{Name: "empty returns ok", Input: "", Expected: "ok\n"},
			},
		},
		{
			Name:     "untested",
			Pipeline: filter.Pipeline{{ActionName: "strip_ansi"}},
		},
	}

	summary := RunTests(filters)

	if summary.TestedFilters != 1 {
		t.Errorf("TestedFilters: got %d, want 1", summary.TestedFilters)
	}
	if len(summary.UntestdFilters) != 1 {
		t.Errorf("UntestdFilters: got %d, want 1", len(summary.UntestdFilters))
	}
}

func TestRunTestsMultipleTestsPerFilter(t *testing.T) {
	filters := []filter.Filter{
		{
			Name: "multi",
			Pipeline: filter.Pipeline{
				{ActionName: "remove_lines", Params: map[string]any{"pattern": `^#`}},
				{ActionName: "on_empty", Params: map[string]any{"message": "ok"}},
			},
			Tests: []filter.FilterTest{
				{
					Name:     "removes comments",
					Input:    "# comment\ncode line\n",
					Expected: "code line\n",
				},
				{
					Name:     "all comments returns ok",
					Input:    "# comment\n",
					Expected: "ok\n",
				},
			},
		},
	}

	summary := RunTests(filters)

	if summary.TotalTests != 2 {
		t.Errorf("TotalTests: got %d, want 2", summary.TotalTests)
	}
	if summary.Passed != 2 {
		t.Errorf("Passed: got %d, want 2", summary.Passed)
	}
}

func TestApplyTestPipelineChained(t *testing.T) {
	f := &filter.Filter{
		Name: "test",
		Pipeline: filter.Pipeline{
			{ActionName: "remove_lines", Params: map[string]any{"pattern": `^$`}},
			{ActionName: "head", Params: map[string]any{"n": 2}},
		},
	}

	got, err := ApplyTestPipeline(f, "a\n\nb\n\nc\nd\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After remove_lines: a, b, c, d
	// After head(2): a, b, +2 more lines
	expected := "a\nb\n+2 more lines\n"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestApplyTestPipelineUnknownAction(t *testing.T) {
	f := &filter.Filter{
		Name: "test",
		Pipeline: filter.Pipeline{
			{ActionName: "nonexistent"},
		},
	}

	_, err := ApplyTestPipeline(f, "input")
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestRunTestsPipelineError(t *testing.T) {
	filters := []filter.Filter{
		{
			Name: "bad-pipeline",
			Pipeline: filter.Pipeline{
				{ActionName: "keep_lines", Params: map[string]any{}}, // missing pattern
			},
			Tests: []filter.FilterTest{
				{Name: "triggers error", Input: "hello\n", Expected: "hello\n"},
			},
		},
	}

	summary := RunTests(filters)

	if summary.Failed != 1 {
		t.Errorf("Failed: got %d, want 1", summary.Failed)
	}
	if summary.Results[0].Passed {
		t.Error("expected test to fail on pipeline error")
	}
	if summary.Results[0].Got == "" {
		t.Error("expected Got to contain error message")
	}
}
