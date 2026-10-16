export async function rpc(method, body) {
  const response = await fetch(process.env.NOE_AGENT_HOST + method, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Authorization": "Bearer " + process.env.NOE_AGENT_TOKEN,
      "X-Noe-Scope": process.env.NOE_AGENT_SCOPE,
    },
    body: JSON.stringify(body),
  });
  const value = await response.json();
  if (!response.ok) {
    const error = new Error(value.error?.message || `host status ${response.status}`);
    error.code = value.error?.code || "host";
    throw error;
  }
  return value;
}
