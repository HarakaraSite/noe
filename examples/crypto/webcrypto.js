const encoder = new TextEncoder();
const secret = encoder.encode("local Web Crypto example material");
const algorithm = { name: "HMAC", hash: "SHA-256" };
const signingKey = await crypto.subtle.importKey(
	"raw",
	secret,
	algorithm,
	false,
	["sign"],
);
const verifyingKey = await crypto.subtle.importKey(
	"raw",
	secret,
	algorithm,
	false,
	["verify"],
);
const message = encoder.encode("hello from NOE");
const signature = await crypto.subtle.sign("HMAC", signingKey, message);
if (!(await crypto.subtle.verify("HMAC", verifyingKey, signature, message))) {
	throw new Error("HMAC verification failed");
}
try {
	await crypto.subtle.sign("HMAC", verifyingKey, message);
	throw new Error("verify-only key permitted signing");
} catch (error) {
	if (!(error instanceof DOMException) || error.name !== "InvalidAccessError") {
		throw error;
	}
}
console.log("HMAC round-trip and key usages passed");
