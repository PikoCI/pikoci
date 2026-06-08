package pikoci_test

import (
	"context"
	"testing"

	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestHCLFunctions_AllAvailable verifies that all documented HCL functions
// can be referenced without error in pipeline expressions.
func TestHCLFunctions_AllAvailable(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" { check_interval = "@every 1m" }

job "test" {
  get "cron" "tick" { trigger = true }
  task "string-funcs" {
    run "exec" {
      path = "/bin/echo"
      args = [
        chomp("hello\n"),
        endswith("hello", "lo") ? "yes" : "no",
        format("v%s", "1"),
        join(",", formatlist("-%s", ["a","b"])),
        indent(2, "a"),
        join(",", ["a","b"]),
        lower("HELLO"),
        replace("hello", "l", "r"),
        join(",", split(",", "a,b")),
        startswith("hello", "he") ? "yes" : "no",
        strcontains("hello", "ell") ? "yes" : "no",
        tostring(strlen("hello")),
        strrev("hello"),
        substr("hello", 0, 3),
        title("hello world"),
        trim("!!hello!!", "!"),
        trimprefix("hello", "he"),
        trimsuffix("hello", "lo"),
        trimspace("  hi  "),
        upper("hello"),
      ]
    }
  }
  task "collection-funcs" {
    run "exec" {
      path = "/bin/echo"
      args = [
        alltrue([true, true]) ? "yes" : "no",
        anytrue([false, true]) ? "yes" : "no",
        join(",", chunklist(["a","b","c"], 2)[0]),
        coalesce("", "b"),
        join(",", coalescelist([], ["a"])),
        join(",", compact(["a","","b"])),
        join(",", concat(["a"], ["b"])),
        contains(["a","b"], "a") ? "yes" : "no",
        join(",", distinct(["a","a","b"])),
        element(["a","b","c"], 1),
        join(",", flatten([["a"],["b"]])),
        tostring(length(["a","b"])),
        lookup({a="1", b="2"}, "a", "0"),
        join(",", keys(merge({a="1"}, {b="2"}))),
        one(["only"]),
        join(",", [for i in range(3) : tostring(i)]),
        join(",", reverse(["a","b"])),
        join(",", slice(["a","b","c"], 1, 3)),
        join(",", sort(["b","a"])),
        tostring(sum([1, 2, 3])),
        join(",", values({a="1", b="2"})),
        lookup(zipmap(["a","b"], ["1","2"]), "a", ""),
      ]
    }
  }
  task "numeric-funcs" {
    run "exec" {
      path = "/bin/echo"
      args = [
        tostring(abs(-5)),
        tostring(ceil(1.2)),
        tostring(floor(1.8)),
        tostring(log(100, 10)),
        tostring(max(1, 3, 2)),
        tostring(min(1, 3, 2)),
        tostring(parseint("ff", 16)),
        tostring(pow(2, 3)),
        tostring(signum(-5)),
      ]
    }
  }
  task "encoding-funcs" {
    run "exec" {
      path = "/bin/echo"
      args = [
        base64encode("hello"),
        base64decode("aGVsbG8="),
        jsonencode({a="1"}),
        lookup(jsondecode("{\"a\":\"1\"}"), "a", ""),
        urlencode("hello world"),
      ]
    }
  }
  task "datetime-funcs" {
    run "exec" {
      path = "/bin/echo"
      args = [
        timestamp(),
        formatdate("YYYY", timestamp()),
      ]
    }
  }
  task "regex-funcs" {
    run "exec" {
      path = "/bin/echo"
      args = [
        regex("[a-z]+", "hello123"),
        join(",", regexall("[0-9]+", "a1b2c3")),
        regexreplace("hello", "l+", "r"),
      ]
    }
  }
  task "set-funcs" {
    run "exec" {
      path = "/bin/echo"
      args = [
        tostring(length(toset(["a","b","a"]))),
        tostring(length(setintersection(toset(["a","b"]), toset(["b","c"])))),
        tostring(length(setunion(toset(["a"]), toset(["b"])))),
        tostring(length(setsubtract(toset(["a","b"]), toset(["b"])))),
        tostring(length(setsymmetricdifference(toset(["a","b"]), toset(["b","c"])))),
      ]
    }
  }
  task "type-funcs" {
    run "exec" {
      path = "/bin/echo"
      args = [
        tostring(42),
        tostring(tonumber("42")),
        tobool("true") ? "yes" : "no",
      ]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "p", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "p").Return(&pipeline.Pipeline{ID: 1}, nil)
	s.Jobs.EXPECT().Create(ctx, "main", "p", gomock.Any()).Return(uint32(1), nil)

	_, err := s.S.CreatePipeline(ctx, "main", "p", hclConfig, nil)
	require.NoError(t, err, "all HCL functions should evaluate without error")
}

// TestHCLFunctions_CustomValues tests the actual return values of all custom
// function implementations by capturing the job.Job from the mock and
// inspecting the task args.
func TestHCLFunctions_CustomValues(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		// String checks
		{"startswith true", `startswith("hello", "he") ? "yes" : "no"`, "yes"},
		{"startswith false", `startswith("hello", "world") ? "yes" : "no"`, "no"},
		{"endswith true", `endswith("hello", "lo") ? "yes" : "no"`, "yes"},
		{"endswith false", `endswith("hello", "he") ? "yes" : "no"`, "no"},
		{"strcontains true", `strcontains("hello world", "lo wo") ? "yes" : "no"`, "yes"},
		{"strcontains false", `strcontains("hello", "xyz") ? "yes" : "no"`, "no"},

		// Encoding
		{"base64encode", `base64encode("hello")`, "aGVsbG8="},
		{"base64decode", `base64decode("aGVsbG8=")`, "hello"},
		{"base64 roundtrip", `base64decode(base64encode("round trip"))`, "round trip"},
		{"urlencode", `urlencode("a b&c=d")`, "a+b%26c%3Dd"},

		// Collection predicates
		{"alltrue all", `alltrue([true, true, true]) ? "yes" : "no"`, "yes"},
		{"alltrue mixed", `alltrue([true, false]) ? "yes" : "no"`, "no"},
		{"alltrue empty", `alltrue([]) ? "yes" : "no"`, "yes"},
		{"anytrue found", `anytrue([false, true]) ? "yes" : "no"`, "yes"},
		{"anytrue none", `anytrue([false, false]) ? "yes" : "no"`, "no"},
		{"anytrue empty", `anytrue([]) ? "yes" : "no"`, "no"},

		// one
		{"one single", `one(["only"])`, "only"},

		// sum
		{"sum integers", `tostring(sum([10, 20, 30]))`, "60"},
		{"sum empty", `tostring(sum([]))`, "0"},

		// transpose
		{"transpose keys", `join(",", sort(keys(transpose({a=["1","2"], b=["1"]}))))`, "1,2"},

		// Type conversions
		{"tostring number", `tostring(42)`, "42"},
		{"tonumber string", `tostring(tonumber("99"))`, "99"},
		{"tobool true", `tobool("true") ? "yes" : "no"`, "yes"},
		{"tobool false", `tobool("false") ? "yes" : "no"`, "no"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			s := newService(ctrl)
			ctx := context.TODO()

			hclConfig := []byte(`
resource "cron" "tick" { check_interval = "@every 1m" }
job "test" {
  get "cron" "tick" { trigger = true }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = [` + tt.expr + `]
    }
  }
}
`)

			s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
			s.Resources.EXPECT().Create(ctx, "main", "p", gomock.Any()).Return(uint32(1), nil)
			s.Pipelines.EXPECT().Find(ctx, "main", "p").Return(&pipeline.Pipeline{ID: 1}, nil)

			var captured job.Job
			s.Jobs.EXPECT().Create(ctx, "main", "p", gomock.Any()).DoAndReturn(
				func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
					captured = j
					return uint32(1), nil
				}).Times(1)

			_, err := s.S.CreatePipeline(ctx, "main", "p", hclConfig, nil)
			require.NoError(t, err)

			require.Len(t, captured.Plan, 2, "expected get + task steps")
			task := captured.Plan[1].Task
			require.NotNil(t, task, "second step should be a task")
			require.Len(t, task.Run.Args, 1, "task should have 1 arg")
			assert.Equal(t, tt.want, task.Run.Args[0])
		})
	}
}

