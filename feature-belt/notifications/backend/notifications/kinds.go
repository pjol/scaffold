package notifications

import (
	"sort"
	"strings"
	"sync"
)

// Kind declares one notification kind for the preferences screen and sets
// its default channels. Modules call RegisterKind in their New. Inbox
// delivery is always on; Push and Email are the defaults until the user
// saves a choice.
type Kind struct {
	ID    string // "chat.message"
	Label string // "Messages" (settings row)
	Push  bool   // default on/off per channel
	Email bool
	// AgentTarget names the item a notification of this kind is about, read
	// from its data (DataTarget("channels.channel", "channel_id")), so an AI
	// agent reading the inbox for the person sees it only while the person's
	// agent scope lets agents read that item. Nil: data {"kind", "id"} when
	// present, else the kind's feature (its prefix) decides as a whole.
	AgentTarget func(data map[string]any) (kind, id string)
}

// Kinds that are sent but never listed in preferences or subject to them.
const kindTest = "notifications.test"

// Defaults for a kind no module registered: push on, email off. Register
// the kind to show it in preferences and choose its defaults.
var unregisteredDefaults = Kind{Push: true, Email: false}

var (
	kindsMu sync.RWMutex
	kinds   = map[string]Kind{}
)

// RegisterKind adds or replaces a kind. Call it in a module's New; the
// registry is read at request time, so construction order never matters.
func RegisterKind(k Kind) {
	k.ID = strings.TrimSpace(k.ID)
	if k.ID == "" {
		panic("notifications: RegisterKind with an empty ID")
	}
	if k.Label == "" {
		k.Label = k.ID
	}
	kindsMu.Lock()
	kinds[k.ID] = k
	kindsMu.Unlock()
}

// lookupKind returns the registered kind, or the unregistered defaults.
func lookupKind(id string) (Kind, bool) {
	kindsMu.RLock()
	defer kindsMu.RUnlock()
	k, ok := kinds[id]
	if !ok {
		k = unregisteredDefaults
		k.ID, k.Label = id, id
	}
	return k, ok
}

// registeredKinds lists every registered kind ordered by group, then label.
func registeredKinds() []Kind {
	kindsMu.RLock()
	out := make([]Kind, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k)
	}
	kindsMu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		gi, gj := kindGroup(out[i].ID), kindGroup(out[j].ID)
		if gi != gj {
			return gi < gj
		}
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// kindGroup is the module prefix of a kind id: "chat" for "chat.message".
func kindGroup(id string) string {
	group, _, _ := strings.Cut(id, ".")
	return group
}
