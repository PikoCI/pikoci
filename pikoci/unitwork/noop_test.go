package unitwork_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/unitwork"
)

func TestNoopUnitOfWork(t *testing.T) {
	repos := unitwork.Repositories{}
	suow := unitwork.NewNoopStartUnitOfWork(repos)

	err := suow(context.TODO(), func(uow unitwork.UnitOfWork) error {
		assert.Nil(t, uow.Users())
		assert.Nil(t, uow.Teams())
		assert.Nil(t, uow.Pipelines())
		assert.Nil(t, uow.Jobs())
		assert.Nil(t, uow.Resources())
		assert.Nil(t, uow.ResourceTypes())
		assert.Nil(t, uow.Builds())
		assert.Nil(t, uow.Runners())
		assert.Nil(t, uow.SecretTypes())
		assert.Nil(t, uow.NotificationTypes())
		assert.Nil(t, uow.Notifications())
		return nil
	})
	require.NoError(t, err)
}
