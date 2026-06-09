package routes

import "testing"

func TestAuditLogFiltersIncludesActorType(t *testing.T) {
	filter, where, params := auditLogFilters("", "", "system", "", "", "", "", "", "")

	if filter != "actor_type = {:actor_type}" {
		t.Fatalf("unexpected filter: %q", filter)
	}
	if where != "actor_type = {:actor_type}" {
		t.Fatalf("unexpected where: %q", where)
	}
	if params["actor_type"] != "system" {
		t.Fatalf("unexpected actor_type param: %#v", params["actor_type"])
	}
}

func TestAuditLogFiltersIncludesDateRange(t *testing.T) {
	filter, where, params := auditLogFilters("2026-06-01", "2026-06-08", "", "", "", "", "", "", "error")

	expectedFilter := "created >= {:from} && created <= {:to} && status = {:status}"
	if filter != expectedFilter {
		t.Fatalf("unexpected filter: %q", filter)
	}

	expectedWhere := "created >= {:from} AND created <= {:to} AND status = {:status}"
	if where != expectedWhere {
		t.Fatalf("unexpected where: %q", where)
	}

	if params["from"] != "2026-06-01" || params["to"] != "2026-06-08" || params["status"] != "error" {
		t.Fatalf("unexpected params: %#v", params)
	}
}

func TestAuditLogFiltersIncludesOrigin(t *testing.T) {
	filter, where, params := auditLogFilters("", "", "", "", "", "", "", "ui", "")

	if filter != "origin = {:origin}" {
		t.Fatalf("unexpected filter: %q", filter)
	}
	if where != "origin = {:origin}" {
		t.Fatalf("unexpected where: %q", where)
	}
	if params["origin"] != "ui" {
		t.Fatalf("unexpected origin param: %#v", params["origin"])
	}
}
