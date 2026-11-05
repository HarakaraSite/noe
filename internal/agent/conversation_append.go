package agent

import (
	jsonv1 "encoding/json"
	"errors"
)

// conversationAppend extends one committed revision. Existing entries are
// immutable; only incoming messages are validated and copied into host ownership.
type conversationAppend struct {
	BaseRevision int64               `json:"baseRevision"`
	Entries      []conversationEntry `json:"entries"`
}

func cloneConversationEntry(entry conversationEntry) conversationEntry {
	copy := conversationEntry{ID: entry.ID, Kind: entry.Kind}
	if entry.Messages != nil {
		copy.Messages = make([]jsonv1.RawMessage, len(entry.Messages))
		for i, message := range entry.Messages {
			copy.Messages[i] = append(jsonv1.RawMessage(nil), message...)
		}
	}
	return copy
}

// conversationAppendCandidate must run with Host.mu held. The snapshot form
// remains available to direct internal callers; managed JS sends appends.
func conversationAppendCandidate(current, snapshot *conversationSnapshot, delta *conversationAppend, count int, kind string, allowReplay bool) (*conversationSnapshot, bool, error) {
	if current == nil || (snapshot == nil) == (delta == nil) {
		return nil, false, errors.New("one conversation update and a committed history are required")
	}
	if snapshot != nil {
		if allowReplay && conversationSnapshotEqual(current, snapshot) {
			return current, true, nil
		}
		if err := validateConversationAppend(current, snapshot, count, kind); err != nil {
			return nil, false, err
		}
		return cloneConversationSnapshot(snapshot), false, nil
	}
	if count <= 0 || len(delta.Entries) != count || delta.BaseRevision < 2 || delta.BaseRevision > maxSafeContextBudget-int64(count) {
		return nil, false, errors.New("invalid conversation append")
	}
	seen := make(map[string]struct{}, count)
	for _, entry := range delta.Entries {
		if err := validateConversationEntry(entry); err != nil {
			return nil, false, err
		}
		if entry.Kind != kind {
			return nil, false, errors.New("conversation append has an unexpected entry kind")
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			return nil, false, errors.New("duplicate conversation entry ID")
		}
		seen[entry.ID] = struct{}{}
	}
	if allowReplay && delta.BaseRevision+int64(count) == current.Revision && len(current.Entries) >= count {
		for i, entry := range current.Entries[len(current.Entries)-count:] {
			if !conversationEntryEqual(entry, delta.Entries[i]) {
				return nil, false, errors.New("conversation append replay does not match committed entries")
			}
		}
		return current, true, nil
	}
	if delta.BaseRevision != current.Revision || current.Revision > maxSafeContextBudget-int64(count) || current.Sequence > maxSafeContextBudget-int64(count) {
		return nil, false, errors.New("conversation append is stale or has an invalid revision")
	}
	for _, entry := range current.Entries {
		if _, duplicate := seen[entry.ID]; duplicate {
			return nil, false, errors.New("duplicate conversation entry ID")
		}
	}
	next := &conversationSnapshot{
		Revision: current.Revision + int64(count), Sequence: current.Sequence + int64(count),
		Entries: make([]conversationEntry, len(current.Entries)+count),
	}
	copy(next.Entries, current.Entries)
	for i, entry := range delta.Entries {
		next.Entries[len(current.Entries)+i] = cloneConversationEntry(entry)
	}
	return next, false, nil
}
