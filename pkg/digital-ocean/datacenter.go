package do

import (
	"context"

	"github.com/digitalocean/godo"
)

func GetDatacenters(token string) ([]string, error) {
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	opt := &godo.ListOptions{
		Page:    1,
		PerPage: 200,
	}

	allRegions, _, err := client.Regions.List(ctx, opt)
	if err != nil {
		return nil, err
	}

	var availRegions []string
	for _, r := range allRegions {
		if r.Available {
			availRegions = append(availRegions, r.Slug)
		}
	}

	return availRegions, nil
}
