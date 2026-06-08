# Functions

HCL functions are available in pipeline definitions for string manipulation, collection operations, encoding, and more.

## String functions

| Function      | Description                              | Example                                  |
|---------------|------------------------------------------|------------------------------------------|
| `chomp`       | Remove trailing newline                  | `chomp("hello\n")` -> `"hello"`          |
| `endswith`    | Check if string ends with suffix         | `endswith("hello", "lo")` -> `true`      |
| `format`      | Format a string (printf-style)           | `format("v%s-%s", "1.0", "prod")` -> `"v1.0-prod"` |
| `formatlist`  | Format each element in a list            | `formatlist("-%s", ["a","b"])` -> `["-a","-b"]` |
| `indent`      | Indent all lines of a string             | `indent(2, "a\nb")` -> `"a\n  b"`       |
| `join`        | Join list elements with separator        | `join(",", ["a","b","c"])` -> `"a,b,c"`  |
| `lower`       | Convert to lowercase                     | `lower("HELLO")` -> `"hello"`           |
| `replace`     | Replace substring                        | `replace("hello", "l", "r")` -> `"herro"` |
| `split`       | Split string into list                   | `split(",", "a,b,c")` -> `["a","b","c"]` |
| `startswith`  | Check if string starts with prefix       | `startswith("hello", "he")` -> `true`    |
| `strcontains` | Check if string contains substring       | `strcontains("hello", "ell")` -> `true`  |
| `strlen`      | Get string length in characters          | `strlen("hello")` -> `5`                |
| `strrev`      | Reverse a string                         | `strrev("hello")` -> `"olleh"`           |
| `substr`      | Extract substring                        | `substr("hello", 1, 3)` -> `"ell"`      |
| `title`       | Capitalize first letter of each word     | `title("hello world")` -> `"Hello World"` |
| `trim`        | Remove characters from both ends         | `trim("?!hello?!", "!?")` -> `"hello"`  |
| `trimprefix`  | Remove prefix                            | `trimprefix("helloworld", "hello")` -> `"world"` |
| `trimsuffix`  | Remove suffix                            | `trimsuffix("helloworld", "world")` -> `"hello"` |
| `trimspace`   | Remove leading/trailing whitespace       | `trimspace("  hello  ")` -> `"hello"`   |
| `upper`       | Convert to uppercase                     | `upper("hello")` -> `"HELLO"`           |

## Collection functions

| Function       | Description                              | Example                                  |
|----------------|------------------------------------------|------------------------------------------|
| `alltrue`      | True if all elements are true            | `alltrue([true, true])` -> `true`        |
| `anytrue`      | True if any element is true              | `anytrue([false, true])` -> `true`       |
| `chunklist`    | Split list into fixed-size chunks        | `chunklist(["a","b","c"], 2)` -> `[["a","b"],["c"]]` |
| `coalesce`     | First non-null, non-empty value          | `coalesce("", "b")` -> `"b"`            |
| `coalescelist` | First non-empty list                     | `coalescelist([], ["a"])` -> `["a"]`     |
| `compact`      | Remove empty strings from list           | `compact(["a","","b"])` -> `["a","b"]`   |
| `concat`       | Concatenate lists                        | `concat(["a"], ["b"])` -> `["a","b"]`    |
| `contains`     | Check if list/set contains value         | `contains(["a","b"], "a")` -> `true`     |
| `distinct`     | Remove duplicates from list              | `distinct(["a","a","b"])` -> `["a","b"]` |
| `element`      | Get element by index (wraps around)      | `element(["a","b","c"], 4)` -> `"b"`     |
| `flatten`      | Flatten nested lists                     | `flatten([["a"],["b"]])` -> `["a","b"]`  |
| `keys`         | Get map keys                             | `keys({a=1, b=2})` -> `["a","b"]`       |
| `length`       | Get length of list, map, or string       | `length(["a","b"])` -> `2`               |
| `lookup`       | Look up value in map with default        | `lookup({a="1"}, "a", "0")` -> `"1"`    |
| `merge`        | Merge maps                               | `merge({a=1}, {b=2})` -> `{a=1, b=2}`   |
| `one`          | Get the single element from a collection | `one(["only"])` -> `"only"`              |
| `range`        | Generate a sequence of numbers           | `range(3)` -> `[0, 1, 2]`               |
| `reverse`      | Reverse a list                           | `reverse(["a","b"])` -> `["b","a"]`      |
| `slice`        | Extract a sub-list                       | `slice(["a","b","c"], 1, 3)` -> `["b","c"]` |
| `sort`         | Sort a list of strings                   | `sort(["b","a"])` -> `["a","b"]`         |
| `sum`          | Sum a list of numbers                    | `sum([1, 2, 3])` -> `6`                 |
| `transpose`    | Swap keys and values in a map of lists   | `transpose({a=["1"], b=["1"]})` -> `{1=["a","b"]}` |
| `values`       | Get map values                           | `values({a=1, b=2})` -> `[1, 2]`        |
| `zipmap`       | Combine key list and value list into map | `zipmap(["a","b"], [1, 2])` -> `{a=1, b=2}` |

