package intouristcore

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// staticHelperConfig is the real, fixed helper.config.yaml for our own
// bridge relay (same on every install — it is not per-user or
// per-subscription data). It is embedded into the compiled .aar at build
// time, so gomobile bind bakes this file's contents straight into the
// binary; editing helper.config.yaml and rebuilding is all that's needed
// to change it, no Android-side plumbing required.
//
//go:embed helper.config.yaml
var staticHelperConfig string

// HelperConfigFromServerData generates a helper.config.yaml string from a JSON
// object that contains at least `host`, `port`, and `cred` (auth token).
// This replaces the need for a static helper.config.yaml file on Android.
func HelperConfigFromServerData(serverDataJSON string) (string, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(serverDataJSON), &data); err != nil {
		return "", fmt.Errorf("parse server data JSON: %w", err)
	}

	host, _ := data["host"].(string)
	if host == "" {
		return "", fmt.Errorf("host is required")
	}

	port, _ := data["port"].(float64)
	if port == 0 {
		port = 443
	}

	authToken, _ := data["cred"].(string)

	// Default config matches helper.config.yaml from third_party/adapter-and-helper
	cfg := map[string]interface{}{
		"bridge": map[string]interface{}{
			"url":         fmt.Sprintf("wss://%s:%d/_helper", host, int(port)),
			"authToken":   authToken,
			"reconnect": map[string]interface{}{
				"initialDelayMs":    1000,
				"maxDelayMs":        30000,
				"backoffMultiplier": 2.0,
			},
			"pingIntervalMs": 240000,
		},
		"listen": map[string]interface{}{
			"address": "127.0.0.1:1080",
		},
		"writeCoalescing": map[string]interface{}{
			"enabled": true,
			"delayMs": 50,
		},
		"wsApi": map[string]interface{}{
			"mode":  "grpc",
			"relay": false,
		},
		"logging": map[string]interface{}{
			"level": "info",
		},
	}

	// Override with user-provided values if present
	if val, ok := data["bridgeURL"].(string); ok && val != "" {
		cfg["bridge"].(map[string]interface{})["url"] = val
	}
	if val, ok := data["bridgeAuthToken"].(string); ok && val != "" {
		cfg["bridge"].(map[string]interface{})["authToken"] = val
	}
	if val, ok := data["listenAddress"].(string); ok && val != "" {
		cfg["listen"].(map[string]interface{})["address"] = val
	}

	// Ensure config does not have nil values
	for k, v := range cfg {
		if v == nil {
			delete(cfg, k)
		}
	}

	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal config to YAML: %w", err)
	}

	// Add YAML header (optional but matches original)
	return "# Intourist VPN helper config (generated)\n# " + time.Now().Format(time.RFC3339) + "\n\n" + string(yamlBytes), nil
}