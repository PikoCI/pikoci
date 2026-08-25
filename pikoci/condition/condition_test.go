package condition

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allVars contains a realistic set of all runtime variable types that
// conditions can reference. It mirrors what runIfStep builds from
// exportedVars + buildMetadataParams + resolved secret vars.
var allVars = map[string]string{
	// GET_<STEP>_<KEY> — from get step version metadata (version_* params)
	"GET_APP_BRANCH": "main",
	"GET_APP_REF":    "abc123def456",
	"GET_APP_TAG":    "v2.1.0",

	// TASK_<STEP>_<KEY> — from task PIKOCI_OUTPUT exports
	"TASK_BUILD_VERSION":    "2.1.0",
	"TASK_BUILD_EXIT_CODE":  "0",
	"TASK_DETECT_ENV_ENV":   "staging",
	"TASK_BUILD_TESTS_PASS": "true",

	// BUILD_* — from buildMetadataParams
	"BUILD_NUMBER":        "42",
	"BUILD_JOB_NAME":      "deploy",
	"BUILD_PIPELINE_NAME": "my-pipeline",
	"BUILD_TEAM_NAME":     "platform",

	// INPUT_<name> — from job input parameters (b.InputValues)
	"INPUT_env":     "staging",
	"INPUT_region":  "us-east-1",
	"INPUT_dry_run": "false",
	"INPUT_count":   "3",
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		want      bool
		wantErr   bool
	}{
		// ── Empty / trivial ──
		{"empty condition is true", "", true, false},

		// ── == (string equality) ──
		// GET_* vars (resource version metadata)
		{"eq GET branch match", "$GET_APP_BRANCH == 'main'", true, false},
		{"eq GET branch no match", "$GET_APP_BRANCH == 'develop'", false, false},
		{"eq GET ref", "$GET_APP_REF == 'abc123def456'", true, false},
		{"eq GET tag semver", "$GET_APP_TAG == 'v2.1.0'", true, false},
		// TASK_* vars (PIKOCI_OUTPUT exports)
		{"eq TASK version", "$TASK_BUILD_VERSION == '2.1.0'", true, false},
		{"eq TASK exit code", "$TASK_BUILD_EXIT_CODE == '0'", true, false},
		{"eq TASK env", "$TASK_DETECT_ENV_ENV == 'staging'", true, false},
		{"eq TASK boolean-like", "$TASK_BUILD_TESTS_PASS == 'true'", true, false},
		// BUILD_* vars (build metadata)
		{"eq BUILD number", "$BUILD_NUMBER == '42'", true, false},
		{"eq BUILD job name", "$BUILD_JOB_NAME == 'deploy'", true, false},
		{"eq BUILD pipeline", "$BUILD_PIPELINE_NAME == 'my-pipeline'", true, false},
		{"eq BUILD team", "$BUILD_TEAM_NAME == 'platform'", true, false},
		// input_* vars (manual trigger inputs)
		{"eq input env", "$INPUT_env == 'staging'", true, false},
		{"eq input region", "$INPUT_region == 'us-east-1'", true, false},
		{"eq input bool-like", "$INPUT_dry_run == 'false'", true, false},
		{"eq input numeric", "$INPUT_count == '3'", true, false},
		// Undefined var expands to empty
		{"eq undefined var empty", "$UNDEFINED == ''", true, false},
		{"eq undefined var nonempty", "$UNDEFINED == 'something'", false, false},

		// ── != (not equal) ──
		{"neq GET match", "$GET_APP_BRANCH != 'develop'", true, false},
		{"neq GET no match", "$GET_APP_BRANCH != 'main'", false, false},
		{"neq TASK", "$TASK_BUILD_EXIT_CODE != '1'", true, false},
		{"neq BUILD", "$BUILD_NUMBER != '99'", true, false},
		{"neq input", "$INPUT_env != 'production'", true, false},
		{"neq undefined vs nonempty", "$UNDEFINED != 'something'", true, false},
		{"neq undefined vs empty", "$UNDEFINED != ''", false, false},

		// ── > (greater than, numeric if parseable) ──
		{"gt numeric TASK", "$TASK_BUILD_EXIT_CODE > '-1'", true, false},
		{"gt numeric TASK false", "$TASK_BUILD_EXIT_CODE > '1'", false, false},
		{"gt numeric BUILD", "$BUILD_NUMBER > '41'", true, false},
		{"gt numeric BUILD false", "$BUILD_NUMBER > '42'", false, false},
		{"gt numeric input", "$INPUT_count > '2'", true, false},
		{"gt numeric input false", "$INPUT_count > '5'", false, false},
		{"gt string fallback GET", "$GET_APP_BRANCH > 'aaa'", true, false},
		{"gt string fallback GET false", "$GET_APP_BRANCH > 'zzz'", false, false},

		// ── < (less than, numeric if parseable) ──
		{"lt numeric BUILD", "$BUILD_NUMBER < '100'", true, false},
		{"lt numeric BUILD false", "$BUILD_NUMBER < '10'", false, false},
		{"lt numeric input", "$INPUT_count < '10'", true, false},
		{"lt numeric input false", "$INPUT_count < '1'", false, false},
		{"lt string fallback GET", "$GET_APP_BRANCH < 'zzz'", true, false},
		{"lt string fallback GET false", "$GET_APP_BRANCH < 'aaa'", false, false},
		{"lt numeric equal", "$BUILD_NUMBER < '42'", false, false},
		{"gt numeric equal", "$BUILD_NUMBER > '42'", false, false},

		// ── contains (substring match) ──
		{"contains GET branch prefix", "$GET_APP_BRANCH contains 'mai'", true, false},
		{"contains GET branch full", "$GET_APP_BRANCH contains 'main'", true, false},
		{"contains GET ref partial", "$GET_APP_REF contains 'abc123'", true, false},
		{"contains GET no match", "$GET_APP_BRANCH contains 'dev'", false, false},
		{"contains TASK version", "$TASK_BUILD_VERSION contains '2.1'", true, false},
		{"contains BUILD pipeline", "$BUILD_PIPELINE_NAME contains 'pipeline'", true, false},
		{"contains input region", "$INPUT_region contains 'east'", true, false},
		{"contains empty in anything", "$GET_APP_BRANCH contains ''", true, false},
		// When $UNDEFINED expands to empty, "contains ''" becomes a bare word
		// "contains" followed by trailing text — this is an error, not a match.
		// Users should quote or compare the empty var differently.
		{"contains undefined empty", "$UNDEFINED contains ''", true, false},

		// ── !contains (negated substring) ──
		{"!contains GET no match", "$GET_APP_BRANCH !contains 'dev'", true, false},
		{"!contains GET match", "$GET_APP_BRANCH !contains 'mai'", false, false},
		{"!contains TASK", "$TASK_DETECT_ENV_ENV !contains 'prod'", true, false},
		{"!contains BUILD", "$BUILD_TEAM_NAME !contains 'backend'", true, false},
		{"!contains input", "$INPUT_region !contains 'west'", true, false},

		// ── && (logical AND) ──
		{"and both true mixed vars", "$GET_APP_BRANCH == 'main' && $INPUT_env == 'staging'", true, false},
		{"and left false", "$GET_APP_BRANCH == 'develop' && $INPUT_env == 'staging'", false, false},
		{"and right false", "$GET_APP_BRANCH == 'main' && $INPUT_env == 'production'", false, false},
		{"and both false", "$GET_APP_BRANCH == 'develop' && $INPUT_env == 'production'", false, false},
		{"and three terms", "$GET_APP_BRANCH == 'main' && $BUILD_NUMBER == '42' && $INPUT_env == 'staging'", true, false},
		{"and three terms one false", "$GET_APP_BRANCH == 'main' && $BUILD_NUMBER == '99' && $INPUT_env == 'staging'", false, false},
		{"and with numeric", "$BUILD_NUMBER > '10' && $INPUT_count < '100'", true, false},

		// ── || (logical OR) ──
		{"or both false", "$GET_APP_BRANCH == 'develop' || $INPUT_env == 'production'", false, false},
		{"or left true", "$GET_APP_BRANCH == 'main' || $INPUT_env == 'production'", true, false},
		{"or right true", "$GET_APP_BRANCH == 'develop' || $INPUT_env == 'staging'", true, false},
		{"or both true", "$GET_APP_BRANCH == 'main' || $INPUT_env == 'staging'", true, false},
		{"or three terms", "$GET_APP_BRANCH == 'develop' || $BUILD_NUMBER == '99' || $INPUT_env == 'staging'", true, false},
		{"or three terms all false", "$GET_APP_BRANCH == 'develop' || $BUILD_NUMBER == '99' || $INPUT_env == 'production'", false, false},

		// ── Parentheses / precedence ──
		{"parens override precedence", "($GET_APP_BRANCH == 'main' || $GET_APP_BRANCH == 'develop') && $TASK_BUILD_EXIT_CODE == '0'", true, false},
		{"parens with false inner", "($GET_APP_BRANCH == 'develop' || $GET_APP_BRANCH == 'feature') && $TASK_BUILD_EXIT_CODE == '0'", false, false},
		{"nested parens", "(($BUILD_NUMBER > '10') && ($INPUT_count < '100'))", true, false},
		{"or with and precedence no parens", "$GET_APP_BRANCH == 'develop' || $GET_APP_BRANCH == 'main' && $INPUT_env == 'staging'", true, false},

		// ── No-space operators ──
		{"no space ==", "$GET_APP_BRANCH=='main'", true, false},
		{"no space !=", "$GET_APP_BRANCH!='develop'", true, false},
		{"no space >", "$BUILD_NUMBER>'10'", true, false},
		{"no space <", "$BUILD_NUMBER<'100'", true, false},

		// ── Bare words (unquoted values) ──
		{"bare word ==", "hello == hello", true, false},
		{"bare word != ", "hello == world", false, false},
		{"bare word contains", "hello-world contains hello", true, false},

		// ── Boolean-like (value-as-truthy) ──
		{"non-empty var is truthy", "$GET_APP_BRANCH", true, false},
		{"empty/undefined var is falsy", "$UNDEFINED", false, false},
		{"non-empty TASK var truthy", "$TASK_BUILD_VERSION", true, false},
		{"non-empty BUILD var truthy", "$BUILD_NUMBER", true, false},
		{"non-empty input var truthy", "$INPUT_env", true, false},

		// ── Real-world condition patterns ──
		{"deploy to prod pattern", "$GET_APP_BRANCH == 'main' && $TASK_BUILD_EXIT_CODE == '0' && $INPUT_dry_run == 'false'", true, false},
		{"deploy to staging pattern", "$TASK_DETECT_ENV_ENV == 'staging' && $INPUT_region contains 'east'", true, false},
		{"feature branch skip", "$GET_APP_BRANCH != 'main' && $GET_APP_BRANCH != 'develop'", false, false},
		{"build number threshold", "$BUILD_NUMBER > '10' && $BUILD_NUMBER < '100'", true, false},
		{"multi-env check", "$INPUT_env == 'production' || $INPUT_env == 'staging'", true, false},
		{"semver tag check", "$GET_APP_TAG contains 'v2.' && $TASK_BUILD_TESTS_PASS == 'true'", true, false},

		// ── Two vars compared against each other ──
		{"var vs var eq match", "$GET_APP_BRANCH == $GET_APP_BRANCH", true, false},
		{"var vs var eq no match", "$GET_APP_BRANCH == $TASK_DETECT_ENV_ENV", false, false},
		{"var vs var neq", "$GET_APP_BRANCH != $TASK_DETECT_ENV_ENV", true, false},
		{"var vs var contains", "$BUILD_PIPELINE_NAME contains $INPUT_env", false, false},

		// ── ${VAR} brace syntax (supported by os.Expand) ──
		{"brace syntax eq", "${GET_APP_BRANCH} == 'main'", true, false},
		{"brace syntax neq", "${BUILD_NUMBER} != '99'", true, false},
		{"brace syntax in expression", "${GET_APP_BRANCH} == 'main' && ${INPUT_env} == 'staging'", true, false},

		// ── Numeric edge cases ──
		{"gt negative number", "$TASK_BUILD_EXIT_CODE > '-1'", true, false},
		{"lt negative number", "$TASK_BUILD_EXIT_CODE < '-1'", false, false},
		{"gt float", "$BUILD_NUMBER > '41.5'", true, false},
		{"lt float", "$BUILD_NUMBER < '42.5'", true, false},
		{"gt leading zero", "$INPUT_count > '003'", false, false}, // 3 > 3 = false
		{"eq leading zero", "$INPUT_count == '003'", false, false}, // "3" != "003" string comparison
		{"gt mixed numeric string left", "$GET_APP_BRANCH > '10'", true, false}, // string fallback: "main" > "10"
		{"lt mixed numeric string right", "$BUILD_NUMBER < 'abc'", true, false},  // string fallback: "42" < "abc"
		{"gt equal numeric", "$BUILD_NUMBER > '42'", false, false},
		{"lt equal numeric", "$BUILD_NUMBER < '42'", false, false},
		{"gt equal string", "$GET_APP_BRANCH > 'main'", false, false},
		{"lt equal string", "$GET_APP_BRANCH < 'main'", false, false},

		// ── contains / !contains no-space ──
		{"contains no space quoted", "$GET_APP_BRANCH contains'mai'", true, false},
		{"!contains no space quoted", "$GET_APP_BRANCH !contains'dev'", true, false},

		// ── Empty string comparisons ──
		{"empty eq empty", "'' == ''", true, false},
		{"empty neq empty", "'' != ''", false, false},
		{"empty contains empty", "'' contains ''", true, false},
		{"empty !contains empty", "'' !contains ''", false, false},
		{"empty lt nonempty", "'' < 'a'", true, false},
		{"nonempty gt empty", "'a' > ''", true, false},

		// ── Values with operator-like content ──
		{"value containing equals", "'a==b' == 'a==b'", true, false},
		{"value containing ampersand", "'a&&b' == 'a&&b'", true, false},
		{"value containing pipe", "'a||b' == 'a||b'", true, false},
		{"value containing contains", "'containsX' == 'containsX'", true, false},
		{"value containing gt", "'a>b' == 'a>b'", true, false},

		// ── && / || mixed precedence (AND binds tighter than OR) ──
		// false && true || true  →  (false && true) || true  →  false || true  →  true
		{"and-or precedence left false", "$GET_APP_BRANCH == 'develop' && $BUILD_NUMBER == '42' || $INPUT_env == 'staging'", true, false},
		// true || false && false  →  true || (false && false)  →  true || false  →  true
		{"and-or precedence right false", "$GET_APP_BRANCH == 'main' || $BUILD_NUMBER == '99' && $INPUT_env == 'production'", true, false},
		// false || false && true  →  false || (false && true)  →  false || false  →  false
		{"and-or precedence all paths false", "$GET_APP_BRANCH == 'develop' || $BUILD_NUMBER == '99' && $INPUT_env == 'staging'", false, false},

		// ── Whitespace edge cases ──
		{"whitespace only", "   ", true, false},
		{"extra whitespace around operators", "  $GET_APP_BRANCH   ==   'main'  ", true, false},
		{"tabs and spaces", "\t$BUILD_NUMBER\t==\t'42'\t", true, false},

		// ── Error cases ──
		{"unterminated quote", "$GET_APP_BRANCH == 'main", false, true},
		{"missing closing paren", "($GET_APP_BRANCH == 'main'", false, true},
		{"trailing text", "$GET_APP_BRANCH == 'main' extratext", false, true},
		{"extra closing paren", "$GET_APP_BRANCH == 'main')", false, true},
		{"empty parens", "()", false, false}, // parses inner as empty → "" → falsy
		{"contains undefined empty", "$UNDEFINED contains ''", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.condition, allVars)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got, "condition: %s", tt.condition)
		})
	}
}

