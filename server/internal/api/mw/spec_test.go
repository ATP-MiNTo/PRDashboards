package mw_test

import (
	"github.com/getkin/kin-openapi/openapi3"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
)

func specForTest() (*openapi3.T, error) { return gen.GetSwagger() }
