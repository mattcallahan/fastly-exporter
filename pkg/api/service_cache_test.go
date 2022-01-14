package api_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/peterbourgon/fastly-exporter/pkg/api"
	"github.com/peterbourgon/fastly-exporter/pkg/filter"
)

func TestServiceCache(t *testing.T) {
	t.Parallel()

	var (
		s1 = api.Service{ID: "AbcDef123ghiJKlmnOPsq", Name: "My first service", Version: 5}
		s2 = api.Service{ID: "XXXXXXXXXXXXXXXXXXXXXX", Name: "Dummy service", Version: 1}
	)

	for _, testcase := range []struct {
		name string
		conf api.ServiceCacheConfig
		want []api.Service
	}{
		{
			name: "no options",
			want: []api.Service{s1, s2},
		},
		{
			name: "allowlist both",
			conf: api.ServiceCacheConfig{IDFilter: api.StringSetWith(s1.ID, s2.ID, "additional service ID")},
			want: []api.Service{s1, s2},
		},
		{
			name: "allowlist one",
			conf: api.ServiceCacheConfig{IDFilter: api.StringSetWith(s1.ID)},
			want: []api.Service{s1},
		},
		{
			name: "allowlist none",
			conf: api.ServiceCacheConfig{IDFilter: api.StringSetWith("nonexistent")},
			want: []api.Service{},
		},
		{
			name: "exact name include match",
			conf: api.ServiceCacheConfig{NameFilter: filterAllowlist(`^` + s1.Name + `$`)},
			want: []api.Service{s1},
		},
		{
			name: "partial name include match",
			conf: api.ServiceCacheConfig{NameFilter: filterAllowlist(`mmy`)},
			want: []api.Service{s2},
		},
		{
			name: "generous name include match",
			conf: api.ServiceCacheConfig{NameFilter: filterAllowlist(`.*e.*`)},
			want: []api.Service{s1, s2},
		},
		{
			name: "no name include match",
			conf: api.ServiceCacheConfig{NameFilter: filterAllowlist(`not found`)},
			want: []api.Service{},
		},
		{
			name: "exact name exclude match",
			conf: api.ServiceCacheConfig{NameFilter: filterBlocklist(`^` + s1.Name + `$`)},
			want: []api.Service{s2},
		},
		{
			name: "partial name exclude match",
			conf: api.ServiceCacheConfig{NameFilter: filterBlocklist(`mmy`)},
			want: []api.Service{s1},
		},
		{
			name: "generous name exclude match",
			conf: api.ServiceCacheConfig{NameFilter: filterBlocklist(`.*e.*`)},
			want: []api.Service{},
		},
		{
			name: "no name exclude match",
			conf: api.ServiceCacheConfig{NameFilter: filterBlocklist(`not found`)},
			want: []api.Service{s1, s2},
		},
		{
			name: "name exclude and include",
			conf: api.ServiceCacheConfig{NameFilter: filterAllowlistBlocklist(`.*e.*`, `mmy`)},
			want: []api.Service{s1},
		},
		{
			name: "single shard",
			conf: api.ServiceCacheConfig{ShardFilter: api.Shard{1, 1}},
			want: []api.Service{s1, s2},
		},
		{
			name: "shard n0 m3",
			conf: api.ServiceCacheConfig{ShardFilter: api.Shard{1, 3}},
			want: []api.Service{s1}, // verified experimentally
		},
		{
			name: "shard n1 m3",
			conf: api.ServiceCacheConfig{ShardFilter: api.Shard{2, 3}},
			want: []api.Service{s2}, // verified experimentally
		},
		{
			name: "shard n2 m3",
			conf: api.ServiceCacheConfig{ShardFilter: api.Shard{3, 3}},
			want: []api.Service{}, // verified experimentally
		},
		{
			name: "shard and service ID passing",
			conf: api.ServiceCacheConfig{ShardFilter: api.Shard{1, 3}, IDFilter: api.StringSetWith(s1.ID)},
			want: []api.Service{s1},
		},
		{
			name: "shard and service ID failing",
			conf: api.ServiceCacheConfig{ShardFilter: api.Shard{2, 3}, IDFilter: api.StringSetWith(s1.ID)},
			want: []api.Service{},
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			testcase.conf.Client = fixedResponseClient{code: 200, response: serviceResponseLarge}
			cache := api.NewServiceCache(testcase.conf)
			ctx := context.Background()
			if err := cache.Refresh(ctx); err != nil {
				t.Fatal(err)
			}

			var (
				serviceIDs = cache.ServiceIDs()
				services   = make([]api.Service, len(serviceIDs))
			)
			for i, id := range serviceIDs {
				name, version, _ := cache.Metadata(id)
				services[i] = api.Service{ID: id, Name: name, Version: version}
			}

			if want, have := testcase.want, services; !cmp.Equal(want, have) {
				t.Fatal(cmp.Diff(want, have))
			}
		})
	}
}