func TestEvaluate_NilVars(t *testing.T) {
	// Nil vars map should not panic; all vars expand to ""
	result, err := Evaluate("$FOO == ''", nil)
	require.NoError(t, err)
	assert.True(t, result)
}

func TestEvaluate_EmptyVars(t *testing.T) {
	result, err := Evaluate("$FOO == ''", map[string]string{})
	require.NoError(t, err)
	assert.True(t, result)
}

func TestEvaluate_SpecialCharValues(t *testing.T) {
	vars := map[string]string{
		"GET_APP_BRANCH":     "feature/my-branch",
		"TASK_BUILD_VERSION": "1.2.3-rc.1",
		"INPUT_path":         "/opt/deploy/app",
	}

	tests := []struct {
		name      string
		condition string
		want      bool
	}{
		{"slash in branch", "$GET_APP_BRANCH contains 'feature/'", true},
		{"dot in version", "$TASK_BUILD_VERSION contains '1.2.3'", true},
		{"dash in version", "$TASK_BUILD_VERSION contains 'rc'", true},
		{"slash in path", "$INPUT_path contains '/deploy/'", true},
		{"eq with slash", "$GET_APP_BRANCH == 'feature/my-branch'", true},
		{"eq with dots and dash", "$TASK_BUILD_VERSION == '1.2.3-rc.1'", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.condition, vars)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEvaluate_ValuesWithSpaces(t *testing.T) {
	// When a variable expands to a value with spaces, the bare word parser
	// reads only until the first space. This means variables containing
	// spaces produce errors when used unquoted — users should avoid spaces
	// in PIKOCI_OUTPUT keys and resource metadata values.
	vars := map[string]string{
		"TASK_BUILD_MSG": "hello world",
		"INPUT_label":    "deploy to prod",
	}

	tests := []struct {
		name      string
		condition string
		want      bool
		wantErr   bool
	}{
		// A variable is expanded after the expression is tokenized, so spaces
		// in its value are part of the value and nothing else.
		{"space value eq", "$TASK_BUILD_MSG == 'hello world'", true, false},
		{"space value contains", "$TASK_BUILD_MSG contains 'hello'", true, false},
		{"space input eq", "$INPUT_label == 'deploy to prod'", true, false},
		// Quoted literal comparison still works fine
		{"space value eq both quoted", "'hello world' == 'hello world'", true, false},
		{"space value contains both quoted", "'hello world' contains 'hello'", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.condition, vars)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got, "condition: %s", tt.condition)
		})
	}
}

