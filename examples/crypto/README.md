# Web Crypto and jose

[Examples](../README.md) · [Contract and standard API gaps](../../docs/development/stdlib-webcrypto.md)

The corrected Web Crypto contracts are available in v2.20.0 and later. Use
the matching release binary or build the current source for these examples.
Earlier releases retain the permissive key-policy behavior corrected here.

```sh
CGO_ENABLED=0 go build -o /tmp/noe-webcrypto ./cmd/noe
/tmp/noe-webcrypto run examples/crypto/webcrypto.js
```

The example imports separate HMAC signing and verification keys, verifies a
message, and checks that the verification key cannot sign. No filesystem,
network, command, authentication, or provider capability is needed by the
cryptographic operations themselves.

## Real SDK execution

The SDK example pins unmodified `jose` 6.2.15 in package-lock.json. Its published
Web API ESM files are imported directly by their explicit local path. NOE does
not acquire npm package discovery or require a patched SDK. Node/npm prepare
the dependency; NOE executes the example.

```sh
npm ci --ignore-scripts --no-audit --no-fund --prefix examples/crypto
/tmp/noe-webcrypto run --no-inst examples/crypto/jose-jwt.js
```

The fixture covers HS256/HS384/HS512 JWT and compact JWS, raw shared material
and CryptoKey inputs, signature tampering, expiration, issuer, and audience.
It uses deterministic local fixture keys and a fixed verification clock;
it does not contact an identity service or perform a login.

For Worker execution and independent interoperability with Node Web Crypto:

```sh
python3 scripts/check-webcrypto-sdk.py \
  --binary /tmp/noe-webcrypto --output /var/tmp/noe-webcrypto-sdk-check
```

The output directory must be new and outside the checkout. The harness records
stdout/stderr, fingerprints the unmodified SDK files, verifies Node-produced
JWTs with NOE and NOE-produced JWTs with Node, and runs the same fixture in a
NOE Worker. It also records unsupported key-generation, CryptoKey JWK export,
and AES-GCM JWE paths. Successful HMAC fixtures do not imply complete jose or
Web Crypto support.
