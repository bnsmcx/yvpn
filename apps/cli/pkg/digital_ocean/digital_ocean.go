package digital_ocean

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/digitalocean/godo"

	"yvpn/pkg/addons"
)

// Tag marks every droplet yVPN made; nothing else is ever listed or deleted.
const Tag = "yVPN"

type ExitNode struct {
	Name  string
	ID    int
	Addon string `json:",omitempty"` // catalog ID from its tags; "" for a plain exit node
}

func FetchExitNodes(token string) (nodes []ExitNode, err error) {
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	opt := &godo.ListOptions{
		Page:    1,
		PerPage: 200,
	}

	droplets, _, err := client.Droplets.ListByTag(ctx, Tag, opt)
	if err != nil {
		return nil, err
	}

	for _, d := range droplets {
		node := ExitNode{
			Name:  d.Name,
			ID:    d.ID,
			Addon: addons.FromTags(d.Tags),
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// image is what every node boots. Not every region carries it, and it sets a
// floor on disk size.
const image = "ubuntu-24-04-x64"

// Datacenter is a region a node can be created in, with the cheapest size
// there that can run one.
type Datacenter struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Size        string  `json:"size"`
	PriceHourly float64 `json:"price_hourly"`
}

// usable reports whether a size can run a node: big enough for what the node
// will run (and for the image), and a plain CPU droplet. GPU droplets need
// their own images and cost dollars an hour.
func usable(s godo.Size, minDisk int, needs addons.Needs) bool {
	return s.Available &&
		s.Memory >= needs.MemoryMB &&
		s.Vcpus >= needs.VCPUs &&
		s.Disk >= max(minDisk, needs.DiskGB) &&
		!strings.HasPrefix(s.Slug, "gpu-") &&
		!strings.Contains(strings.ToUpper(s.Description), "GPU")
}

// FetchDatacenters lists every region a node running addon ("" for a plain
// exit node) can be created in, sorted by slug, each with the cheapest size
// there that is big enough for it. Regions without one (no such size, or no
// Ubuntu image) are left out rather than failing later at create time.
func FetchDatacenters(token, addon string) ([]Datacenter, error) {
	a, ok := addons.Lookup(addon)
	if !ok {
		return nil, fmt.Errorf("no add-on called %q; there is %s", addon, strings.Join(addons.IDs(), ", "))
	}
	client := godo.NewFromToken(token)
	ctx := context.TODO()
	opts := &godo.ListOptions{Page: 1, PerPage: 200}

	regions, _, err := client.Regions.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	sizes, _, err := client.Sizes.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	img, _, err := client.Images.GetBySlug(ctx, image)
	if err != nil {
		return nil, err
	}

	return pickDatacenters(regions, sizes, img, a.Needs), nil
}

// pickDatacenters is FetchDatacenters without the API calls.
func pickDatacenters(regions []godo.Region, sizes []godo.Size, img *godo.Image, needs addons.Needs) []Datacenter {
	var datacenters []Datacenter
	for _, r := range regions {
		if !r.Available || !slices.Contains(img.Regions, r.Slug) {
			continue
		}
		var best *godo.Size
		for i, s := range sizes {
			if !usable(s, img.MinDiskSize, needs) || !slices.Contains(s.Regions, r.Slug) {
				continue
			}
			if best == nil || s.PriceHourly < best.PriceHourly {
				best = &sizes[i]
			}
		}
		if best == nil {
			continue
		}
		datacenters = append(datacenters, Datacenter{
			Slug:        r.Slug,
			Name:        r.Name,
			Size:        best.Slug,
			PriceHourly: best.PriceHourly,
		})
	}

	slices.SortFunc(datacenters, func(a, b Datacenter) int {
		return strings.Compare(a.Slug, b.Slug)
	})

	return datacenters
}

// Options is what a node is created as.
type Options struct {
	Addon   string // catalog ID, or "" for a plain exit node
	Exit    bool   // advertise it as an exit node
	Version string // the yVPN release its add-on agent is downloaded from
}

// NodeName is what a node created in datacenter now is called. The name is
// fixed before the droplet exists because the node's secrets derive from it.
func NodeName(datacenter string, now time.Time) string {
	return fmt.Sprintf("%s-yvpn-%d", datacenter, now.Unix())
}

func Create(token, tailscaleAuth, datacenter string, opts Options) (string, int, error) {
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	if opts.Addon == "" && !opts.Exit {
		return "", 0, fmt.Errorf("a node with no add-on has to be an exit node")
	}
	datacenters, err := FetchDatacenters(token, opts.Addon)
	if err != nil {
		return "", 0, err
	}
	i := slices.IndexFunc(datacenters, func(d Datacenter) bool { return d.Slug == datacenter })
	if i < 0 {
		return "", 0, fmt.Errorf("can't create this node in %q: no such region, or it has no droplet size that can run it", datacenter)
	}
	size := datacenters[i].Size

	name := NodeName(datacenter, time.Now())
	params := addons.Params{AuthKey: tailscaleAuth, Exit: opts.Exit, Addon: opts.Addon, Version: opts.Version}
	tags := []string{Tag}
	if opts.Addon != "" {
		params.NodeConfig = addons.NodeConfig(opts.Addon, addons.Derive(token, name))
		tags = append(tags, addons.TagPrefix+opts.Addon)
	}

	createRequest := &godo.DropletCreateRequest{
		Tags:   tags,
		Name:   name,
		Region: datacenter,
		Size:   size,
		Image: godo.DropletCreateImage{
			Slug: image,
		},
		// The same script the web app writes, held to it by the parity tests
		// in pkg/addons.
		UserData: addons.CloudInit(params),
	}

	droplet, _, err := client.Droplets.Create(ctx, createRequest)
	if err != nil {
		return "", 0, err
	}
	return createRequest.Name, droplet.ID, nil
}

func Delete(token string, id int) error {
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	_, err := client.Droplets.Delete(ctx, id)
	return err
}