func TestEvaluate_SingleQuotesInValues(t *testing.T) {
	// A quote inside a variable value is data, not syntax, because the value
	// is substituted after the expression has been parsed.
	vars := map[string]string{
		"TASK_BUILD_MSG": "it's done",
	}

	result, err := Evaluate("$TASK_BUILD_MSG contains 'done'", vars)
	require.NoError(t, err)
	assert.True(t, result)
}

func TestEvaluate_ValueCannotInjectSyntax(t *testing.T) {
	// A value that looks like expression syntax must be compared as data,
	// otherwise a job input could rewrite the condition guarding a branch.
	vars := map[string]string{
		"INPUT_env": "x' || '1' == '1",
		"GET_TAG":   "v1 == v1",
	}

	result, err := Evaluate("$INPUT_env == 'prod'", vars)
	require.NoError(t, err)
	assert.False(t, result)

	result, err = Evaluate("$GET_TAG == 'prod'", vars)
	require.NoError(t, err)
	assert.False(t, result)
}

func TestEvaluate_FalseIsNotTruthy(t *testing.T) {
	vars := map[string]string{
		"FLAG_OFF": "false",
		"ZERO":     "0",
		"FLAG_ON":  "true",
		"WORD":     "yes",
	}

	for condition, want := range map[string]bool{
		"$FLAG_OFF":  false,
		"$ZERO":      false,
		"$UNDEFINED": false,
		"$FLAG_ON":   true,
		"$WORD":      true,
	} {
		got, err := Evaluate(condition, vars)
		require.NoError(t, err, condition)
		assert.Equal(t, want, got, "condition: %s", condition)
	}
}

func TestEvaluate_MalformedIsAnError(t *testing.T) {
	vars := map[string]string{"S": "hello", "FLAG": "false"}

	for _, condition := range []string{
		"$S contains",
		"'a' == 'a' &&",
		"$S ==",
		"!$FLAG",
		"$S >= 5",
	} {
		_, err := Evaluate(condition, vars)
		require.Error(t, err, "condition %q should be rejected", condition)
	}
}
