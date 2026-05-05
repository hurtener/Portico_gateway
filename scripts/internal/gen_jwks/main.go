// gen_jwks: small one-shot tool to produce testdata/jwks-test.json with a
// deterministic dummy public RSA key. Used only for the testdata fixture
// that lets `portico validate testdata/portico.yaml` succeed without
// committing a real keypair.
//
// Usage:
//   go run ./scripts/internal/gen_jwks/ > testdata/jwks-test.json
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
)

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
	N   string `json:"n"`
	E   string `json:"e"`
}
type jwks struct {
	Keys []jwk `json:"keys"`
}

func main() {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pub := priv.PublicKey
	out := jwks{Keys: []jwk{{
		Kty: "RSA",
		Kid: "portico-test-fixture",
		Use: "sig",
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(bigEndian(pub.E)),
	}}}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func bigEndian(e int) []byte {
	if e == 0 {
		return []byte{0}
	}
	var buf []byte
	for e > 0 {
		buf = append([]byte{byte(e & 0xff)}, buf...)
		e >>= 8
	}
	return buf
}
