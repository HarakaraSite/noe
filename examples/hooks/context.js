export async function beforeModelRequest(event, api) {
  const context = await api.context.get();
  const execution = await api.execution.get();
  await api.context.setRuntimeInfo({
    key: "execution",
    text: `Group ${execution.groupId}: ${execution.counters.providerCalls} provider calls started.`,
  });
  if (context.usedPercent !== null && context.usedPercent >= 90) {
    await api.context.setRuntimeInfo({
      key: "context-guidance",
      text: "The estimated input is near the advisory budget. Keep tool output concise and save progress to a file when useful.",
    });
  }
}