## Numeric functions

| Function   | Description                      | Example                    |
|------------|----------------------------------|----------------------------|
| `abs`      | Absolute value                   | `abs(-5)` -> `5`          |
| `ceil`     | Round up to nearest integer      | `ceil(1.2)` -> `2`        |
| `floor`    | Round down to nearest integer    | `floor(1.8)` -> `1`       |
| `log`      | Logarithm of number in given base| `log(100, 10)` -> `2`     |
| `max`      | Maximum of given numbers         | `max(1, 3, 2)` -> `3`     |
| `min`      | Minimum of given numbers         | `min(1, 3, 2)` -> `1`     |
| `parseint` | Parse string to int with base    | `parseint("ff", 16)` -> `255` |
| `pow`      | Raise to a power                 | `pow(2, 3)` -> `8`        |
| `signum`   | Sign of a number (-1, 0, or 1)  | `signum(-5)` -> `-1`      |

## Encoding functions

| Function       | Description                | Example                              |
|----------------|----------------------------|--------------------------------------|
| `base64decode` | Decode base64 string       | `base64decode("aGVsbG8=")` -> `"hello"` |
| `base64encode` | Encode string as base64    | `base64encode("hello")` -> `"aGVsbG8="` |
| `csvdecode`    | Decode CSV string          | `csvdecode("a,b\n1,2")` -> list of maps |
| `jsondecode`   | Decode JSON string         | `jsondecode("{\"a\":1}")` -> `{a=1}` |
| `jsonencode`   | Encode value as JSON       | `jsonencode({a=1})` -> `"{\"a\":1}"` |
| `urlencode`    | URL-encode a string        | `urlencode("a b")` -> `"a+b"`       |

## Date/Time functions

| Function     | Description                              | Example                                        |
|--------------|------------------------------------------|-------------------------------------------------|
| `formatdate` | Format a timestamp                       | `formatdate("YYYY-MM-DD", timestamp())` -> `"2026-06-08"` |
| `timeadd`    | Add duration to a timestamp              | `timeadd(timestamp(), "24h")` -> next day       |
| `timestamp`  | Current UTC time in RFC3339              | `timestamp()` -> `"2026-06-08T10:30:00Z"`       |

## Regex functions

| Function       | Description                         | Example                                   |
|----------------|-------------------------------------|-------------------------------------------|
| `regex`        | Match regex, return first match     | `regex("v(\\d+)", "v123")` -> `"123"`     |
| `regexall`     | Match regex, return all matches     | `regexall("\\d+", "a1b2")` -> `["1","2"]` |
| `regexreplace` | Replace regex matches               | `regexreplace("hello", "l+", "r")` -> `"hero"` |

## Set functions

| Function          | Description                              | Example                                       |
|-------------------|------------------------------------------|-----------------------------------------------|
| `toset`           | Convert list to set (deduplicated)       | `toset(["a","b","a"])` -> set of `["a","b"]`  |
| `setproduct`      | Cartesian product of sets/lists          | `setproduct(["a","b"], ["1","2"])` -> all pairs |
| `setintersection` | Elements common to all sets              | `setintersection(toset(["a","b"]), toset(["b","c"]))` -> `["b"]` |
| `setunion`        | All elements from all sets               | `setunion(toset(["a"]), toset(["b"]))` -> `["a","b"]` |
| `setsubtract`     | Elements in first set but not second     | `setsubtract(toset(["a","b"]), toset(["b"]))` -> `["a"]` |

## Type conversion functions

| Function   | Description                | Example                       |
|------------|----------------------------|-------------------------------|
| `tobool`   | Convert to boolean         | `tobool("true")` -> `true`   |
| `tolist`   | Convert to list            | `tolist(toset(["a"]))` -> `["a"]` |
| `tomap`    | Convert to map             | `tomap({a="1"})` -> map      |
| `tonumber` | Convert to number          | `tonumber("42")` -> `42`     |
| `tostring` | Convert to string          | `tostring(42)` -> `"42"`     |

## Practical examples

Building docker args:

```hcl
task "test" {
  run "docker" {
    image = "golang:1.23"
    cmd   = "make test"
    args  = concat(
      ["-e", "CI=true"],
      ["-v", "/cache:/cache"],
    )
  }
}
```

String formatting:

```hcl
variable "version" {
  type    = string
  default = "1.0"
}

variable "env" {
  type    = string
  default = "prod"
}

task "tag" {
  run "exec" {
    path = "echo"
    args = [format("v%s-%s", var.version, var.env)]
  }
}
```

Using for_each with set functions:

```hcl
job "test" {
  for_each = toset(["unit", "integration", "e2e"])
  task "run" {
    run "exec" {
      path = "/bin/sh"
      args = ["-c", "make test-${each.value}"]
    }
  }
}
```

Conditional logic with `coalesce`:

```hcl
variable "region" {
  type    = string
  default = ""
}

task "deploy" {
  run "exec" {
    path = "deploy.sh"
    args = ["--region", coalesce(var.region, "us-east-1")]
  }
}
```
