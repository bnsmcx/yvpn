package tailscale

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Machine struct {
	ID   string `json:"id"`
	Name string `json:"hostname"`
}

type Routes struct {
	AdvertisedRoutes []string `json:"advertisedRoutes"`
	EnabledRoutes    []string `json:"enabledRoutes"`
}

func EnableExit(name, token, tailnet string) error {
	machines, err := getTailscaleMachines(token, tailnet)
	if err != nil {
		return fmt.Errorf("Error retrieving Tailscale machines: %s", err.Error())
	}

	for _, machine := range machines {
		if machine.Name == name {
			if err := enableExitNode(machine.ID, token, tailnet); err != nil {
				return fmt.Errorf("Error enabling exit node: %v", err)
			}
		}
	}

  return nil
}

func getTailscaleMachines(token, tailnet string) ([]Machine, error) {
	apiURL := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s/machines", tailnet)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to retrieve machines: %s", string(body))
	}

	var machines []Machine
	if err := json.Unmarshal(body, &machines); err != nil {
		return nil, err
	}

	return machines, nil
}

func enableExitNode(machineID, token, tailnet string) error {
	apiURL := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s/machines/%s/routes", tailnet, machineID)

	routes := Routes{
		AdvertisedRoutes: []string{"0.0.0.0/0", "::/0"}, // Advertise default routes for exit node
		EnabledRoutes:    []string{"0.0.0.0/0", "::/0"}, // Enable routes
	}

	requestBody, err := json.Marshal(routes)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to enable exit node: %s", string(body))
	}

	return nil
}
