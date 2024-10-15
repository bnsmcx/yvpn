package tailscale

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Device struct {
	Addresses                 []string  `json:"addresses"`
	Authorized                bool      `json:"authorized"`
	BlocksIncomingConnections bool      `json:"blocksIncomingConnections"`
	ClientVersion             string    `json:"clientVersion"`
	Created                   time.Time `json:"created"`
	Expires                   time.Time `json:"expires"`
	Hostname                  string    `json:"hostname"`
	ID                        string    `json:"id"`
	IsExternal                bool      `json:"isExternal"`
	KeyExpiryDisabled         bool      `json:"keyExpiryDisabled"`
	LastSeen                  time.Time `json:"lastSeen"`
	MachineKey                string    `json:"machineKey"`
	Name                      string    `json:"name"`
	NodeID                    string    `json:"nodeId"`
	NodeKey                   string    `json:"nodeKey"`
	Os                        string    `json:"os"`
	TailnetLockError          string    `json:"tailnetLockError"`
	TailnetLockKey            string    `json:"tailnetLockKey"`
	UpdateAvailable           bool      `json:"updateAvailable"`
	User                      string    `json:"user"`
}

type DevicesResponse struct {
	Devices []Device `json:"devices"`
}

type Routes struct {
	AdvertisedRoutes []string `json:"advertisedRoutes"`
	EnabledRoutes    []string `json:"enabledRoutes"`
}

func EnableExit(name, token string) (int, error) {
	for elapsed := 0; elapsed < 360; elapsed++ {
		machines, err := getTailscaleMachines(token)
		if err != nil {
			return elapsed, fmt.Errorf("Error retrieving Tailscale machines: %s", err.Error())
		}

		for _, machine := range machines {
			if strings.Contains(machine.Name, name) {
				if err := enableExitNode(machine.ID, token); err != nil {
					return elapsed, fmt.Errorf("Error enabling exit node: %v", err)
				}
				fmt.Printf("Exit node active after %d seconds\n", elapsed)
				return elapsed, nil
			}
		}

		time.Sleep(time.Second)
	}
	return 360, fmt.Errorf("Exit node not found on tailnet within six minutes.")
}

func getTailscaleMachines(token string) ([]Device, error) {
	apiURL := "https://api.tailscale.com/api/v2/tailnet/-/devices"

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

	var dr DevicesResponse
	if err := json.Unmarshal(body, &dr); err != nil {
		return nil, err
	}

	return dr.Devices, nil
}

func enableExitNode(machineID, token string) error {
	apiURL := fmt.Sprintf("https://api.tailscale.com/api/v2/device/%s/routes", machineID)

	routes, err := getAdvertisedRoutes(machineID, token)

	payload := map[string][]string{
		"routes": routes.AdvertisedRoutes,
	}

	requestBody, err := json.Marshal(payload)
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

func getAdvertisedRoutes(machineID, token string) (Routes, error) {
	url := fmt.Sprintf("https://api.tailscale.com/api/v2/device/%s/routes", machineID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return Routes{}, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Routes{}, err
	}

	defer res.Body.Close()

	var routes Routes
	err = json.NewDecoder(res.Body).Decode(&routes)
	return routes, err
}
