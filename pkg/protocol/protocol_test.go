package protocol

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDecode(t *testing.T) {
	valid := `{"protocol_version":1,"type":"player.init","request_id":"req-1","payload":{}}`
	if _, err := Decode([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"protocol_version":2,"type":"ping","request_id":"r","payload":{}}`,
		`{"protocol_version":1,"type":"ping","request_id":"r","payload":null}`,
		`{"protocol_version":1,"type":"ping","request_id":"r","payload":[]}`,
		`{"protocol_version":1,"type":"ping","request_id":"","payload":{}}`,
		`{"protocol_version":1,"type":"ping","request_id":"r","payload":{},"extra":1}`,
		valid + `{}`, "invalid", "null",
	} {
		if _, err := Decode([]byte(invalid)); err == nil {
			t.Errorf("accepted %s", invalid)
		}
	}
}

func TestContractExamplesAndRequiredInput(t *testing.T) {
	data, err := os.ReadFile("../../contracts/examples/client.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []json.RawMessage
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	for _, raw := range examples {
		e, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if e.Type == "player.input" {
			if _, err := DecodeInput(e.Payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, invalid := range []string{
		`{"seq":1}`, `{"seq":1,"x":null,"y":0,"z":0,"yaw":0}`,
		`{"seq":0,"x":0,"y":0,"z":0,"yaw":0}`,
		`{"seq":9007199254740992,"x":0,"y":0,"z":0,"yaw":0}`,
	} {
		if _, err := DecodeInput([]byte(invalid)); err == nil {
			t.Errorf("accepted %s", invalid)
		}
	}
}
