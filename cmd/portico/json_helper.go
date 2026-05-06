package main

import "encoding/json"

// jsonUnmarshal forwards to encoding/json. Kept as a separate symbol so
// phase5_wiring.go can call it without colliding with json packages
// imported elsewhere via different aliases.
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
