// Package updates resolves app versions against the Velopack feed's
// changelogs — the version → publish-date source of truth for update
// entitlement (desktop/tasks/spec-app-updates.md).
package updates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Resolver looks up release publish dates from the on-disk update feed
// (updatesDir/{channel}/{rid}/changelog.{channel}.json), searching the union
// of every channel. A nil Resolver, a missing feed or a malformed changelog
// all report "unknown" — callers must fail open (only a known, post-window
// version may ever be refused).
type Resolver struct {
	dir string
}

// NewResolver builds a Resolver over the feed root. Empty dir yields nil,
// which disables every lookup.
func NewResolver(dir string) *Resolver {
	if dir == "" {
		return nil
	}
	return &Resolver{dir: dir}
}

type entry struct {
	Version        string    `json:"version"`
	PublishedAtUtc time.Time `json:"publishedAtUtc"`
}

// PublishDate returns the publish date of version, if any changelog knows it.
func (r *Resolver) PublishDate(version string) (time.Time, bool) {
	if r == nil {
		return time.Time{}, false
	}
	for _, e := range r.entries() {
		if e.Version == version {
			return e.PublishedAtUtc, true
		}
	}
	return time.Time{}, false
}

// MaxEntitledVersion returns the latest-published version whose publish date
// is not after until — the "reinstall this or older" hint in the
// version_not_entitled refusal.
func (r *Resolver) MaxEntitledVersion(until time.Time) (string, bool) {
	if r == nil {
		return "", false
	}
	var best entry
	found := false
	for _, e := range r.entries() {
		if e.PublishedAtUtc.After(until) {
			continue
		}
		if !found || e.PublishedAtUtc.After(best.PublishedAtUtc) {
			best, found = e, true
		}
	}
	return best.Version, found
}

// entries reads every channel changelog fresh on each call — the files are a
// few KB and bind/validate traffic is low, so no cache invalidation to get
// wrong when the release script rewrites them.
func (r *Resolver) entries() []entry {
	matches, _ := filepath.Glob(filepath.Join(r.dir, "*", "*", "changelog.*.json"))
	var out []entry
	for _, m := range matches {
		raw, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var es []entry
		if json.Unmarshal(raw, &es) != nil {
			continue
		}
		out = append(out, es...)
	}
	return out
}
