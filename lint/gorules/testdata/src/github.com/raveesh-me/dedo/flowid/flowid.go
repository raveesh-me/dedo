// Package flowid stands in for dedo's.
package flowid

import "connectrpc.com/connect/v2"

func Interceptor() connect.ServerInterceptor { return nil }

func ClientInterceptor() connect.ServerInterceptor { return nil }
