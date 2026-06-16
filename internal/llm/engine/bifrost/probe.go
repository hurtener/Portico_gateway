// Package bifrost is a temporary compile probe for the Bifrost SDK dependency.
// Replaced by the real adapter in the next unit step.
package bifrost

import (
	bcore "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

var _ = bcore.Init
var _ schemas.Account
