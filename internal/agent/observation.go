package agent

import "forge.harakara.site/littleisland/noe/v2/internal/observation"

// ObserveLog joins the same operation lifetime as provider requests. Closing an
// evaluation cancels a pending wait and joins its file work before returning.
func (op *Operation) ObserveLog(query observation.Query) (observation.Snapshot, error) {
	if !op.beginRequest() {
		return observation.Snapshot{}, ErrOperationClosed
	}
	defer op.requests.Done()
	return observation.Observe(op.ctx, query)
}
