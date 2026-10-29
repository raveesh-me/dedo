package restdoc

import "testing"

// A tag, the group a reference shows a service's operations under, reads as
// words: the service name without the Service suffix buf requires.
func TestTagName(t *testing.T) {
	for _, c := range []struct{ service, title, want string }{
		{"CatalogEntryService", "", "Catalog Entry"},
		{"ItemsService", "", "Items"},
		{"ItemBffService", "", "Item Bff"},
		{"ItemBffService", "Item BFF", "Item BFF"},
		{"RecurringPlanSyncService", "", "Recurring Plan Sync"},
		{"HTTPProxyService", "", "HTTP Proxy"},
		{"V2MigrationService", "", "V2 Migration"},
		{"Service", "", "Service"},
		{"Library", "", "Library"},
	} {
		if got := tagName(c.service, c.title); got != c.want {
			t.Errorf("tagName(%q, %q) = %q, want %q", c.service, c.title, got, c.want)
		}
	}
}