func TestServiceCachePagination(t *testing.T) {
	t.Parallel()

	responses := []string{
		`[
			{ "version": 6, "name": "Service 1/1", "id": "c9407d61ae888d" },
			{ "version": 1, "name": "Service 1/2", "id": "cb32a38adf2e00" },
			{ "version": 6, "name": "Service 1/3", "id": "82de5396a46629" },
			{ "version": 2, "name": "Service 1/4", "id": "4200f01763cff9" }
		]`,
		`[
			{ "version": 7, "name": "Service 2/1", "id": "ce2976ac5a3e24" },
			{ "version": 3, "name": "Service 2/2", "id": "e1c2f1aa5fc341" }
		]`,
		`[
			{ "version": 7, "name": "Service 3/1", "id": "65544b504189bf" },
			{ "version": 5, "name": "Service 3/2", "id": "686ec4e72a836a" }
		]`,
	}

	var (
		ctx    = context.Background()
		client = paginatedResponseClient{responses}
		cache  = api.NewServiceCache(api.ServiceCacheConfig{Client: client})
	)

	if err := cache.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	if want, have := []string{
		"4200f01763cff9", "65544b504189bf", "686ec4e72a836a", "82de5396a46629",
		"c9407d61ae888d", "cb32a38adf2e00", "ce2976ac5a3e24", "e1c2f1aa5fc341",
	}, cache.ServiceIDs(); !cmp.Equal(want, have) {
		t.Fatal(cmp.Diff(want, have))
	}
}

func TestParseShard(t *testing.T) {
	t.Parallel()

	for _, testcase := range []struct {
		input string
		err   bool
		want  api.Shard
	}{
		{
			input: "",
			err:   true,
		},
		{
			input: "123",
			err:   true,
		},
		{
			input: "0/2",
			err:   true,
		},
		{
			input: "1/2",
			want:  api.Shard{N: 1, M: 2},
		},
		{
			input: "2/2",
			want:  api.Shard{N: 2, M: 2},
		},
		{
			input: " 2 / 2 ",
			want:  api.Shard{N: 2, M: 2},
		},
		{
			input: "3/2",
			err:   true,
		},
	} {
		t.Run(testcase.input, func(t *testing.T) {
			want := testcase.want
			have, err := api.ParseShard(testcase.input)
			switch {
			case testcase.err && err == nil:
				t.Errorf("want error, have none")
			case !testcase.err && want != have:
				t.Errorf("want %+v, have %+v", want, have)
			}
		})
	}
}

func filterAllowlist(a string) (f filter.Filter) {
	f.Allow(a)
	return f
}

func filterBlocklist(b string) (f filter.Filter) {
	f.Block(b)
	return f
}

func filterAllowlistBlocklist(a, b string) (f filter.Filter) {
	f.Allow(a)
	f.Block(b)
	return f
}

const serviceResponseLarge = `[
	{
		"version": 5,
		"name": "My first service",
		"created_at": "2018-07-26T06:13:51Z",
		"versions": [
			{
				"testing": false,
				"locked": true,
				"number": 1,
				"active": false,
				"service_id": "AbcDef123ghiJKlmnOPsq",
				"staging": false,
				"created_at": "2018-07-26T06:13:51Z",
				"deleted_at": null,
				"comment": "",
				"updated_at": "2018-07-26T06:17:27Z",
				"deployed": false
			},
			{
				"testing": false,
				"locked": true,
				"number": 2,
				"active": false,
				"service_id": "AbcDef123ghiJKlmnOPsq",
				"staging": false,
				"created_at": "2018-07-26T06:15:47Z",
				"deleted_at": null,
				"comment": "",
				"updated_at": "2018-07-26T20:30:44Z",
				"deployed": false
			},
			{
				"testing": false,
				"locked": true,
				"number": 3,
				"active": false,
				"service_id": "AbcDef123ghiJKlmnOPsq",
				"staging": false,
				"created_at": "2018-07-26T20:28:26Z",
				"deleted_at": null,
				"comment": "",
				"updated_at": "2018-07-26T20:48:40Z",
				"deployed": false
			},
			{
				"testing": false,
				"locked": true,
				"number": 4,
				"active": false,
				"service_id": "AbcDef123ghiJKlmnOPsq",
				"staging": false,
				"created_at": "2018-07-26T20:47:58Z",
				"deleted_at": null,
				"comment": "",
				"updated_at": "2018-07-26T21:35:32Z",
				"deployed": false
			},
			{
				"testing": false,
				"locked": true,
				"number": 5,
				"active": true,
				"service_id": "AbcDef123ghiJKlmnOPsq",
				"staging": false,
				"created_at": "2018-07-26T21:35:23Z",
				"deleted_at": null,
				"comment": "",
				"updated_at": "2018-07-26T21:35:33Z",
				"deployed": false
			},
			{
				"testing": false,
				"locked": false,
				"number": 6,
				"active": false,
				"service_id": "AbcDef123ghiJKlmnOPsq",
				"staging": false,
				"created_at": "2018-09-28T04:02:22Z",
				"deleted_at": null,
				"comment": "",
				"updated_at": "2018-09-28T04:05:33Z",
				"deployed": false
			}
		],
		"comment": "",
		"customer_id": "1a2a3a4azzzzzzzzzzzzzz",
		"updated_at": "2018-10-24T06:31:41Z",
		"id": "AbcDef123ghiJKlmnOPsq"
	},
	{
		"version": 1,
		"name": "Dummy service",
		"created_at": "2018-09-20T16:42:20Z",
		"versions": [
			{
				"testing": false,
				"locked": true,
				"number": 1,
				"active": true,
				"service_id": "XXXXXXXXXXXXXXXXXXXXXX",
				"staging": false,
				"created_at": "2018-09-20T16:42:20Z",
				"deleted_at": null,
				"comment": "",
				"updated_at": "2018-09-20T16:42:21Z",
				"deployed": false
			}
		],
		"comment": "",
		"customer_id": "1a2a3a4azzzzzzzzzzzzzz",
		"updated_at": "2018-09-20T16:42:20Z",
		"id": "XXXXXXXXXXXXXXXXXXXXXX"
	}
]`
