package httpapi

// T106: a route-coverage test — an unguarded route fails the build instead
// of only ever showing up as a 3am 403 in dev (requirePerm, T104, already
// denies it at runtime; this catches it at `make test` time). The checking
// logic is split from the walk so a synthetic router can prove the checker
// actually catches a mismatch, not just that it stays quiet against the
// real one.

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

const tenantRoutePrefix = "/v1/tenants/{id}"

// walkTenantRoutes returns every "METHOD suffix" pair chi has registered
// under tenantRoutePrefix, sorted; suffix is the path after the prefix (""
// for the bundle route itself, "/hq/orders" for a leaf). The SSE endpoint
// (/v1/tenants/{id}/events) is deliberately excluded — see permTable's doc
// comment for why it sits outside this table entirely.
func walkTenantRoutes(mux chi.Router) ([]string, error) {
	var registered []string
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == tenantRoutePrefix+"/events" || !strings.HasPrefix(route, tenantRoutePrefix) {
			return nil
		}
		suffix := strings.TrimSuffix(strings.TrimPrefix(route, tenantRoutePrefix), "/")
		registered = append(registered, method+" "+suffix)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(registered)
	return registered, nil
}

func ruleKey(r accessRule) string {
	if len(r.segments) == 0 {
		return r.method + " "
	}
	return r.method + " /" + strings.Join(r.segments, "/")
}

// permTableCoverageErrors compares registered routes against permTable in
// both directions — a registered route with no table entry would be denied
// to everyone, owner included; a table entry matching no registered route
// is dead weight hiding a route rename — and names the offending method and
// pattern in each message, not just a count, so a failure is actionable
// without grepping the whole table by hand.
func permTableCoverageErrors(registered []string) []string {
	tableKeys := make(map[string]bool, len(permTable))
	for _, r := range permTable {
		tableKeys[ruleKey(r)] = true
	}

	var errs []string
	registeredSet := make(map[string]bool, len(registered))
	for _, key := range registered {
		registeredSet[key] = true
		if !tableKeys[key] {
			errs = append(errs, `route "`+key+`" is registered but has no permTable entry — it would be denied to everyone, owner included`)
		}
	}
	for _, r := range permTable {
		key := ruleKey(r)
		if !registeredSet[key] {
			errs = append(errs, `permTable entry "`+key+`" does not match any registered route`)
		}
	}
	return errs
}

// TestPermTableCoversEveryRegisteredRoute walks the real router and checks
// every /v1/tenants/{id}/* route has exactly one permTable entry, and that
// permTable has no stale entries pointing at a route that no longer exists.
// Registering a new route without a permission is a build-time-visible test
// failure here, not just a runtime 403 in dev.
func TestPermTableCoversEveryRegisteredRoute(t *testing.T) {
	s := &Server{}
	mux, ok := s.Router().(chi.Router)
	if !ok {
		t.Fatalf("router is not a chi.Router")
	}
	registered, err := walkTenantRoutes(mux)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for _, msg := range permTableCoverageErrors(registered) {
		t.Error(msg)
	}
}

// TestPermTableCoverageCatchesUnmappedRoute proves the checker above isn't
// vacuously green: a route registered on a router with no matching
// permTable entry must be reported, and the message must name the
// offending method + pattern rather than just a count — a developer
// chasing a bare count would have to grep the whole table by hand.
func TestPermTableCoverageCatchesUnmappedRoute(t *testing.T) {
	r := chi.NewRouter()
	r.Route(tenantRoutePrefix, func(r chi.Router) {
		r.Get("/", func(http.ResponseWriter, *http.Request) {}) // matches permTable's real memberRule(GET, "")
		r.Get("/no-such-route", func(http.ResponseWriter, *http.Request) {})
	})

	registered, err := walkTenantRoutes(r)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	const want = `route "GET /no-such-route" is registered but has no permTable entry — it would be denied to everyone, owner included`
	var found bool
	for _, msg := range permTableCoverageErrors(registered) {
		if msg == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("coverage errors did not report the unmapped route; want to contain %q", want)
	}
}
