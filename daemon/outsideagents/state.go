package outsideagents

import "encoding/json"

func marshalState(state stored) ([]byte, error) {
	return json.MarshalIndent(state, "", "  ")
}

func unmarshalState(raw []byte, state *stored) error {
	return json.Unmarshal(raw, state)
}
