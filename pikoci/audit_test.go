package pikoci_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/pikoci/pikoci/pikoci/auditlog"
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
