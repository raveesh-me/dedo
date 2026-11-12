// Package ignore holds //dedo:ignore directives, good and bad.
package ignore

//dedo:ignore DEDO_GO_SERVER_INTERCEPTORS a reason
var a = 1

//dedo:ignore DEDO_GO_SERVER_INTERCEPTORS // want `DEDO_GO_IGNORE_REASON: //dedo:ignore DEDO_GO_SERVER_INTERCEPTORS gives no reason`
var b = 2

//dedo:ignore // want `names no rule`
var c = 3

//dedo:ignore not-a-rule because // want `names "not-a-rule", which is not a rule ID`
var d = 4

var e = 5 //dedo:ignore HOUSE_GO_X // want `HOUSE_GO_X gives no reason`

// dedo:ignore with a space is prose, not a directive.
var f = 6

//dedo:ignored is another word.
var g = 7
