import { computed, inject, resource, ResourceRef, Signal } from '@angular/core';
import { Api, ApiFnRequired } from '../api/api';
import { StrictHttpResponse } from '../api/strict-http-response';

export type ApiResource<R> = ResourceRef<StrictHttpResponse<R> | undefined> & {
  /** The response body, or undefined while loading or after an error (never throws). */
  body: Signal<R | undefined>;
  /** A response header, e.g. X-Total-Count (never throws). */
  header: (name: string) => string | null;
};

/**
 * A resource over one generated API function: it reloads whenever params()
 * changes, and skips loading while params() is undefined. Unlike value(),
 * which throws in the error state, body() and header() are safe to read from
 * templates.
 */
export function apiResource<P, R>(
  fn: ApiFnRequired<P, R>,
  params: () => P | undefined,
): ApiResource<R> {
  const api = inject(Api);
  const ref = resource({
    params,
    loader: ({ params }) => api.invoke$Response(fn, params),
  });
  const response = computed(() => (ref.hasValue() ? ref.value() : undefined));
  return Object.assign(ref, {
    body: computed(() => response()?.body),
    header: (name: string) => response()?.headers.get(name) ?? null,
  });
}
