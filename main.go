package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	DefaultHubURL            = "https://mcp-hub-agent.cfapps.eu12.hana.ondemand.com"
	DefaultHeartbeatInterval = 30 * time.Second
	DefaultHealthTimeout     = 60 * time.Second
	ManifestFile             = "mcp-manifest.json"
)

type MCPManifest struct {
	Name         string       `json:"name"`
	Version      string       `json:"version,omitempty"`
	Description  string       `json:"description,omitempty"`
	Capabilities []Capability `json:"capabilities"`
}

type Capability struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
	HTTP        HTTPConfig             `json:"http"`
}

type HTTPConfig struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type VCAPApplication struct {
	ApplicationURIs []string `json:"application_uris"`
	Name            string   `json:"name"`
}

type RegisterRequest struct {
	URL          string       `json:"url"`
	Name         string       `json:"name,omitempty"`
	Capabilities []Capability `json:"capabilities,omitempty"`
}

func main() {
	log.SetPrefix("[mcp-sidecar] ")
	log.Println("Starting MCP Sidecar...")

	hubURL := getHubURL()
	appURL := getAppURL()

	if hubURL == "" {
		log.Fatal("No Hub URL found. Set HUB_URL env var.")
	}
	if appURL == "" {
		log.Fatal("No App URL found. Set APP_URL or deploy to CF.")
	}

	log.Printf("Hub URL: %s", hubURL)
	log.Printf("App URL: %s", appURL)

	// Load capabilities from mcp-manifest.json
	manifest, err := loadManifest()
	if err != nil {
		log.Fatalf("Failed to load %s: %v", ManifestFile, err)
	}
	log.Printf("Loaded %d capabilities from %s", len(manifest.Capabilities), ManifestFile)

	// Wait for app to be ready (using first capability endpoint)
	waitForApp(appURL, manifest)

	// Register with capabilities
	if err := register(hubURL, appURL, manifest); err != nil {
		log.Printf("Initial registration failed: %v, will retry...", err)
	}

	// Heartbeat loop
	log.Printf("Starting heartbeat loop (interval: %v)", DefaultHeartbeatInterval)
	for {
		time.Sleep(DefaultHeartbeatInterval)
		if err := heartbeat(hubURL, appURL); err != nil {
			log.Printf("Heartbeat failed: %v, re-registering...", err)
			if err := register(hubURL, appURL, manifest); err != nil {
				log.Printf("Re-registration failed: %v", err)
			}
		}
	}
}

func getHubURL() string {
	if url := os.Getenv("HUB_URL"); url != "" {
		return url
	}

	// Try VCAP_SERVICES for bound service
	if vcapServices := os.Getenv("VCAP_SERVICES"); vcapServices != "" {
		var services map[string][]struct {
			Credentials struct {
				HubURL string `json:"hub_url"`
			} `json:"credentials"`
		}
		if err := json.Unmarshal([]byte(vcapServices), &services); err == nil {
			for name, instances := range services {
				if len(instances) > 0 && instances[0].Credentials.HubURL != "" {
					log.Printf("Found Hub URL in VCAP_SERVICES (%s)", name)
					return instances[0].Credentials.HubURL
				}
			}
		}
	}

	return DefaultHubURL
}

func getAppURL() string {
	if url := os.Getenv("APP_URL"); url != "" {
		return url
	}

	// Get from VCAP_APPLICATION
	if vcapApp := os.Getenv("VCAP_APPLICATION"); vcapApp != "" {
		var app VCAPApplication
		if err := json.Unmarshal([]byte(vcapApp), &app); err == nil {
			if len(app.ApplicationURIs) > 0 {
				return "https://" + app.ApplicationURIs[0]
			}
		}
	}

	return ""
}

func loadManifest() (*MCPManifest, error) {
	data, err := os.ReadFile(ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var manifest MCPManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	return &manifest, nil
}

func waitForApp(appURL string, manifest *MCPManifest) {
	// Use first capability's path to check if app is ready
	checkPath := "/health" // fallback
	if len(manifest.Capabilities) > 0 {
		checkPath = manifest.Capabilities[0].HTTP.Path
	}

	checkURL := appURL + checkPath
	log.Printf("Waiting for app to be ready at %s...", checkURL)

	client := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()

	for time.Since(start) < DefaultHealthTimeout {
		resp, err := client.Get(checkURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				log.Println("App is ready!")
				return
			}
		}
		time.Sleep(2 * time.Second)
	}

	log.Printf("App not ready after %v, registering anyway", DefaultHealthTimeout)
}

func register(hubURL, appURL string, manifest *MCPManifest) error {
	reqBody := RegisterRequest{
		URL:          appURL,
		Name:         manifest.Name,
		Capabilities: manifest.Capabilities,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(hubURL+"/api/apps", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("POST request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("Registered successfully: %s", string(body))
		return nil
	}

	return fmt.Errorf("registration failed: %d %s", resp.StatusCode, string(body))
}

func heartbeat(hubURL, appURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}

	encodedURL := url.PathEscape(appURL)
	heartbeatURL := fmt.Sprintf("%s/api/apps/%s/heartbeat", hubURL, encodedURL)

	resp, err := client.Post(heartbeatURL, "application/json", nil)
	if err != nil {
		return fmt.Errorf("POST request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("app not registered (404)")
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("heartbeat failed: %d", resp.StatusCode)
}
