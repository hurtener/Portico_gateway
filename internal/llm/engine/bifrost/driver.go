package bifrost

import (
	"github.com/hurtener/Portico_gateway/internal/llm/engine"
	"github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
)

// bifrostDriver is the engine driver that builds Bifrost-backed engines. It
// self-registers; cmd/portico pulls it in via a blank import.
type bifrostDriver struct{}

func (bifrostDriver) Name() string { return driverName }

func (bifrostDriver) New(_ map[string]any, deps ifaces.Deps) (ifaces.Engine, error) {
	return newAdapter(deps), nil
}

func init() {
	engine.Register(bifrostDriver{})
}
