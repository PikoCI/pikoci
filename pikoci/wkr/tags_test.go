package wkr

import "testing"

func TestTagsMatch(t *testing.T) {
	tests := []struct {
		name          string
		jobTags       []string
		workerTags    []string
		exclusiveTags bool
		want          bool
	}{
		{"untagged job, untagged worker", nil, nil, false, true},
		{"untagged job, tagged worker", nil, []string{"gpu"}, false, true},
		{"untagged job, exclusive worker", nil, []string{"gpu"}, true, false},
		{"tagged job, untagged worker", []string{"gpu"}, nil, false, false},
		{"tagged job, matching worker", []string{"gpu"}, []string{"gpu"}, false, true},
		{"tagged job, superset worker", []string{"gpu"}, []string{"gpu", "vpn"}, false, true},
		{"tagged job, partial worker", []string{"gpu", "vpn"}, []string{"gpu"}, false, false},
		{"tagged job, exact match", []string{"gpu", "vpn"}, []string{"gpu", "vpn"}, false, true},
		{"tagged job, exclusive matching", []string{"gpu"}, []string{"gpu"}, true, true},
		{"tagged job, exclusive no match", []string{"vpn"}, []string{"gpu"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TagsMatch(tt.jobTags, tt.workerTags, tt.exclusiveTags)
			if got != tt.want {
				t.Errorf("TagsMatch(%v, %v, %v) = %v, want %v",
					tt.jobTags, tt.workerTags, tt.exclusiveTags, got, tt.want)
			}
		})
	}
}

func TestValidateTags(t *testing.T) {
	tests := []struct {
		name    string
		tags    []string
		wantErr bool
	}{
		{"empty", nil, false},
		{"valid single", []string{"gpu"}, false},
		{"valid multiple", []string{"gpu", "vpn"}, false},
		{"valid with hyphens", []string{"my-tag"}, false},
		{"invalid uppercase", []string{"GPU"}, true},
		{"invalid spaces", []string{"my tag"}, true},
		{"duplicate tags", []string{"gpu", "gpu"}, true},
		{"too many tags", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTags(tt.tags)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTags(%v) error = %v, wantErr %v", tt.tags, err, tt.wantErr)
			}
		})
	}
}
