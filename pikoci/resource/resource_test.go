package resource_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/pikoci/pikoci/pikoci/resource"
)

func TestGetParams(t *testing.T) {
	r := resource.Resource{
		Params: &resource.Params{
			Params: map[string]string{"url": "https://example.com"},
		},
	}
	assert.Equal(t, map[string]string{"url": "https://example.com"}, r.GetParams())
}

func TestGetParams_Nil(t *testing.T) {
	r := resource.Resource{}
	assert.Nil(t, r.GetParams())
}
