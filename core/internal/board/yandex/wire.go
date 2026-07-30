package yandex

import (
	"encoding/json"

	"bproxy-core/internal/board"
)

// dashboardEnvelope is the top-level body of every realtime action: all
// application actions go through the single "dashboard" event (SPEC §5.2).
type dashboardEnvelope struct {
	Action      string `json:"action"`
	Data        any    `json:"data"`
	Participant string `json:"participant"`
}

// subscribeData is the payload of subscribe-slide-dashboard (SPEC §5.3). It must
// forward the whole properties and participant objects verbatim.
type subscribeData struct {
	Session             string           `json:"session"`
	Dashboard           string           `json:"dashboard"`
	Presentation        string           `json:"presentation"`
	Properties          json.RawMessage  `json:"properties"`
	Participant         string           `json:"participant"`
	ParticipantTeamRole int              `json:"participant_team_role"`
	Options             subscribeOptions `json:"options"`
}

type subscribeOptions struct {
	Type         string          `json:"type"`
	Participant  json.RawMessage `json:"participant"`
	Intermediate any             `json:"intermediate"`
	Device       map[string]any  `json:"device"`
}

// modifyData is the payload of modify-objects / delete-objects.
type modifyData struct {
	Objects []mxCell `json:"objects"`
}

// parseSnapshot extracts the page object snapshot from a subscribe ack. The ack
// callback's first argument is a data object carrying `objects` (SPEC §5.3).
func parseSnapshot(args []json.RawMessage) []board.Object {
	if len(args) == 0 {
		return nil
	}
	var ack struct {
		Objects []json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal(args[0], &ack); err != nil {
		return nil
	}
	out := make([]board.Object, 0, len(ack.Objects))
	for _, raw := range ack.Objects {
		if obj, ok := parseCell(raw); ok {
			out = append(out, obj)
		}
	}
	return out
}

// incomingEnvelope is a "dashboard" broadcast from the server. The server emits
// action "server-<original>" to other subscribers (SPEC §5.2).
type incomingEnvelope struct {
	Action      string          `json:"action"`
	Data        json.RawMessage `json:"data"`
	Participant string          `json:"participant"`
}

// toEvents converts a broadcast into board.Events. server-modify-objects
// carries created/updated cells; server-drop-objects carries deleted object
// hashes. Both action names and payloads are verified against captured board
// traffic.
func (e incomingEnvelope) toEvents() []board.Event {
	switch e.Action {
	case "server-modify-objects":
		var d modifyData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return nil
		}
		var evs []board.Event
		for _, cell := range d.Objects {
			raw, err := json.Marshal(cell)
			if err != nil {
				continue
			}
			if obj, ok := parseCell(raw); ok {
				evs = append(evs, board.Event{Kind: board.Created, Object: obj})
			}
		}
		return evs
	case "server-drop-objects":
		return deleteEventsFromData(e.Data)
	default:
		return nil
	}
}

// deleteEventsFromData best-effort extracts deleted object ids from a delete
// broadcast, tolerating either an {objects:[...]} shape or a bare id array.
func deleteEventsFromData(data json.RawMessage) []board.Event {
	var withObjects modifyData
	if err := json.Unmarshal(data, &withObjects); err == nil && len(withObjects.Objects) > 0 {
		var evs []board.Event
		for _, cell := range withObjects.Objects {
			id := cell.Attributes.ID
			if id == "" {
				id = cell.Hash
			}
			if id != "" {
				evs = append(evs, board.Event{Kind: board.Deleted, Object: board.Object{ID: id}})
			}
		}
		return evs
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err == nil {
		evs := make([]board.Event, 0, len(ids))
		for _, id := range ids {
			evs = append(evs, board.Event{Kind: board.Deleted, Object: board.Object{ID: id}})
		}
		return evs
	}
	return nil
}
