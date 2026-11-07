import {
	CompactSign,
	compactVerify,
	jwtVerify,
	SignJWT,
} from "./node_modules/jose/dist/webapi/index.js";

// Deterministic fixtures, with a fixed verification clock and shared material.
const secret = Uint8Array.from({ length: 64 }, (_, index) => index + 1);
const now = 1_794_009_600;
const date = new Date(now * 1_000);
const algorithms = ["HS256", "HS384", "HS512"];
const encoder = new TextEncoder();
const decoder = new TextDecoder();

function options(algorithm) {
	return {
		algorithms: [algorithm],
		issuer: "noe-fixture",
		audience: "local-consumer",
		currentDate: date,
	};
}

async function rejected(operation, code) {
	let failure;
	try {
		await operation();
	} catch (error) {
		failure = error;
	}
	if (!failure || failure.code !== code) {
		throw new Error(`expected ${code}, received ${failure?.code}`);
	}
}

function token(algorithm, expiration = now + 600) {
	return new SignJWT({ message: "Web Crypto 日本語", fixture: true })
		.setProtectedHeader({ alg: algorithm })
		.setIssuer("noe-fixture")
		.setAudience("local-consumer")
		.setSubject("local-fixture")
		.setIssuedAt(now)
		.setExpirationTime(expiration);
}

export async function runJoseCases(peerTokens = []) {
	const tokens = [];
	let checks = 0;
	for (const algorithm of algorithms) {
		const hash = `SHA-${algorithm.slice(2)}`;
		const signingKey = await crypto.subtle.importKey(
			"raw",
			secret,
			{ name: "HMAC", hash },
			false,
			["sign"],
		);
		const verifyingKey = await crypto.subtle.importKey(
			"raw",
			secret,
			{ name: "HMAC", hash },
			false,
			["verify"],
		);
		for (const [mode, key] of [
			["raw", secret],
			["CryptoKey", signingKey],
		]) {
			const signed = await token(algorithm).sign(key);
			const result = await jwtVerify(signed, verifyingKey, options(algorithm));
			if (
				result.payload.message !== "Web Crypto 日本語" ||
				result.payload.sub !== "local-fixture"
			) {
				throw new Error("JWT payload changed");
			}
			tokens.push({ algorithm, mode, token: signed });
			checks++;
		}
		const signed = tokens[tokens.length - 1].token;
		const parts = signed.split(".");
		parts[2] = (parts[2][0] === "A" ? "B" : "A") + parts[2].slice(1);
		await rejected(
			() => jwtVerify(parts.join("."), secret, options(algorithm)),
			"ERR_JWS_SIGNATURE_VERIFICATION_FAILED",
		);
		await rejected(
			() =>
				jwtVerify(signed, secret, { ...options(algorithm), issuer: "wrong" }),
			"ERR_JWT_CLAIM_VALIDATION_FAILED",
		);
		await rejected(
			() =>
				jwtVerify(signed, secret, { ...options(algorithm), audience: "wrong" }),
			"ERR_JWT_CLAIM_VALIDATION_FAILED",
		);
		const expired = await token(algorithm, now - 60).sign(secret);
		await rejected(
			() => jwtVerify(expired, secret, options(algorithm)),
			"ERR_JWT_EXPIRED",
		);
		checks += 4;
		const payload = encoder.encode("compact JWS 日本語");
		const compact = await new CompactSign(payload)
			.setProtectedHeader({ alg: algorithm })
			.sign(signingKey);
		const verified = await compactVerify(compact, verifyingKey, {
			algorithms: [algorithm],
		});
		if (decoder.decode(verified.payload) !== "compact JWS 日本語")
			throw new Error("JWS payload changed");
		checks++;
	}
	for (const peer of peerTokens) {
		const verified = await jwtVerify(
			peer.token,
			secret,
			options(peer.algorithm),
		);
		if (verified.payload.sub !== "local-fixture")
			throw new Error("peer JWT changed");
		checks++;
	}
	return {
		sdk: "jose",
		version: "6.2.15",
		algorithms,
		checks,
		peerTokens: peerTokens.length,
		tokens,
	};
}
