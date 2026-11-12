// Package app builds servers, some of them without dedo's interceptors.
package app

import (
	"connectrpc.com/connect/v2"

	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/interceptor"
)

func auth(next connect.ServerFunc) connect.ServerFunc { return next }

func validate() connect.ServerInterceptor { return auth }

// newGuard wraps interceptor.New: a call to it is the policy interceptor.
func newGuard() (connect.ServerInterceptor, error) { // want newGuard:"returns interceptor policy"
	guard, err := interceptor.New(interceptor.Config{})
	if err != nil {
		return nil, err
	}
	return guard, nil
}

// direct returns interceptor.New's results as they are.
func direct() (connect.ServerInterceptor, error) { // want direct:"returns interceptor policy"
	return interceptor.New(interceptor.Config{})
}

// either may return anything.
func either(ok bool) connect.ServerInterceptor { // want either:"returns interceptor ?"
	if ok {
		return flowid.Interceptor()
	}
	return validate()
}

func compliant() *connect.Server {
	guard, err := interceptor.New(interceptor.Config{})
	if err != nil {
		return nil
	}
	return connect.NewServer(flowid.Interceptor(), auth, guard, validate())
}

func throughHelpers() {
	guard, _ := newGuard()
	connect.NewServer(flowid.Interceptor(), guard)
	other, _ := direct()
	connect.NewServer(flowid.Interceptor(), other)
}

func noPolicy() {
	connect.NewServer(flowid.Interceptor(), auth) // want `DEDO_GO_SERVER_INTERCEPTORS: connect.NewServer has no policy interceptor`
}

func noFlowID() {
	guard, _ := interceptor.New(interceptor.Config{})
	connect.NewServer(auth, guard) // want `DEDO_GO_SERVER_INTERCEPTORS: connect.NewServer has no flowid.Interceptor\(\)`
}

func outOfOrder() {
	guard, _ := interceptor.New(interceptor.Config{})
	connect.NewServer(guard, flowid.Interceptor()) // want `runs the policy interceptor before flowid.Interceptor\(\)`
}

func none() {
	connect.NewServer() // want `has no flowid.Interceptor\(\)`
}

func spread() {
	guard, _ := interceptor.New(interceptor.Config{})
	all := []connect.ServerInterceptor{flowid.Interceptor(), guard}
	connect.NewServer(all...)
	some := []connect.ServerInterceptor{flowid.Interceptor()}
	connect.NewServer(some...) // want `has no policy interceptor`
}

func inClosure() func() {
	return func() {
		connect.NewServer(flowid.Interceptor(), validate()) // want `has no policy interceptor`
	}
}

// Unresolvable arguments might be either interceptor: no finding.
func unresolved(param connect.ServerInterceptor, list []connect.ServerInterceptor) {
	connect.NewServer(flowid.Interceptor(), param)
	connect.NewServer(list...)
	connect.NewServer(either(true))
	changed := flowid.Interceptor()
	changed = validate()
	connect.NewServer(changed, auth)
	var field struct{ ic connect.ServerInterceptor }
	connect.NewServer(flowid.Interceptor(), field.ic)
}

// wrap and chain take dedo's interceptors in: what they return may be one.
func wrap(ic connect.ServerInterceptor) connect.ServerInterceptor { return ic } // want wrap:"returns interceptor ?"

func chain(ics ...connect.ServerInterceptor) connect.ServerInterceptor { return ics[0] } // want chain:"returns interceptor ?"

// pick is generic, and always returns the flow ID interceptor.
func pick[T any](T) connect.ServerInterceptor { return flowid.Interceptor() } // want pick:"returns interceptor flowid"

func wrapped(c bool, ic connect.ServerInterceptor) {
	guard, _ := interceptor.New(interceptor.Config{})
	connect.NewServer(wrap(flowid.Interceptor()), guard)
	connect.NewServer(chain(flowid.Interceptor(), guard))
	connect.NewServer(flowid.Interceptor(), func(next connect.ServerFunc) connect.ServerFunc { return guard(next) })
	connect.NewServer(pick(1), guard)
	connect.NewServer(pick(1)) // want `has no policy interceptor`
	if c {
		ic = validate()
	}
	connect.NewServer(ic, guard)
	for _, each := range []connect.ServerInterceptor{guard} {
		connect.NewServer(flowid.Interceptor(), each)
	}
}

func suppressed() {
	//dedo:ignore DEDO_GO_SERVER_INTERCEPTORS this server only serves the readiness probe
	connect.NewServer(flowid.Interceptor())
	connect.NewServer(flowid.Interceptor()) //dedo:ignore DEDO_GO_SERVER_INTERCEPTORS same-line works too
	//dedo:ignore DEDO_GO_SERVER_INTERCEPTORS
	connect.NewServer(flowid.Interceptor()) // want `has no policy interceptor`
	//dedo:ignore DEDO_GO_EVENT_DECLARED another rule's directive suppresses nothing here
	connect.NewServer(flowid.Interceptor()) // want `has no policy interceptor`
}
