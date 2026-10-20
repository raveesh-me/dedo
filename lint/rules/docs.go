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

// The ZALANDO_* and DEDO_* rule IDs. Each has a heading of the same name in guidance/RULES.md.
const (
	idServiceRulesRequired       = "ZALANDO_SERVICE_RULES_REQUIRED"
	idMethodRulesRequired        = "DEDO_METHOD_RULES_REQUIRED"
	idScopeNameFormat            = "ZALANDO_SCOPE_NAME_FORMAT"
	idErrorReasonFormat          = "DEDO_ERROR_REASON_FORMAT"
	idCacheRequiresNoSideEffects = "ZALANDO_CACHE_REQUIRES_NO_SIDE_EFFECTS"
	idDeprecationConsistent      = "ZALANDO_DEPRECATION_CONSISTENT"
	idReadMethodNoSideEffects    = "DEDO_READ_METHOD_NO_SIDE_EFFECTS"
	idHTTPResponseBody           = "DEDO_HTTP_RESPONSE_BODY"
	idHTTPPathVersion            = "DEDO_HTTP_PATH_VERSION"
	idNoJSONName                 = "DEDO_NO_JSON_NAME"
	idMoneyFieldType             = "ZALANDO_MONEY_FIELD_TYPE"
	idToolRequired               = "DEDO_TOOL_REQUIRED"
	idToolAnnotations            = "DEDO_TOOL_ANNOTATIONS"
	idMessageDescriptionRequired = "DEDO_MESSAGE_DESCRIPTION_REQUIRED"
)

// DocsURL returns the link to a rule's section in guidance/RULES.md.
func DocsURL(ruleID string) string {
	return docsBase + DocsRef + "/guidance/RULES.md#" + strings.ToLower(ruleID)
}

// messagef formats an annotation message and appends the rule's
// documentation link. Editors link the rule ID itself to Buf's own docs and
// plugins cannot change that, so the message carries the link instead.
func messagef(ruleID, format string, args ...any) check.AddAnnotationOption {
	return check.WithMessage(fmt.Sprintf(format, args...) + " (" + DocsURL(ruleID) + ")")
}
