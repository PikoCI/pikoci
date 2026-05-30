// Package utils provides shared utility functions used across the PikoCI
// codebase, including canonical name generation, password hashing, and
// command definitions.
package utils

import (
	"strings"

	"github.com/gosimple/slug"
)

// Canonicalize converts a display name into a URL-safe slug suitable for use
// as a canonical identifier.
func Canonicalize(n string) string { return slug.Make(n) }

// ValidateCanonical reports whether c is a valid canonical identifier. A valid
// canonical is a well-formed slug no longer than 255 characters.
func ValidateCanonical(c string) bool {
	return slug.IsSlug(c) && len(c) <= 255
}

// ValidateResourceCanonical reports whether rc is a valid resource canonical.
// A resource canonical has the form "type.name", where both parts are valid
// canonicals.
func ValidateResourceCanonical(rc string) bool {
	rcs := strings.Split(rc, ".")
	if len(rcs) != 2 {
		return false
	}
	return ValidateCanonical(rcs[0]) && ValidateCanonical(rcs[1])
}

// ResourceCanonical builds a resource canonical from a resource type name (rt)
// and a resource name (rn), joining them with a dot separator.
func ResourceCanonical(rt, rn string) string { return strings.Join([]string{rt, rn}, ".") }

// NotificationCanonical builds a notification canonical from a notification type
// name (nt) and a notification name (nn), joining them with a dot separator.
func NotificationCanonical(nt, nn string) string { return strings.Join([]string{nt, nn}, ".") }
