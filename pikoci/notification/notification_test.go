package notification_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/pikoci/pikoci/pikoci/notification"
)

func TestGetParams(t *testing.T) {
	n := notification.Notification{
		Params: &notification.Params{
			Params: map[string]string{"key": "value"},
		},
	}
	assert.Equal(t, map[string]string{"key": "value"}, n.GetParams())
}

func TestGetParams_Nil(t *testing.T) {
	n := notification.Notification{}
	assert.Nil(t, n.GetParams())
}
