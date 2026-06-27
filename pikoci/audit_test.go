package pikoci_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// limitMatcher is a custom gomock.Matcher that verifies the Limit field
// of a FilterOpts value.
type limitMatcher struct {
	expected uint32
}

func (m limitMatcher) Matches(x interface{}) bool {
	opts, ok := x.(auditlog.FilterOpts)
	if !ok {
		return false
	}
	return opts.Limit == m.expected
}

func (m limitMatcher) String() string {
	return fmt.Sprintf("FilterOpts with Limit=%d", m.expected)
}

func hasLimit(limit uint32) gomock.Matcher {
	return limitMatcher{expected: limit}
}

func makeEntries(n int) []*auditlog.Entry {
	entries := make([]*auditlog.Entry, n)
	for i := 0; i < n; i++ {
		entries[i] = &auditlog.Entry{ID: uint32(i)}
	}
	return entries
}

func TestListAuditLog_DefaultLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.AuditLogs.EXPECT().
		Filter(gomock.Any(), "main", hasLimit(51)).
		Return(makeEntries(51), nil)

	entries, hasMore, err := s.P.ListAuditLog(ctx, "main", auditlog.FilterOpts{Limit: 0})
	require.NoError(t, err)
	assert.Len(t, entries, 50)
	assert.True(t, hasMore)
}

func TestListAuditLog_CustomLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.AuditLogs.EXPECT().
		Filter(gomock.Any(), "main", hasLimit(11)).
		Return(makeEntries(11), nil)

	entries, hasMore, err := s.P.ListAuditLog(ctx, "main", auditlog.FilterOpts{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, entries, 10)
	assert.True(t, hasMore)
}

func TestListAuditLog_NoMore(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.AuditLogs.EXPECT().
		Filter(gomock.Any(), "main", hasLimit(11)).
		Return(makeEntries(5), nil)

	entries, hasMore, err := s.P.ListAuditLog(ctx, "main", auditlog.FilterOpts{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, entries, 5)
	assert.False(t, hasMore)
}

func TestListAuditLog_ExactMatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.AuditLogs.EXPECT().
		Filter(gomock.Any(), "main", hasLimit(11)).
		Return(makeEntries(10), nil)

	entries, hasMore, err := s.P.ListAuditLog(ctx, "main", auditlog.FilterOpts{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, entries, 10)
	assert.False(t, hasMore)
}

func TestListAuditLog_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.AuditLogs.EXPECT().
		Filter(gomock.Any(), "main", hasLimit(51)).
		Return(nil, nil)

	entries, hasMore, err := s.P.ListAuditLog(ctx, "main", auditlog.FilterOpts{})
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.False(t, hasMore)
}

func TestListAuditLog_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expectedErr := fmt.Errorf("db connection failed")
	s.AuditLogs.EXPECT().
		Filter(gomock.Any(), "main", hasLimit(11)).
		Return(nil, expectedErr)

	entries, hasMore, err := s.P.ListAuditLog(ctx, "main", auditlog.FilterOpts{Limit: 10})
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, entries)
	assert.False(t, hasMore)
}

// entryMatcher verifies the audit entry passed to Create has the expected action and actor.
type entryMatcher struct {
	action auditlog.Action
	actor  string
}

func (m entryMatcher) Matches(x interface{}) bool {
	e, ok := x.(auditlog.Entry)
	if !ok {
		return false
	}
	return e.Action == m.action && e.Actor == m.actor
}

func (m entryMatcher) String() string {
	return fmt.Sprintf("Entry{Action: %s, Actor: %s}", m.action, m.actor)
}

func TestAudit_ActorFromContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)

	// Replace with a fresh mock to override the AnyTimes blanket
	alr := mock.NewAuditLogRepository(ctrl)
	s.P.AuditLogs = alr

	ctx := context.WithValue(context.TODO(), pikoci.ActorContextKey, "alice")

	alr.EXPECT().Create(gomock.Any(), "main",
		entryMatcher{action: auditlog.PipelinePaused, actor: "alice"}).Return(nil)

	s.Jobs.EXPECT().PauseAll(gomock.Any(), "main", "my-pipe").Return(nil)

	err := s.S.PausePipeline(ctx, "main", "my-pipe")
	require.NoError(t, err)
}

func TestAudit_DefaultActorIsSystem(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)

	alr := mock.NewAuditLogRepository(ctrl)
	s.P.AuditLogs = alr

	ctx := context.TODO() // no actor in context

	alr.EXPECT().Create(gomock.Any(), "main",
		entryMatcher{action: auditlog.PipelinePaused, actor: "system"}).Return(nil)

	s.Jobs.EXPECT().PauseAll(gomock.Any(), "main", "my-pipe").Return(nil)

	err := s.S.PausePipeline(ctx, "main", "my-pipe")
	require.NoError(t, err)
}

func TestAudit_FireAndForget(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)

	alr := mock.NewAuditLogRepository(ctrl)
	s.P.AuditLogs = alr

	ctx := context.TODO()

	// Audit Create fails, but service method should still succeed
	alr.EXPECT().Create(gomock.Any(), "main", gomock.Any()).Return(fmt.Errorf("db write failed"))

	s.Jobs.EXPECT().PauseAll(gomock.Any(), "main", "my-pipe").Return(nil)

	err := s.S.PausePipeline(ctx, "main", "my-pipe")
	require.NoError(t, err, "service method should succeed even if audit fails")
}

func TestAudit_NilAuditLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Set AuditLogs to nil — audit should silently skip
	s.P.AuditLogs = nil

	s.Jobs.EXPECT().PauseAll(gomock.Any(), "main", "my-pipe").Return(nil)

	err := s.S.PausePipeline(ctx, "main", "my-pipe")
	require.NoError(t, err, "should work fine with nil AuditLogs")
}

func TestAudit_DeletePipelineDetails(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)

	alr := mock.NewAuditLogRepository(ctrl)
	s.P.AuditLogs = alr

	ctx := context.WithValue(context.TODO(), pikoci.ActorContextKey, "bob")

	alr.EXPECT().Create(gomock.Any(), "main", gomock.Any()).DoAndReturn(
		func(_ context.Context, tc string, e auditlog.Entry) error {
			assert.Equal(t, auditlog.PipelineDeleted, e.Action)
			assert.Equal(t, "pipeline", e.TargetType)
			assert.Equal(t, "my-pipe", e.TargetName)
			assert.Equal(t, "bob", e.Actor)
			return nil
		})

	s.Pipelines.EXPECT().Delete(gomock.Any(), "main", "my-pipe").Return(nil)

	err := s.P.DeletePipeline(ctx, "main", "my-pipe")
	require.NoError(t, err)
}
