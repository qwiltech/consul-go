package consul

import (
	"sort"
	"testing"
)

var balancers = []struct {
	name string
	new  func() Balancer
}{
	{
		name: "RoundRobin",
		new:  func() Balancer { return &RoundRobin{} },
	},

	{
		name: "PreferTags",
		new:  func() Balancer { return PreferTags{"us-west-2a"} },
	},

	{
		name: "PreferTags+RoundRobin",
		new: func() Balancer {
			return MultiBalancer(PreferTags{"us-west-2a"}, &RoundRobin{})
		},
	},

	{
		name: "Shuffler",
		new:  func() Balancer { return &Shuffler{} },
	},

	{
		name: "WeightedShufflerOnRTT",
		new: func() Balancer {
			return &WeightedShuffler{
				WeightOf: func(e Endpoint) float64 { return float64(e.RTT) },
			}
		},
	},
}

func TestBalancer(t *testing.T) {
	for _, balancer := range balancers {
		t.Run(balancer.name, func(t *testing.T) {
			testBalancer(t, &LoadBalancer{New: balancer.new})
		})
	}
}

func testBalancer(t *testing.T, balancer Balancer) {
	const endpointsCount = 30
	const draws = 200

	type counter struct {
		index int
		value int
	}

	base := generateTestEndpoints(endpointsCount)
	counters := make([]counter, endpointsCount)
	for i := range counters {
		counters[i].index = i
	}

	endpoints := make([]Endpoint, endpointsCount)
	for i := 0; i != draws; i++ {
		copy(endpoints, base)
		endpoints = balancer.Balance("test-service", endpoints)

		for i := range base {
			if endpoints[0].ID == base[i].ID {
				counters[i].value++
				break
			}
		}
	}

	sort.Slice(counters, func(i int, j int) bool {
		return counters[i].value > counters[j].value
	})

	for _, c := range counters {
		endpoint := base[c.index]
		t.Logf("ID = %  s, RTT = % 5s, Tags = %s: % 3d\t(%g%%)", endpoint.ID, endpoint.RTT, endpoint.Tags, c.value, float64(c.value)*100.0/draws)
	}
}

func BenchmarkBalancer(b *testing.B) {
	for _, balancer := range balancers {
		b.Run(balancer.name, func(b *testing.B) {
			benchmarkBalancer(b, balancer.new())
		})
	}
}

func benchmarkBalancer(b *testing.B, balancer Balancer) {
	endpoints := generateTestEndpoints(300)

	for i := 0; i != b.N; i++ {
		balancer.Balance("service-A", endpoints)
	}
}
