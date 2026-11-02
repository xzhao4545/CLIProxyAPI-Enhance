package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResponseModelValidationConfigRoundTrip(t *testing.T) {
	for _, header := range []string{"port: 8317\n", "config-version: 8\nserver:\n  port: 8317\n"} {
		data := []byte(header + "response-model-validation:\n  - provider: codex\n    models: [gpt-5.4, gpt-5.4-mini]\n")
		cfg, errParse := ParseConfigBytes(data)
		if errParse != nil || len(cfg.ResponseModelValidation) != 1 {
			t.Fatalf("parse validation config: %v", errParse)
		}
		for _, codec := range []struct {
			marshal   func(any) ([]byte, error)
			unmarshal func([]byte, any) error
		}{{yaml.Marshal, yaml.Unmarshal}, {json.Marshal, json.Unmarshal}} {
			encoded, errMarshal := codec.marshal(cfg)
			if errMarshal != nil {
				t.Fatal(errMarshal)
			}
			var roundTrip Config
			if errDecode := codec.unmarshal(encoded, &roundTrip); errDecode != nil || !reflect.DeepEqual(cfg.ResponseModelValidation, roundTrip.ResponseModelValidation) {
				t.Fatalf("validation rules lost in round trip: %v", errDecode)
			}
		}
		path := filepath.Join(t.TempDir(), "config.yaml")
		if errWrite := os.WriteFile(path, data, 0600); errWrite != nil {
			t.Fatal(errWrite)
		}
		if errSave := SaveConfigPreserveComments(path, cfg); errSave != nil {
			t.Fatal(errSave)
		}
		reloaded, errLoad := LoadConfig(path)
		if errLoad != nil || !reflect.DeepEqual(cfg.ResponseModelValidation, reloaded.ResponseModelValidation) {
			t.Fatalf("validation rules lost on save: %v", errLoad)
		}
	}
}
