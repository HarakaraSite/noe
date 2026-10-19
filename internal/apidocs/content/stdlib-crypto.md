# node:crypto

Offers hashing, HMAC, random values, key derivation, and a limited Web Crypto surface. The global crypto is also installed. These exports are the implemented API, not the full Node crypto module.

### Exports

`constants`, `createHash`, `createHmac`, `default`, `getHashes`, `getRandomValues`, `hkdf`, `hkdfSync`, `pbkdf2`, `pbkdf2Sync`, `randomBytes`, `randomFillSync`, `randomInt`, `randomUUID`, `subtle`, `timingSafeEqual`, `webcrypto`

The Web Crypto subtle surface is intentionally small and source-defined; do not assume unsupported algorithms or key formats.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
