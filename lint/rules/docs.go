package rules

import (
	"fmt"
	"strings"

	"buf.build/go/bufplugin/check"
)

// DocsRef is the git ref that rule documentation links point at. Release
// builds set it to their tag with
// -ldflags "-X github.com/raveesh-me/dedo/lint/rules.DocsRef=<tag>",
// so each plugin version links to the rules as they were at that version.
var DocsRef = "main"

const docsBase = "https://github.com/raveesh-me/dedo/blob/"

// The house rule IDs. Each has a heading of the same name in HOUSE_RULES.md.
const (
	idServiceRulesRequired       = "HOUSE_SERVICE_RULES_REQUIRED"
	idMethodRulesRequired        = "HOUSE_METHOD_RULES_REQUIRED"
	idScopeNameFormat            = "HOUSE_SCOPE_NAME_FORMAT"
	idErrorReasonFormat          = "HOUSE_ERROR_REASON_FORMAT"
	idCacheRequiresNoSideEffects = "HOUSE_CACHE_REQUIRES_NO_SIDE_EFFECTS"
	idDeprecationConsistent      = "HOUSE_DEPRECATION_CONSISTENT"
	idReadMethodNoSideEffects    = "HOUSE_READ_METHOD_NO_SIDE_EFFECTS"
	idHTTPResponseBody           = "HOUSE_HTTP_RESPONSE_BODY"
	idHTTPPathVersion            = "HOUSE_HTTP_PATH_VERSION"
	idNoJSONName                 = "HOUSE_NO_JSON_NAME"
	idMoneyFieldType             = "HOUSE_MONEY_FIELD_TYPE"
	idToolRequired               = "HOUSE_TOOL_REQUIRED"
	idToolAnnotations            = "HOUSE_TOOL_ANNOTATIONS"
	idMessageDescriptionRequired = "HOUSE_MESSAGE_DESCRIPTION_REQUIRED"
)

// DocsURL returns the link to a rule's section in HOUSE_RULES.md.
func DocsURL(ruleID string) string {
	return docsBase + DocsRef + "/HOUSE_RULES.md#" + strings.ToLower(ruleID)
}

// messagef formats an annotation message and appends the rule's
// documentation link. Editors link the rule ID itself to Buf's own docs and
// plugins cannot change that, so the message carries the link instead.
func messagef(ruleID, format string, args ...any) check.AddAnnotationOption {
	return check.WithMessage(fmt.Sprintf(format, args...) + " (" + DocsURL(ruleID) + ")")
}
