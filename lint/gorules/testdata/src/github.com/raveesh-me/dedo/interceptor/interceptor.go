// Package interceptor stands in for dedo's.
package interceptor

import "connectrpc.com/connect/v2"

type Config struct{}

func New(cfg Config) (connect.ServerInterceptor, error) { return nil, nil }
