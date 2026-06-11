package wkr

import (
	"fmt"
	"slices"

	"github.com/pikoci/pikoci/pikoci/utils"
)

const MaxTags = 10

// TagsMatch reports whether a worker with the given tags and exclusivity setting
// can run work that requires jobTags. Untagged work (empty jobTags) is accepted
// by any non-exclusive worker. Tagged work requires the worker to have ALL of
// the job's tags.
func TagsMatch(jobTags, workerTags []string, exclusiveTags bool) bool {
	if len(jobTags) == 0 {
		return !exclusiveTags
	}
	for _, jt := range jobTags {
		if !slices.Contains(workerTags, jt) {
			return false
		}
	}
	return true
}

// ValidateTags checks that each tag is a valid canonical and that there are
// at most MaxTags tags.
func ValidateTags(tags []string) error {
	if len(tags) > MaxTags {
		return fmt.Errorf("too many tags (%d), maximum is %d", len(tags), MaxTags)
	}
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		if !utils.ValidateCanonical(t) {
			return fmt.Errorf("invalid tag %q: must be a lowercase slug (letters, digits, hyphens)", t)
		}
		if _, ok := seen[t]; ok {
			return fmt.Errorf("duplicate tag %q", t)
		}
		seen[t] = struct{}{}
	}
	return nil
}
