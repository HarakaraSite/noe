package stdlib

import (
	_ "embed"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed webcrypto.js
var webCryptoJS string

var webCryptoFactory = fixedBuiltin("<webcrypto>", webCryptoJS)

// installWebCrypto adds the Web Crypto key operations to the Crypto object
// installed by webAPIs. The Node-style crypto module is installed afterwards
// and observes these same object identities.
func (mode builtinEvaluation) installWebCrypto(rt *quickjs.Runtime, native quickjs.Value) error {
	host := rt.NewObject()
	if err := host.Set("native", native); err != nil {
		return err
	}
	keyConstructor, err := mode.withHost(rt, webCryptoFactory, host)
	if err != nil {
		return err
	}
	return rt.Set("CryptoKey", keyConstructor)
}
