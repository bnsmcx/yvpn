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

type KeyDevicesCreate struct {
	Reusable      bool     `json:"reusable"`
	Ephemeral     bool     `json:"ephemeral"`
	Preauthorized bool     `json:"preauthorized"`
	Tags          []string `json:"tags"`
}

type KeyDevices struct {
	Create KeyDevicesCreate `json:"create"`
}

type KeyCapabilities struct {
	Devices KeyDevices `json:"devices"`
}

type KeyRequest struct {
	Capabilities  KeyCapabilities `json:"capabilities"`
	ExpirySeconds int             `json:"expirySeconds"`
	Description   string          `json:"description"`
}

type KeyResponse struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

func GetAuthKey(token string) (key, id string, err error) {
	url := "https://api.tailscale.com/api/v2/tailnet/-/keys?all=true"

	payload := KeyRequest{
		Capabilities: KeyCapabilities{
			Devices: KeyDevices{
				Create: KeyDevicesCreate{
					Reusable:      false,
					Ephemeral:     true,
					Preauthorized: true,
					Tags:          []string{},
				},
			},
		},
		ExpirySeconds: 3600,
		Description:   "yVPN endpoint auth key",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return "", "", err
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		err = fmt.Errorf("request failed with status: %s", res.Status)
		return "", "", err
	}

	var kr KeyResponse
	err = json.NewDecoder(res.Body).Decode(&kr)
	if err != nil {
		return "", "", err
	}

	return kr.Key, kr.ID, nil
}

func DeleteAuthKey(token, id string) error {
	url := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/-/keys/%s", id)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return fmt.Errorf("request failed with status: %s", res.Status)
	}

	return nil
}

func EnableExit(name, token string) (int, error) {
	for elapsed := 0; elapsed < 3600; elapsed++ {
		machines, err := getTailscaleMachines(token)
		if err != nil {
			return elapsed, fmt.Errorf("Error retrieving Tailscale machines: %s", err.Error())
		}

		for _, machine := range machines {
			if strings.Contains(machine.Name, name) {
				if err := enableExitNode(machine.ID, token); err != nil {
					return elapsed, fmt.Errorf("Error enabling exit node: %v", err)
				}
				return elapsed, nil
			}
		}

		time.Sleep(time.Second)
	}
	return 3600, fmt.Errorf("Exit node not found on tailnet within sixty minutes.")
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
