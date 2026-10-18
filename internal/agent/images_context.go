package agent

import (
	"encoding/json"
	"math"
	imageapi "noe/internal/image"
)

// An explicitly labelled approximation, not a provider tokenizer or billing
// calculation. Details and source are recorded in docs/agent-images-plan.md.
func estimateImageTokens(data *imageapi.Data) int64 {
	w, h := float64(data.Width), float64(data.Height)
	if data.Detail == "low" {
		scale := math.Min(1, 512/math.Max(w, h))
		w = math.Ceil(w * scale)
		h = math.Ceil(h * scale)
	}
	return int64(math.Ceil(math.Ceil(w/32) * math.Ceil(h/32) * 1.2))
}
func imageEstimatesLocked(run *agentRun) map[string]int64 {
	result := map[string]int64{}
	for id, data := range run.imageStore() {
		result[id] = estimateImageTokens(data)
	}
	return result
}
func historyImageTokens(messages []json.RawMessage, estimates map[string]int64) int64 {
	var total int64
	for _, id := range imageReferences(messages) {
		total += estimates[id]
	}
	return total
}
func imageEstimateMethod(imageTokens int64) string {
	if imageTokens == 0 {
		return "json_utf8_bytes/4"
	}
	return "json_utf8_bytes/4+image_32px_patches_x1.2_fallback"
}
