package stdlib

import (
	_ "embed"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed webcrypto.js
var webCryptoJS string

// installWebCrypto adds the Web Crypto key operations to the Crypto object
// installed by webAPIs. The Node-style crypto module is installed afterwards
// and observes these same object identities.
func installWebCrypto(rt *quickjs.Runtime, native quickjs.Value) error {
	host := rt.NewObject()
	if err := host.Set("native", native); err != nil {
		return err
	}
	keyConstructor, err := evalWithHost(rt, "<webcrypto>", webCryptoJS, host)
	if err != nil {
		return err
	}
	return rt.Set("CryptoKey", keyConstructor)
}
