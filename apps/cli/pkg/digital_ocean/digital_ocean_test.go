package digital_ocean

import (
	"reflect"
	"testing"

	"github.com/digitalocean/godo"

	"yvpn/pkg/addons"
)

func TestPickDatacenters(t *testing.T) {
	regions := []godo.Region{
		{Slug: "nyc1", Name: "New York 1", Available: true},
		{Slug: "mem1", Name: "Memphis 1", Available: true},
		{Slug: "lon1", Name: "London 1", Available: false},
		{Slug: "atl1", Name: "Atlanta 1", Available: true},
		{Slug: "noimg", Name: "No Image", Available: true},
	}
	sizes := []godo.Size{
		{Slug: "s-1vcpu-512mb-10gb", Memory: 512, Vcpus: 1, Disk: 10, PriceHourly: 0.006, Available: true,
			Regions: []string{"nyc1", "lon1", "noimg"}},
		{Slug: "s-1vcpu-1gb", Memory: 1024, Vcpus: 1, Disk: 25, PriceHourly: 0.009, Available: true,
			Regions: []string{"nyc1", "lon1"}},
		// mem1 offers neither of the small sizes, only something pricier.
		{Slug: "s-2vcpu-4gb", Memory: 4096, Vcpus: 2, Disk: 80, PriceHourly: 0.036, Available: true,
			Regions: []string{"nyc1", "mem1"}},
		{Slug: "s-1vcpu-2gb", Memory: 2048, Vcpus: 1, Disk: 50, PriceHourly: 0.018, Available: true,
			Regions: []string{"mem1"}},
		// Enough memory for Jellyfin but one core, and cheaper than s-2vcpu-4gb.
		{Slug: "m-1vcpu-8gb", Memory: 8192, Vcpus: 1, Disk: 25, PriceHourly: 0.030, Available: true,
			Regions: []string{"nyc1", "mem1"}},
		// Two cores and 4 GB but too little disk for Jellyfin's videos.
		{Slug: "c-2-4gb-small", Memory: 4096, Vcpus: 2, Disk: 25, PriceHourly: 0.020, Available: true,
			Regions: []string{"nyc1"}},
		// Disk below the image's floor.
		{Slug: "tiny", Memory: 512, Vcpus: 1, Disk: 5, PriceHourly: 0.001, Available: true,
			Regions: []string{"nyc1", "mem1"}},
		// Unavailable, however cheap.
		{Slug: "gone", Memory: 1024, Vcpus: 1, Disk: 25, PriceHourly: 0.002, Available: false,
			Regions: []string{"mem1"}},
		// atl1 only sells GPUs.
		{Slug: "gpu-h100x1-80gb", Memory: 245760, Vcpus: 20, Disk: 720, PriceHourly: 3.39, Available: true,
			Description: "GPU", Regions: []string{"atl1"}},
	}
	img := &godo.Image{MinDiskSize: 7, Regions: []string{"nyc1", "mem1", "lon1", "atl1"}}

	plain, _ := addons.Lookup("")
	got := pickDatacenters(regions, sizes, img, plain.Needs)
	want := []Datacenter{
		{Slug: "mem1", Name: "Memphis 1", Size: "s-1vcpu-2gb", PriceHourly: 0.018},
		{Slug: "nyc1", Name: "New York 1", Size: "s-1vcpu-512mb-10gb", PriceHourly: 0.006},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// An add-on raises the floor: the cheapest size in each region that has the
// cores, memory and disk it needs, and regions with none drop out.
func TestPickDatacentersForAddon(t *testing.T) {
	regions := []godo.Region{
		{Slug: "nyc1", Name: "New York 1", Available: true},
		{Slug: "lon1", Name: "London 1", Available: true},
	}
	sizes := []godo.Size{
		{Slug: "s-1vcpu-512mb-10gb", Memory: 512, Vcpus: 1, Disk: 10, PriceHourly: 0.006, Available: true,
			Regions: []string{"nyc1", "lon1"}},
		{Slug: "m-1vcpu-8gb", Memory: 8192, Vcpus: 1, Disk: 25, PriceHourly: 0.030, Available: true,
			Regions: []string{"nyc1"}},
		{Slug: "c-2-4gb-small", Memory: 4096, Vcpus: 2, Disk: 25, PriceHourly: 0.020, Available: true,
			Regions: []string{"nyc1"}},
		{Slug: "s-2vcpu-4gb", Memory: 4096, Vcpus: 2, Disk: 80, PriceHourly: 0.036, Available: true,
			Regions: []string{"nyc1"}},
		{Slug: "s-4vcpu-8gb", Memory: 8192, Vcpus: 4, Disk: 160, PriceHourly: 0.071, Available: true,
			Regions: []string{"nyc1"}},
	}
	img := &godo.Image{MinDiskSize: 7, Regions: []string{"nyc1", "lon1"}}
	jf, _ := addons.Lookup("jellyfin")

	got := pickDatacenters(regions, sizes, img, jf.Needs)
	want := []Datacenter{{Slug: "nyc1", Name: "New York 1", Size: "s-2vcpu-4gb", PriceHourly: 0.036}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}
