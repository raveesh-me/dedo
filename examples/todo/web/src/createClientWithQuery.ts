// Adapted from https://github.com/raveesh-me/connect-tan-vue: a Connect
// client whose unary methods also offer TanStack Query's useQuery and
// useMutation, keyed by method name and request.
import { createClient } from '@connectrpc/connect'
import type { Client, Transport } from '@connectrpc/connect'
import type { DescService } from '@bufbuild/protobuf'
import { useMutation, useQuery } from '@tanstack/vue-query'
import type { UseMutationReturnType, UseQueryReturnType } from '@tanstack/vue-query'

type AnyFn = (...args: any[]) => any

export interface QueryHelpers<Req, Res> {
  useQuery(request: Req, options?: Record<string, unknown>): UseQueryReturnType<Res, Error>
  useMutation(options?: {
    onSuccess?: (response: Res, request: Req) => unknown
    onError?: (error: Error, request: Req) => unknown
  }): UseMutationReturnType<Res, Error, Req, unknown>
}

// QueryClient adds the query helpers to every unary method of the client.
export type QueryClient<S extends DescService> = {
  [K in keyof Client<S>]: Client<S>[K] extends (request: infer Req, ...rest: any[]) => Promise<infer Res>
    ? Client<S>[K] & QueryHelpers<Req, Res>
    : Client<S>[K]
}

export function createClientWithQuery<S extends DescService>(service: S, transport: Transport): QueryClient<S> {
  const client = createClient(service, transport) as Record<string, AnyFn>
  const cache = new Map<string, AnyFn>()
  return new Proxy(client, {
    get(target, prop, receiver) {
      const value = Reflect.get(target, prop, receiver)
      if (typeof prop !== 'string' || typeof value !== 'function') {
        return value
      }
      if (!cache.has(prop)) {
        const method = value.bind(target) as AnyFn
        if (service.method[prop]?.methodKind === 'unary') {
          Object.assign(method, {
            useQuery: (request: unknown, options?: object) =>
              useQuery({ queryKey: [prop, request], queryFn: () => method(request), ...options }),
            useMutation: (options?: object) =>
              useMutation({ mutationFn: (request: unknown) => method(request), ...options }),
          })
        }
        cache.set(prop, method)
      }
      return cache.get(prop)
    },
  }) as unknown as QueryClient<S>
}
