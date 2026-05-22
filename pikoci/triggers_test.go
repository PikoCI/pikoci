package pikoci_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/trigger"
	"go.uber.org/mock/gomock"
)

func TestCreateTrigger(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	version := map[string]interface{}{"payload": "build succeeded"}
	s.Triggers.EXPECT().Create(ctx, "main", "trigger.deploy", version).Return(&trigger.Trigger{
		ID:      1,
		Name:    "trigger.deploy",
		Version: version,
	}, nil)

	tr, err := s.S.CreateTrigger(ctx, "main", "trigger.deploy", version)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), tr.ID)
	assert.Equal(t, "trigger.deploy", tr.Name)
	assert.Equal(t, "build succeeded", tr.Version["payload"])
}

func TestCreateTrigger_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateTrigger(ctx, "INVALID", "trigger.deploy", nil)
	require.Error(t, err)
}

func TestCreateTrigger_EmptyName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateTrigger(ctx, "main", "", nil)
	require.Error(t, err)
}

func TestListTriggersAfter(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Triggers.EXPECT().FilterAfter(ctx, "main", "trigger.deploy", uint32(0)).Return([]*trigger.Trigger{
		{ID: 1, Name: "trigger.deploy", Version: map[string]interface{}{"payload": "v1"}},
		{ID: 2, Name: "trigger.deploy", Version: map[string]interface{}{"payload": "v2"}},
	}, nil)

	triggers, err := s.S.ListTriggersAfter(ctx, "main", "trigger.deploy", 0)
	require.NoError(t, err)
	require.Len(t, triggers, 2)
	assert.Equal(t, uint32(1), triggers[0].ID)
	assert.Equal(t, uint32(2), triggers[1].ID)
}

func TestListTriggersAfter_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.ListTriggersAfter(ctx, "INVALID", "trigger.deploy", 0)
	require.Error(t, err)
}

func TestListTriggersAfter_EmptyName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.ListTriggersAfter(ctx, "main", "", 0)
	require.Error(t, err)
}

func TestListTriggersAfter_WithAfterID(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Triggers.EXPECT().FilterAfter(ctx, "main", "trigger.deploy", uint32(5)).Return([]*trigger.Trigger{
		{ID: 6, Name: "trigger.deploy"},
	}, nil)

	triggers, err := s.S.ListTriggersAfter(ctx, "main", "trigger.deploy", 5)
	require.NoError(t, err)
	require.Len(t, triggers, 1)
	assert.Equal(t, uint32(6), triggers[0].ID)
}

func TestListTriggersAfter_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Triggers.EXPECT().FilterAfter(ctx, "main", "trigger.deploy", uint32(10)).Return(nil, nil)

	triggers, err := s.S.ListTriggersAfter(ctx, "main", "trigger.deploy", 10)
	require.NoError(t, err)
	assert.Empty(t, triggers)
}
