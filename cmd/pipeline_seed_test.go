package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikoci/pikoci/pikoci/pipeline"
)

// fakePipelineSvc records which of the two write paths createOrUpdatePipeline
// took, and whether it looked the pipeline up under the canonical name.
type fakePipelineSvc struct {
	existing *pipeline.Pipeline
	getErr   error

	gotCan     string
	created    int
	updated    int
	updatedCan string
}

func (f *fakePipelineSvc) GetPipeline(_ context.Context, _, pCan string) (*pipeline.Pipeline, error) {
	f.gotCan = pCan
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.existing, nil
}

func (f *fakePipelineSvc) CreatePipeline(_ context.Context, _, _ string, _ []byte, _ map[string]interface{}) (*pipeline.Pipeline, error) {
	f.created++
	return &pipeline.Pipeline{}, nil
}

func (f *fakePipelineSvc) UpdatePipeline(_ context.Context, _, pCan string, _ []byte, _ map[string]interface{}, _ ...string) (*pipeline.Pipeline, error) {
	f.updated++
	f.updatedCan = pCan
	return &pipeline.Pipeline{}, nil
}

func writeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pipeline.hcl")
	require.NoError(t, os.WriteFile(path, []byte(`job "hello" {
  task "say" {
    run "shell" { cmd = "echo hi" }
  }
}
`), 0o600))
	return path
}

// The --pipeline-* startup flags must be idempotent: a second boot against a
// persistent database has to update the existing pipeline rather than fail the
// insert and abort startup. See #686.
func TestCreateOrUpdatePipeline(t *testing.T) {
	config := writeConfig(t)

	tests := []struct {
		name            string
		svc             *fakePipelineSvc
		pipelineName    string
		wantCreated     int
		wantUpdated     int
		wantLookupCan   string
		wantUpdatedWith string
	}{
		{
			name:          "creates when absent",
			svc:           &fakePipelineSvc{getErr: errors.New("not found")},
			pipelineName:  "my-pipeline",
			wantCreated:   1,
			wantUpdated:   0,
			wantLookupCan: "my-pipeline",
		},
		{
			name:            "updates when present",
			svc:             &fakePipelineSvc{existing: &pipeline.Pipeline{Canonical: "my-pipeline"}},
			pipelineName:    "my-pipeline",
			wantCreated:     0,
			wantUpdated:     1,
			wantLookupCan:   "my-pipeline",
			wantUpdatedWith: "my-pipeline",
		},
		{
			name:            "looks up and updates by canonical name",
			svc:             &fakePipelineSvc{existing: &pipeline.Pipeline{Canonical: "my-pipeline"}},
			pipelineName:    "My Pipeline",
			wantCreated:     0,
			wantUpdated:     1,
			wantLookupCan:   "my-pipeline",
			wantUpdatedWith: "my-pipeline",
		},
		{
			name:          "creates when the lookup returns no pipeline and no error",
			svc:           &fakePipelineSvc{},
			pipelineName:  "my-pipeline",
			wantCreated:   1,
			wantUpdated:   0,
			wantLookupCan: "my-pipeline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := createOrUpdatePipeline(context.Background(), tt.svc, "main", tt.pipelineName, config, "")
			require.NoError(t, err)

			assert.Equal(t, tt.wantCreated, tt.svc.created, "CreatePipeline calls")
			assert.Equal(t, tt.wantUpdated, tt.svc.updated, "UpdatePipeline calls")
			assert.Equal(t, tt.wantLookupCan, tt.svc.gotCan, "canonical used for lookup")
			if tt.wantUpdatedWith != "" {
				assert.Equal(t, tt.wantUpdatedWith, tt.svc.updatedCan, "canonical passed to UpdatePipeline")
			}
		})
	}
}

func TestCreateOrUpdatePipelineMissingConfig(t *testing.T) {
	svc := &fakePipelineSvc{}
	err := createOrUpdatePipeline(context.Background(), svc, "main", "my-pipeline", "/does/not/exist.hcl", "")

	require.Error(t, err)
	assert.Zero(t, svc.created)
	assert.Zero(t, svc.updated)
}
