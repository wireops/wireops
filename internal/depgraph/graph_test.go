package depgraph

import (
	"sort"
	"testing"
)

func nodeByID(g *Graph, id string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

func edgesOfType(g *Graph, typ string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Target < out[j].Target
	})
	return out
}

func TestBuildFromComposeDependsOnListForm(t *testing.T) {
	yamlContent := `
services:
  web:
    image: nginx:latest
    depends_on:
      - api
  api:
    image: my/api:1.0
`
	g, err := BuildFromCompose([]byte(yamlContent))
	if err != nil {
		t.Fatalf("BuildFromCompose: %v", err)
	}

	web := nodeByID(g, ServiceNodeID("web"))
	if web == nil || web.Image != "nginx:latest" {
		t.Fatalf("expected web service node with image nginx:latest, got %+v", web)
	}

	edges := edgesOfType(g, EdgeDependsOn)
	if len(edges) != 1 || edges[0].Source != ServiceNodeID("web") || edges[0].Target != ServiceNodeID("api") {
		t.Fatalf("expected web->api depends_on edge, got %+v", edges)
	}
	if edges[0].Label != "" {
		t.Fatalf("expected empty condition label for list-form depends_on, got %q", edges[0].Label)
	}
}

func TestBuildFromComposeDependsOnMapForm(t *testing.T) {
	yamlContent := `
services:
  web:
    image: nginx:latest
    depends_on:
      api:
        condition: service_healthy
  api:
    image: my/api:1.0
`
	g, err := BuildFromCompose([]byte(yamlContent))
	if err != nil {
		t.Fatalf("BuildFromCompose: %v", err)
	}

	edges := edgesOfType(g, EdgeDependsOn)
	if len(edges) != 1 || edges[0].Label != "service_healthy" {
		t.Fatalf("expected depends_on edge with condition service_healthy, got %+v", edges)
	}
}

func TestBuildFromComposeExternalNetwork(t *testing.T) {
	yamlContent := `
services:
  web:
    image: nginx:latest
    networks:
      - proxy
networks:
  proxy:
    external: true
`
	g, err := BuildFromCompose([]byte(yamlContent))
	if err != nil {
		t.Fatalf("BuildFromCompose: %v", err)
	}

	net := nodeByID(g, NetworkNodeID("proxy"))
	if net == nil || !net.External {
		t.Fatalf("expected external proxy network node, got %+v", net)
	}

	edges := edgesOfType(g, EdgeNetwork)
	if len(edges) != 1 || edges[0].Source != ServiceNodeID("web") || edges[0].Target != NetworkNodeID("proxy") {
		t.Fatalf("expected web->proxy network edge, got %+v", edges)
	}
}

func TestBuildFromComposeImplicitDefaultNetwork(t *testing.T) {
	yamlContent := `
services:
  web:
    image: nginx:latest
    networks:
      - default
`
	g, err := BuildFromCompose([]byte(yamlContent))
	if err != nil {
		t.Fatalf("BuildFromCompose: %v", err)
	}

	net := nodeByID(g, NetworkNodeID("default"))
	if net == nil {
		t.Fatalf("expected a synthesized default network node")
	}
	if net.External {
		t.Fatalf("implicit undeclared network should not be marked external")
	}
}

func TestBuildFromComposeExternalNamedVolume(t *testing.T) {
	yamlContent := `
services:
  db:
    image: postgres:16
    volumes:
      - data:/var/lib/postgresql/data
volumes:
  data:
    external: true
`
	g, err := BuildFromCompose([]byte(yamlContent))
	if err != nil {
		t.Fatalf("BuildFromCompose: %v", err)
	}

	vol := nodeByID(g, VolumeNodeID("data"))
	if vol == nil || !vol.External {
		t.Fatalf("expected external data volume node, got %+v", vol)
	}

	edges := edgesOfType(g, EdgeVolume)
	if len(edges) != 1 || edges[0].Target != VolumeNodeID("data") {
		t.Fatalf("expected db->data volume edge, got %+v", edges)
	}
}

func TestBuildFromComposeBindMountExcluded(t *testing.T) {
	yamlContent := `
services:
  db:
    image: postgres:16
    volumes:
      - /host/data:/var/lib/postgresql/data
      - ./relative:/etc/conf
`
	g, err := BuildFromCompose([]byte(yamlContent))
	if err != nil {
		t.Fatalf("BuildFromCompose: %v", err)
	}

	edges := edgesOfType(g, EdgeVolume)
	if len(edges) != 0 {
		t.Fatalf("expected no volume edges for bind mounts, got %+v", edges)
	}
	for _, n := range g.Nodes {
		if n.Kind == KindVolume {
			t.Fatalf("expected no volume nodes for bind-mount-only stack, got %+v", n)
		}
	}
}

func TestBuildFromComposeAnonymousVolumeExcluded(t *testing.T) {
	yamlContent := `
services:
  db:
    image: postgres:16
    volumes:
      - /var/lib/postgresql/data
`
	g, err := BuildFromCompose([]byte(yamlContent))
	if err != nil {
		t.Fatalf("BuildFromCompose: %v", err)
	}

	edges := edgesOfType(g, EdgeVolume)
	if len(edges) != 0 {
		t.Fatalf("expected no volume edges for anonymous volume, got %+v", edges)
	}
}

func TestBuildFromComposeCustomImageSlug(t *testing.T) {
	yamlContent := `
services:
  db:
    image: postgres:16
    labels:
      customization.image.slug: postgresql
  cache:
    image: redis:7
    deploy:
      labels:
        customization.image.slug: redis
  web:
    image: nginx:latest
`
	g, err := BuildFromCompose([]byte(yamlContent))
	if err != nil {
		t.Fatalf("BuildFromCompose: %v", err)
	}

	db := nodeByID(g, ServiceNodeID("db"))
	if db == nil || db.Slug != "postgresql" {
		t.Fatalf("expected db node with slug postgresql, got %+v", db)
	}

	cache := nodeByID(g, ServiceNodeID("cache"))
	if cache == nil || cache.Slug != "redis" {
		t.Fatalf("expected cache node with deploy-label slug redis, got %+v", cache)
	}

	web := nodeByID(g, ServiceNodeID("web"))
	if web == nil || web.Slug != "" {
		t.Fatalf("expected web node with no slug, got %+v", web)
	}
}

func TestBuildFromComposeInvalidYAML(t *testing.T) {
	_, err := BuildFromCompose([]byte("services: [this is not a map"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
