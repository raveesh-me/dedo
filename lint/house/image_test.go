package house_test

import "testing"

// image holds every file, service and method buf passes, the module's own
// and its imports', keyed by path or full name, each with the variables a
// rule on it reads and imported set for an import's.
func TestImageShape(t *testing.T) {
	const (
		charge   = "image.methods['acme.pay.v1.PaymentService.Charge']"
		checkout = "image.methods['acme.shop.v1.CartService.Checkout']"
	)
	got := fixtureFindings(t, "testdata/image", false,
		rule("HOUSE_METHODS", "file", charge+".imported && !"+checkout+".imported && "+
			checkout+".service == 'acme.shop.v1.CartService' && "+checkout+".scopes == ['shop/carts.write'] && "+
			checkout+".options['acme.auth.v1.method'].calls == ['acme.pay.v1.PaymentService.Charge'] && "+
			checkout+".service_options['acme.auth.v1.service'].root == 'shop'"),
		rule("HOUSE_SERVICES", "file", "image.services['acme.pay.v1.PaymentService'].imported && "+
			"image.services['acme.pay.v1.PaymentService'].options['acme.auth.v1.service'].root == 'pay' && "+
			"image.services['acme.shop.v1.CartService'].methods.size() == 4"),
		rule("HOUSE_FILES", "file", "image.files['acme/pay/v1/pay.proto'].imported && !image.files['api.proto'].imported && "+
			"image.files['api.proto'].options['acme.auth.v1.file'].permissions.size() == 4"),
		rule("HOUSE_EVERY_KIND", "message", "size(image.methods) == 6"),
		rule("HOUSE_NOT_IN_IMAGE", "method", "!('acme.pay.v1.PaymentService.Void' in image.methods)"),
		// A control: it reports on each of the four methods, so the rules run.
		rule("HOUSE_CONTROL", "method", "!(full_name in image.methods)"),
	)
	assertFindings(t, got, map[string]int{"HOUSE_CONTROL": 4})
}

// A breaking rule reads the current version's image; against has none. The
// file's permission grants two scopes now and one in the earlier version, so
// a rule reading the earlier version's image would report.
func TestImageBreaking(t *testing.T) {
	const grants = "image.files['api.proto'].options['acme.auth.v1.file'].permissions[0].grants.size()"
	now := rule("HOUSE_IMAGE_IS_NOW", "file", grants+" == 2 && against.options['acme.auth.v1.file'].permissions[0].grants.size() == 1 && !('image' in against)")
	before := rule("HOUSE_IMAGE_IS_AGAINST", "file", grants+" == 1")
	now.Breaking, before.Breaking = true, true
	assertFindings(t, optionFindings(t, true, now, before), map[string]int{"HOUSE_IMAGE_IS_AGAINST": 1})
}

// image lets a rule check across files: here, that permissions grant only
// declared scopes and the scopes of the methods those call, and that calls
// resolve.
func TestImageAcrossFiles(t *testing.T) {
	const (
		perms = "options['acme.auth.v1.file'].permissions"
		calls = "options['acme.auth.v1.method'].calls"
	)
	got := fixtureFindings(t, "testdata/image", false,
		rule("HOUSE_GRANTS_DECLARED", "file", "!('acme.auth.v1.file' in options) || "+perms+".all(p, p.grants.all(g, "+
			"image.methods.exists(k, cel.bind(m, image.methods[k], !m.imported && (g in m.scopes || "+
			"('acme.auth.v1.method' in m.options && m."+calls+".exists(c, c in image.methods && g in image.methods[c].scopes)))))))"),
		rule("HOUSE_GRANTS_COVER_CALLS", "method", "!('acme.auth.v1.method' in options) || scopes.size() != 1 || "+
			"image.files.all(f, cel.bind(file, image.files[f], file.imported || !('acme.auth.v1.file' in file.options) || "+
			"file."+perms+".all(p, !(scopes[0] in p.grants) || "+calls+".all(c, "+
			"!(c in image.methods) || image.methods[c].scopes.size() != 1 || image.methods[c].scopes[0] in p.grants))))"),
		rule("HOUSE_CALLS_RESOLVED", "method", "!('acme.auth.v1.method' in options) || "+
			calls+".all(c, c in image.methods && image.methods[c].scopes.size() == 1)"),
	)
	assertFindings(t, got, map[string]int{
		"HOUSE_GRANTS_DECLARED":    1, // shop/carts.legacy grants shop/baskets.read
		"HOUSE_GRANTS_COVER_CALLS": 1, // shop/carts.edit grants Checkout's scope, not Charge's
		"HOUSE_CALLS_RESOLVED":     2, // Abandon calls Void, not in the image; Reconcile calls Audit, with two scopes
	})
}