// TestHCLFunctions_SetOperationsValues tests set operation results.
func TestHCLFunctions_SetOperationsValues(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" { check_interval = "@every 1m" }
job "test" {
  get "cron" "tick" { trigger = true }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = [
        tostring(length(setintersection(toset(["a","b","c"]), toset(["b","c","d"])))),
        tostring(length(setunion(toset(["a","b"]), toset(["c","d"])))),
        tostring(length(setsubtract(toset(["a","b","c"]), toset(["b"])))),
        tostring(length(setsymmetricdifference(toset(["a","b"]), toset(["b","c"])))),
      ]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "p", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "p").Return(&pipeline.Pipeline{ID: 1}, nil)

	var captured job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "p", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			captured = j
			return uint32(1), nil
		}).Times(1)

	_, err := s.S.CreatePipeline(ctx, "main", "p", hclConfig, nil)
	require.NoError(t, err)

	args := captured.Plan[1].Task.Run.Args
	require.Len(t, args, 4)
	assert.Equal(t, "2", args[0], "setintersection {a,b,c} & {b,c,d} = {b,c}")
	assert.Equal(t, "4", args[1], "setunion {a,b} | {c,d} = {a,b,c,d}")
	assert.Equal(t, "2", args[2], "setsubtract {a,b,c} - {b} = {a,c}")
	assert.Equal(t, "2", args[3], "setsymmetricdifference {a,b} ^ {b,c} = {a,c}")
}

// TestHCLFunctions_NumericValues tests numeric function results.
func TestHCLFunctions_NumericValues(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" { check_interval = "@every 1m" }
job "test" {
  get "cron" "tick" { trigger = true }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = [
        tostring(parseint("ff", 16)),
        tostring(pow(2, 10)),
        tostring(signum(-42)),
        tostring(signum(0)),
        tostring(signum(99)),
        tostring(abs(-99)),
        tostring(log(100, 10)),
      ]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "p", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "p").Return(&pipeline.Pipeline{ID: 1}, nil)

	var captured job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "p", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			captured = j
			return uint32(1), nil
		}).Times(1)

	_, err := s.S.CreatePipeline(ctx, "main", "p", hclConfig, nil)
	require.NoError(t, err)

	args := captured.Plan[1].Task.Run.Args
	require.Len(t, args, 7)
	assert.Equal(t, "255", args[0], "parseint ff base 16")
	assert.Equal(t, "1024", args[1], "pow 2^10")
	assert.Equal(t, "-1", args[2], "signum -42")
	assert.Equal(t, "0", args[3], "signum 0")
	assert.Equal(t, "1", args[4], "signum 99")
	assert.Equal(t, "99", args[5], "abs -99")
	assert.Equal(t, "2", args[6], "log(100, 10) = 2")
}
